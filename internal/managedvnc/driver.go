// SPDX-License-Identifier: GPL-2.0-or-later

// Package managedvnc implements a restricted, private Desktop child adapter
// for one approved Mac Screen Sharing relay. It is not a listener or MCP tool.
package managedvnc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math/bits"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/si-mods/MyVNC/internal/managedrdp"
	"github.com/si-mods/MyVNC/rfb"
	"github.com/si-mods/MyVNC/rfb/decode"
	"github.com/si-mods/MyVNC/rfb/pixel"
	"github.com/si-mods/MyVNC/viewer/keymap"
)

const (
	Version    = managedrdp.Version
	VNCUser    = "vnc" // password-only Screen Sharing mode, not a macOS user
	frameLimit = 4 << 20
	frameWait  = 12 * time.Second
	inputWait  = 3 * time.Second
)

var (
	errIdentity = errors.New("managed VNC relay identity does not match approval")
	errBinding  = errors.New("managed VNC binding is not the approved relay")
	errNotReady = errors.New("managed VNC session is not ready")
	errFrame    = errors.New("managed VNC frame is incomplete or exceeds the limit")
	errInput    = errors.New("unsupported managed VNC input")
)

// Identity is immutable configuration from Desktop's authorized device record,
// never from a task or a pipe request. SPKISHA256 is the pre-enrolled SHA-256 of
// the approved relay certificate's DER SubjectPublicKeyInfo (raw 32 bytes).
// ServerName must match a currently valid certificate from a trusted CA.
type Identity struct {
	Host, ServerName string
	Port             int
	SPKISHA256       [sha256.Size]byte
}

// Serve uses the private, single-open newline-JSON broker. The broker owns the
// pipes and closes the session on EOF, revocation, or TTL expiry.
func Serve(ctx context.Context, in io.ReadCloser, out io.WriteCloser, id Identity) error {
	return managedrdp.Serve(ctx, in, out, func(ctx context.Context, b managedrdp.Binding) (managedrdp.Driver, error) {
		d, err := connect(ctx, b, id, dialRelay, nil)
		if err != nil {
			return nil, err
		}
		return d, nil
	})
}

type dialFunc func(context.Context, string, int) (net.Conn, error)

// Do not fall back to loopback, link-local, or wildcard addresses discovered
// through DNS. TLS still authenticates the concrete peer on the chosen address.
func dialRelay(ctx context.Context, host string, port int) (net.Conn, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	var d net.Dialer
	for _, ip := range ips {
		ip = ip.Unmap()
		if ip.Zone() != "" || !relayAddress(ip) {
			continue
		}
		conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(port)))
		if err == nil {
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("managed VNC relay is unavailable")
}

func relayAddress(ip netip.Addr) bool { return ip.IsGlobalUnicast() && ip.IsPrivate() }

func connect(ctx context.Context, b managedrdp.Binding, id Identity, dial dialFunc, fixtureRoots *x509.CertPool) (*driver, error) {
	if id.Host == "" || id.Host != b.Host || id.Port < 1 || id.Port > 65535 || id.Port != b.Port ||
		id.ServerName == "" || strings.TrimSpace(id.ServerName) != id.ServerName ||
		id.SPKISHA256 == ([sha256.Size]byte{}) || b.User != VNCUser || len(b.Password) != 8 {
		return nil, errBinding
	}
	for _, ch := range []byte(b.Password) {
		if ch < 0x21 || ch > 0x7e {
			return nil, errBinding
		}
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: id.ServerName, RootCAs: fixtureRoots}
	cfg.VerifyConnection = func(s tls.ConnectionState) error {
		if len(s.VerifiedChains) == 0 || len(s.PeerCertificates) == 0 {
			return errIdentity
		}
		got := sha256.Sum256(s.PeerCertificates[0].RawSubjectPublicKeyInfo)
		if subtle.ConstantTimeCompare(got[:], id.SPKISHA256[:]) != 1 {
			return errIdentity
		}
		return nil
	}
	raw, err := dial(ctx, id.Host, id.Port)
	if err != nil {
		return nil, err
	}
	tunnel := tls.Client(raw, cfg)
	if err := tunnel.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	d := &driver{raw: raw, done: make(chan struct{}), allowInput: b.AllowInput}
	c, err := rfb.NewClient(ctx, tunnel, id.ServerName, rfb.Options{
		ManagedView: true,
		Decoders:    map[int32]pixel.Decoder{rfb.EncodingRaw: decode.Raw{}},
		Password:    func() (string, string, error) { return "", b.Password, nil },
	}, d)
	b.Password = ""
	if err != nil {
		return nil, err
	}
	d.c = c
	go func() {
		_ = c.Run()
		_ = d.Close()
		close(d.done)
	}()
	return d, nil
}

