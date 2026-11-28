// SPDX-License-Identifier: GPL-2.0-or-later

// Package managedrdp implements the private, single-use Desktop RDP broker.
// It is not an MCP server and must not be wired directly to an agent. A
// production driver must provide authenticated RDP-session-only surfaces.
package managedrdp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/netip"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const Version = 1

type Binding struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password"`
	TTLSeconds int    `json:"ttl_seconds"`
	AllowInput bool   `json:"allow_input,omitempty"`
}

type Status struct {
	Connected     bool `json:"connected"`
	Authenticated bool `json:"authenticated"`
	Screenshot    bool `json:"screenshot"`
	Input         bool `json:"input"`
}

type Frame struct {
	ID     uint64 `json:"id"`
	Bytes  []byte `json:"data"`
	MIME   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

type Input struct {
	FrameID uint64 `json:"frame_id"`
	Kind    string `json:"kind"`
	X       int    `json:"x,omitempty"`
	Y       int    `json:"y,omitempty"`
	Button  string `json:"button,omitempty"`
	Key     string `json:"key,omitempty"`
	Text    string `json:"text,omitempty"`
}

// Driver receives only one immutable endpoint; it must refuse any negotiated
// redirection. There is no fallback to a machine desktop or global input. All
// operations must honor context cancellation. Close releases the session.
type Driver interface {
	Status(context.Context) (Status, error)
	Screenshot(context.Context) (Frame, error)
	Input(context.Context, Input) error
	Close() error
}

// Connect uses its context only for the bounded handshake. The returned
// driver's lifetime is controlled separately by Close and operation contexts.
type Connect func(context.Context, Binding) (Driver, error)

type envelope struct {
	ID      uint64   `json:"id"`
	Op      string   `json:"op"`
	Binding *Binding `json:"binding,omitempty"`
	Input   *Input   `json:"input,omitempty"`
}

type result struct {
	ID     uint64  `json:"id"`
	OK     bool    `json:"ok"`
	Error  string  `json:"error,omitempty"`
	Status *Status `json:"status,omitempty"`
	Frame  *Frame  `json:"screenshot,omitempty"`
}

func validBinding(b Binding) bool {
	return len(b.Password) > 0 && len(b.Password) <= 4096 && !strings.ContainsRune(b.Password, 0) && validEndpoint(b)
}

// validStoredBinding is a binding whose password stays in the child's own
// credential store: the pipe must not carry one.
func validStoredBinding(b Binding) bool { return b.Password == "" && validEndpoint(b) }

func validEndpoint(b Binding) bool {
	if b.Port < 1 || b.Port > 65535 || b.TTLSeconds < 1 {
		return false
	}
	if b.TTLSeconds > 1800 || len(b.Host) == 0 || len(b.Host) > 253 ||
		len(b.User) == 0 || len(b.User) > 256 || strings.TrimSpace(b.User) != b.User {
		return false
	}
	for _, r := range b.User {
		if unicode.IsControl(r) {
			return false
		}
	}
	if p, err := netip.ParseAddr(b.Host); err == nil {
		return p.IsGlobalUnicast() && !p.IsLoopback() && p.Zone() == ""
	}
	if strings.EqualFold(b.Host, "localhost") || strings.HasSuffix(strings.ToLower(b.Host), ".localhost") {
		return false
	}
	for _, label := range strings.Split(b.Host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-') {
				return false
			}
		}
	}
	return true
}

func validInput(i Input) bool {
	switch i.Kind {
	case "click":
		return i.X >= 0 && i.X <= 4095 && i.Y >= 0 && i.Y <= 4095 &&
			(i.Button == "left" || i.Button == "right" || i.Button == "middle") && i.Key == "" && i.Text == ""
	case "key":
		return i.X == 0 && i.Y == 0 && i.Button == "" && i.Text == "" && i.Key != "" && len(i.Key) <= 64 && !strings.ContainsFunc(i.Key, unicode.IsControl)
	case "text":
		return i.X == 0 && i.Y == 0 && i.Button == "" && i.Key == "" && i.Text != "" &&
			utf8.ValidString(i.Text) && utf8.RuneCountInString(i.Text) <= 1024 && !strings.ContainsFunc(i.Text, unicode.IsControl)
	}
	return false
}

func checkedFrame(f Frame) bool {
	// Match dimensions against encoded JPEG/PNG headers, not untrusted metadata.
	if f.Width < 1 || f.Width > 4096 || f.Height < 1 || f.Height > 4096 || len(f.Bytes) == 0 || len(f.Bytes) > 4<<20 {
		return false
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(f.Bytes))
	return err == nil && cfg.Width == f.Width && cfg.Height == f.Height &&
		(format == "jpeg" && f.MIME == "image/jpeg" || format == "png" && f.MIME == "image/png")
}

// Serve handles the private pipe; the caller must enforce an authenticated and
// authorized Desktop launch and must never export this pipe to tasks. The first
// and only open binds this child to a host/account; later messages cannot broaden
// the lease. Passing the real stdin as in lets cancellation unblock a blocked
// read. Serve owns both pipe handles, closing them on revocation. Send one
// request at a time and keep stdin open until the last response.
func Serve(parent context.Context, in io.ReadCloser, out io.WriteCloser, connect Connect) error {
	return serve(parent, in, out, connect, validBinding)
}