type driver struct {
	c          *rfb.Client
	raw        net.Conn
	allowInput bool
	closed     atomic.Bool
	done       chan struct{}

	mu      sync.Mutex
	pending *capture
}

type capture struct {
	frame   chan *image.RGBA
	marked  []uint64
	covered int
}

func (d *driver) Status(ctx context.Context) (managedrdp.Status, error) {
	if err := ctx.Err(); err != nil {
		return managedrdp.Status{}, err
	}
	if d.closed.Load() {
		return managedrdp.Status{}, nil
	}
	return managedrdp.Status{Connected: true, Authenticated: true, Screenshot: true, Input: d.allowInput}, nil
}

func (d *driver) Screenshot(ctx context.Context) (managedrdp.Frame, error) {
	ctx, cancel := context.WithTimeout(ctx, frameWait)
	stop := context.AfterFunc(ctx, func() { _ = d.Close() })
	defer func() {
		if ctx.Err() != nil {
			_ = d.Close()
		}
		stop()
		cancel()
	}()
	if d.closed.Load() || ctx.Err() != nil {
		return managedrdp.Frame{}, errNotReady
	}
	p := &capture{frame: make(chan *image.RGBA, 1)}
	d.mu.Lock()
	if d.pending != nil {
		d.mu.Unlock()
		return managedrdp.Frame{}, errNotReady
	}
	d.pending = p
	d.mu.Unlock()
	if err := d.c.RequestManagedFrame(ctx); err != nil {
		d.Close()
		return managedrdp.Frame{}, err
	}
	var im *image.RGBA
	select {
	case <-ctx.Done():
		return managedrdp.Frame{}, ctx.Err()
	case <-d.done:
		return managedrdp.Frame{}, errNotReady
	case im = <-p.frame:
	}
	if im == nil {
		d.Close() // incomplete response cannot establish trusted coordinates
		return managedrdp.Frame{}, errFrame
	}
	if d.closed.Load() || ctx.Err() != nil {
		return managedrdp.Frame{}, errNotReady
	}
	out := &encodedFrame{ctx: ctx}
	for _, quality := range []int{76, 52, 32} {
		if err := jpeg.Encode(out, im, &jpeg.Options{Quality: quality}); err == nil {
			if d.closed.Load() || ctx.Err() != nil {
				return managedrdp.Frame{}, errNotReady
			}
			r := im.Bounds()
			return managedrdp.Frame{MIME: "image/jpeg", Bytes: out.buf.Bytes(), Width: r.Dx(), Height: r.Dy()}, nil
		} else if !errors.Is(err, errFrame) {
			return managedrdp.Frame{}, err
		}
		out.buf.Reset()
	}
	return managedrdp.Frame{}, errFrame
}

type encodedFrame struct {
	ctx context.Context
	buf bytes.Buffer
}

func (o *encodedFrame) Write(p []byte) (int, error) {
	if err := o.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) > frameLimit-o.buf.Len() {
		return 0, errFrame
	}
	return o.buf.Write(p)
}

// Updated and UpdateDone run only on the RFB read goroutine. They alone read
// the framebuffer and copy it before returning to the decoder.
func (d *driver) Updated(r image.Rectangle) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p := d.pending
	if p == nil {
		return
	}
	size := d.c.Framebuffer().Bounds()
	w, total := size.Dx(), size.Dx()*size.Dy()
	if p.covered == total {
		return
	}
	if r == size {
		p.covered = total
		return
	}
	if p.marked == nil {
		p.marked = make([]uint64, (total+63)/64)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		p.mark(y*w+r.Min.X, y*w+r.Max.X)
	}
}