// ServeStored is Serve for a child that reads the account's password from
// its own credential store; the open binding must leave the password empty.
func ServeStored(parent context.Context, in io.ReadCloser, out io.WriteCloser, connect Connect) error {
	return serve(parent, in, out, connect, validStoredBinding)
}

func serve(parent context.Context, in io.ReadCloser, out io.WriteCloser, connect Connect, valid func(Binding) bool) error {
	if connect == nil {
		return errors.New("RDP driver unavailable")
	}
	defer in.Close()
	defer out.Close()
	read := make(chan envelope)
	bad := make(chan bool, 1)
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() {
		defer close(read)
		defer cancel() // parent EOF also revokes in-flight handshakes and operations
		s := bufio.NewScanner(in)
		s.Buffer(make([]byte, 4096), 24<<10)
		for s.Scan() {
			line := s.Bytes()
			d := json.NewDecoder(bytes.NewReader(line))
			d.DisallowUnknownFields()
			var request envelope
			err := d.Decode(&request)
			if err == nil {
				var extra json.RawMessage
				if d.Decode(&extra) != io.EOF {
					err = errors.New("trailing data")
				}
			}
			clear(line)
			if err != nil || request.ID == 0 {
				bad <- true
				return
			}
			select {
			case read <- request:
			case <-ctx.Done():
				return
			}
		}
		if s.Err() != nil && ctx.Err() == nil {
			bad <- true
		}
	}()

	write := json.NewEncoder(out).Encode
	start := time.NewTimer(10 * time.Second)
	defer start.Stop()
	var open envelope
	select {
	case <-ctx.Done():
		return nil
	case <-start.C:
		return nil
	case r, ok := <-read:
		if !ok {
			return nil
		}
		open = r
	}
	if open.Op != "open" || open.Binding == nil || open.Input != nil || !valid(*open.Binding) {
		return write(result{ID: open.ID, Error: "invalid_binding"})
	}
	allow := open.Binding.AllowInput
	lease, revoke := context.WithTimeout(ctx, time.Duration(open.Binding.TTLSeconds)*time.Second)
	defer revoke()
	stopClosingPipes := context.AfterFunc(lease, func() { _ = in.Close(); _ = out.Close() })
	defer stopClosingPipes()
	handshake, stopHandshake := context.WithTimeout(lease, 20*time.Second)
	driver, err := connect(handshake, *open.Binding)
	stopHandshake()
	open.Binding.Password = ""
	open.Binding = nil
	if driver != nil {
		defer driver.Close()
	}
	if lease.Err() != nil {
		return nil
	}
	if err != nil || driver == nil {
		return write(result{ID: open.ID, Error: "connect_failed"})
	}
	if err := write(result{ID: open.ID, OK: true}); err != nil {
		return err
	}

	last := open.ID
	// A frame must belong to this lease, and no click can lie outside its
	// declared bounds. Validate again after each screenshot and on disconnect.
	width, height := 0, 0
	var frameID uint64
	var frameAt time.Time
	for {
		select {
		case <-lease.Done():
			return nil
		case r, ok := <-read:
			if !ok {
				select {
				case <-bad:
					return errors.New("malformed RDP broker input")
				default:
					return nil
				}
			}
			if lease.Err() != nil {
				return nil
			}
			if r.ID <= last || r.Binding != nil {
				return write(result{ID: r.ID, Error: "scope_violation"})
			}
			last = r.ID
			res := result{ID: r.ID}
			if r.Op == "disconnect" && r.Input == nil {
				res.OK = true
				return write(res)
			}
			if (r.Op != "status" && r.Op != "screenshot" && r.Op != "input") || (r.Op != "input" && r.Input != nil) {
				res.Error = "operation_unavailable"
				if err := write(res); err != nil {
					return err
				}
				continue
			}
			status, err := driver.Status(lease)
			switch {
			case err != nil:
				res.Error = "session_unavailable"
			case r.Op == "status":
				status.Input = status.Input && allow && status.Connected && status.Authenticated
				status.Screenshot = status.Screenshot && status.Connected && status.Authenticated
				if !status.Screenshot {
					width, height = 0, 0
				}
				res.OK, res.Status = true, &status
			case !status.Connected || !status.Authenticated:
				res.Error = "session_not_ready"
				width, height = 0, 0
			case r.Op == "screenshot":
				width, height = 0, 0
				if !status.Screenshot {
					res.Error = "capture_unavailable"
					break
				}
				f, e := driver.Screenshot(lease)
				if e != nil || !checkedFrame(f) {
					res.Error = "capture_failed"
					break
				}
				width, height = f.Width, f.Height
				f.ID = r.ID // unique within this broker lease
				frameID, frameAt = f.ID, time.Now()
				res.OK, res.Frame = true, &f
			case !allow:
				res.Error = "input_not_authorized"
			case !status.Input || !status.Screenshot || width == 0 || height == 0:
				res.Error = "input_unavailable"
			case r.Input == nil || r.Input.FrameID != frameID || time.Since(frameAt) > 20*time.Second:
				res.Error = "stale_frame"
			case !validInput(*r.Input) ||
				(r.Input.Kind == "click" && (r.Input.X >= width || r.Input.Y >= height)):
				res.Error = "invalid_input"
			default:
				if driver.Input(lease, *r.Input) != nil {
					res.Error = "input_failed"
				} else {
					res.OK = true
				}
				width, height = 0, 0 // require a fresh RDP frame before the next action
			}
			if lease.Err() != nil {
				return nil
			}
			if err := write(res); err != nil {
				return err
			}
		}
	}
}