func (p *capture) mark(start, end int) {
	for start < end {
		slot := start >> 6
		to := min(end, (slot+1)<<6)
		mask := (^uint64(0) << (start & 63)) & (^uint64(0) >> (64 - (to - (slot << 6))))
		p.covered += bits.OnesCount64(mask &^ p.marked[slot])
		p.marked[slot] |= mask
		start = to
	}
}

func (d *driver) UpdateDone() {
	d.mu.Lock()
	p := d.pending
	d.pending = nil
	d.mu.Unlock()
	if p == nil {
		return
	}
	fb := d.c.Framebuffer()
	w, h := fb.Bounds().Dx(), fb.Bounds().Dy()
	if p.covered != w*h {
		p.frame <- nil
		return
	}
	im := image.NewRGBA(fb.Bounds())
	copy(im.Pix, fb.Pix)
	p.frame <- im
}

func (d *driver) Resize(int, int)                 {} // managed viewer refuses display-resize messages
func (d *driver) Cursor(*image.RGBA, image.Point) {}
func (d *driver) Bell()                           {}
func (d *driver) Clipboard(string)                {}
func (d *driver) Name(string)                     {}

var namedKeys = map[string]uint32{
	"BackSpace": 0xff08, "Tab": 0xff09, "Return": 0xff0d, "Escape": 0xff1b, "Delete": 0xffff,
	"Home": 0xff50, "Left": 0xff51, "Up": 0xff52, "Right": 0xff53, "Down": 0xff54,
	"PageUp": 0xff55, "PageDown": 0xff56, "End": 0xff57, "Space": 0x20,
}

func printableKey(r rune) uint32 {
	if !unicode.IsPrint(r) {
		return 0
	}
	return keymap.UCSToKeysym(uint32(r))
}

func keyStroke(s string) uint32 {
	if v, ok := namedKeys[s]; ok {
		return v
	}
	r, n := utf8.DecodeRuneInString(s)
	if n != len(s) || r == utf8.RuneError {
		return 0
	}
	return printableKey(r)
}

func (d *driver) keys(ctx context.Context, keys []uint32) error {
	for _, k := range keys {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := d.c.KeyEvent(k, true); err != nil {
			return err
		}
		if err := d.c.KeyEvent(k, false); err != nil {
			return err
		}
	}
	return nil
}

func (d *driver) Input(ctx context.Context, i managedrdp.Input) error {
	if !d.allowInput || d.closed.Load() {
		return errNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, inputWait)
	stop := context.AfterFunc(ctx, func() { _ = d.Close() })
	defer func() {
		if ctx.Err() != nil {
			_ = d.Close()
		}
		stop()
		cancel()
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	switch i.Kind {
	case "click":
		mask := map[string]uint16{"left": 1, "middle": 2, "right": 4}[i.Button]
		// The broker already checked coordinates against the last frame.
		// Managed view has fixed dimensions, so they cannot change here.
		if mask == 0 {
			return errInput
		}
		err = d.c.PointerEvent(mask, i.X, i.Y)
		if err == nil {
			err = d.c.PointerEvent(0, i.X, i.Y)
		}
	case "key":
		if key := keyStroke(i.Key); key != 0 {
			err = d.keys(ctx, []uint32{key})
		} else {
			return errInput
		}
	case "text":
		keys := make([]uint32, 0, utf8.RuneCountInString(i.Text))
		for _, ch := range i.Text {
			v := printableKey(ch)
			if v == 0 {
				return errInput // validate all glyphs before the first event
			}
			keys = append(keys, v)
		}
		if len(keys) == 0 || len(keys) > 1024 {
			return errInput
		}
		err = d.keys(ctx, keys)
	default:
		return errInput
	}
	if err == nil {
		err = d.c.FlushManagedInput(ctx)
	}
	if err != nil {
		d.Close() // partial presses or uncertain delivery cannot be reused
		return fmt.Errorf("managed VNC session input: %w", err)
	}
	return nil
}

func (d *driver) Close() error {
	if d.closed.Swap(true) {
		return nil
	}
	// Cut the transport first so TLS shutdown cannot wait on a stalled peer.
	err := d.raw.Close()
	_ = d.c.Close()
	return err
}
