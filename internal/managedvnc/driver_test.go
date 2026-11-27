// SPDX-License-Identifier: GPL-2.0-or-later

package managedvnc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"io"
	"math/big"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"github.com/si-mods/MyVNC/internal/managedrdp"
	"github.com/si-mods/MyVNC/rfb/auth"
)

const fixturePassword = "Z2u.Fa5x"

type fixture struct {
	certificate tls.Certificate
	roots       *x509.CertPool
	id          Identity
	b           managedrdp.Binding
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	_, caKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, serverKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture CA"},
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"approved.example.test"},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		NotBefore: ca.NotBefore, NotAfter: ca.NotAfter}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, caKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, serverKey.Public(), caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err = x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err = x509.ParseCertificate(leafDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return fixture{
		tls.Certificate{Certificate: [][]byte{leafDER}, PrivateKey: serverKey},
		roots,
		Identity{Host: "relay.example.test", Port: 7800, ServerName: "approved.example.test", SPKISHA256: sha256.Sum256(leaf.RawSubjectPublicKeyInfo)},
		managedrdp.Binding{Host: "relay.example.test", Port: 7800, User: VNCUser, Password: fixturePassword, TTLSeconds: 20, AllowInput: true},
	}
}

func (f fixture) dial(run func(net.Conn) error, result chan<- error) dialFunc {
	return func(_ context.Context, host string, port int) (net.Conn, error) {
		if host != f.id.Host || port != f.id.Port {
			return nil, errors.New("test got unexpected endpoint")
		}
		client, server := net.Pipe()
		go func() {
			defer server.Close()
			server.SetDeadline(time.Now().Add(5 * time.Second))
			conn := tls.Server(server, &tls.Config{Certificates: []tls.Certificate{f.certificate}})
			err := conn.Handshake()
			if err == nil {
				err = run(conn)
			}
			result <- err
		}()
		return client, nil
	}
}

func writeBytes(c net.Conn, p ...byte) error {
	_, err := c.Write(p)
	return err
}

func expect(c net.Conn, want ...byte) error {
	buf := make([]byte, len(want))
	if _, err := io.ReadFull(c, buf); err != nil {
		return err
	}
	if !bytes.Equal(buf, want) {
		return fmt.Errorf("unexpected client bytes: %x want %x", buf, want)
	}
	return nil
}

func rfbStart(c net.Conn, version string, w, h uint16, security byte) error {
	if err := writeBytes(c, []byte(version)...); err != nil {
		return err
	}
	if err := expect(c, []byte("RFB 003.008\n")...); err != nil {
		return err
	}
	if err := writeBytes(c, 1, security); err != nil {
		return err
	}
	if security != 2 {
		var reply [1]byte
		if _, err := c.Read(reply[:]); err == nil {
			return errors.New("unexpectedly selected unsupported security")
		}
		return nil
	}
	if err := expect(c, 2); err != nil {
		return err
	}
	var challenge [16]byte
	if _, err := rand.Read(challenge[:]); err != nil {
		return err
	}
	if err := writeBytes(c, challenge[:]...); err != nil {
		return err
	}
	got := make([]byte, 16)
	if _, err := io.ReadFull(c, got); err != nil {
		return err
	}
	want := auth.VncAuthResponse(challenge, fixturePassword)
	if !bytes.Equal(want[:], got) {
		return writeBytes(c, 0, 0, 0, 1, 0, 0, 0, 0)
	}
	if err := writeBytes(c, 0, 0, 0, 0); err != nil {
		return err
	}
	if err := expect(c, 1); err != nil {
		return err
	}
	header := make([]byte, 24)
	binary.BigEndian.PutUint16(header, w)
	binary.BigEndian.PutUint16(header[2:], h)
	if err := writeBytes(c, header...); err != nil {
		return err
	}
	if w > 4096 || h > 4096 {
		return nil
	}
	pf := make([]byte, 20)
	if _, err := io.ReadFull(c, pf); err != nil {
		return err
	}
	if pf[0] != 0 || pf[4] != 32 {
		return errors.New("did not set raw pixel format")
	}
	// Verify it advertises Raw exclusively: no resize/continuous, clipboard,
	// media, ARD or other extensions are offered by this client.
	return expect(c, 2, 0, 0, 1, 0, 0, 0, 0)
}

func frameReq(c net.Conn) error { return expect(c, 3, 0, 0, 0, 0, 0, 0, 12, 0, 8) }

func rawFrame(c net.Conn, rects ...image.Rectangle) error {
	buf := []byte{0, 0, 0, byte(len(rects))}
	for _, r := range rects {
		buf = binary.BigEndian.AppendUint16(buf, uint16(r.Min.X))
		buf = binary.BigEndian.AppendUint16(buf, uint16(r.Min.Y))
		buf = binary.BigEndian.AppendUint16(buf, uint16(r.Dx()))
		buf = binary.BigEndian.AppendUint16(buf, uint16(r.Dy()))
		buf = binary.BigEndian.AppendUint32(buf, 0)
		for range r.Dx() * r.Dy() {
			buf = append(buf, 180, 40, 10, 0)
		}
	}
	return writeBytes(c, buf...)
}

type testResponse struct {
	OK     bool              `json:"ok"`
	Error  string            `json:"error"`
	Status managedrdp.Status `json:"status"`
	Frame  managedrdp.Frame  `json:"screenshot"`
}

func TestPrivateBrokerScreenshotAndSessionInput(t *testing.T) {
	f := newFixture(t)
	errCh := make(chan error, 1)
	dial := f.dial(func(c net.Conn) error {
		if err := rfbStart(c, "RFB 003.889\n", 12, 8, 2); err != nil {
			return err
		}
		if err := frameReq(c); err != nil {
			return err
		}
		if err := rawFrame(c, image.Rect(0, 0, 6, 8), image.Rect(6, 0, 12, 8)); err != nil {
			return err
		}
		// A stale frame and an out-of-bounds click were rejected upstream.
		if err := expect(c, 5, 1, 0, 7, 0, 4, 5, 0, 0, 7, 0, 4); err != nil {
			return err
		}
		if err := frameReq(c); err != nil {
			return err
		}
		if err := rawFrame(c, image.Rect(0, 0, 12, 8)); err != nil {
			return err
		}
		if err := expect(c, 4, 1, 0, 0, 0, 0, 0xff, 0x0d, 4, 0, 0, 0, 0, 0, 0xff, 0x0d); err != nil {
			return err
		}
		// Keep the fixture session live until the broker explicitly revokes it.
		var b [1]byte
		_, err := c.Read(b[:])
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}, errCh)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	in, parentIn := io.Pipe()
	parentOut, out := io.Pipe()
	defer parentIn.Close()
	defer parentOut.Close()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- managedrdp.Serve(ctx, in, out, func(ctx context.Context, b managedrdp.Binding) (managedrdp.Driver, error) {
			return connect(ctx, b, f.id, dial, f.roots)
		})
	}()
	enc, dec := json.NewEncoder(parentIn), json.NewDecoder(parentOut)
	round := func(id int, op string, scope string, v any) (result testResponse) {
		t.Helper()
		m := map[string]any{"id": id, "op": op}
		if scope != "" {
			m[scope] = v
		}
		if err := enc.Encode(m); err != nil {
			t.Fatal(err)
		}
		if err := dec.Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if reply := round(1, "open", "binding", f.b); !reply.OK {
		t.Fatalf("open: %+v", reply)
	}
	if reply := round(2, "status", "", nil); !reply.OK || !reply.Status.Authenticated || !reply.Status.Input {
		t.Fatalf("status: %+v", reply)
	}
	f1 := round(3, "screenshot", "", nil)
	cfg, typ, err := image.DecodeConfig(bytes.NewReader(f1.Frame.Bytes))
	if !f1.OK || f1.Frame.ID != 3 || err != nil || typ != "jpeg" || cfg.Width != 12 || cfg.Height != 8 {
		t.Fatalf("invalid frame: %+v %s %v", cfg, typ, err)
	}
	click := managedrdp.Input{Kind: "click", Button: "left", X: 7, Y: 4, FrameID: 2}
	if reply := round(4, "input", "input", click); reply.OK || reply.Error != "stale_frame" {
		t.Fatalf("wrong frame accepted: %+v", reply)
	}
	click.FrameID, click.X = 3, 12
	if reply := round(5, "input", "input", click); reply.OK || reply.Error != "invalid_input" {
		t.Fatalf("out of bounds accepted: %+v", reply)
	}
	click.X = 7
	if reply := round(6, "input", "input", click); !reply.OK {
		t.Fatalf("click: %+v", reply)
	}
	if reply := round(7, "input", "input", click); reply.OK {
		t.Fatalf("reused frame accepted: %+v", reply)
	}
	if reply := round(8, "screenshot", "", nil); !reply.OK || reply.Frame.ID != 8 {
		t.Fatalf("second screenshot: %+v", reply)
	}
	stroke := managedrdp.Input{Kind: "key", Key: "Return", FrameID: 8}
	if reply := round(9, "input", "input", stroke); !reply.OK {
		t.Fatalf("key: %+v", reply)
	}
	if reply := round(10, "disconnect", "", nil); !reply.OK {
		t.Fatalf("disconnect: %+v", reply)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if err := <-serveErr; err != nil {
		t.Fatal(err)
	}
}

func TestIdentityAndAuthenticationFailBeforeCapabilities(t *testing.T) {
	f := newFixture(t)
	for _, variant := range []string{"pin", "name", "untrustedCA", "wrongPassword", "anonymous", "ardDHOnly", "macAuthOnly", "oversize"} {
		t.Run(variant, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			id, b, roots := f.id, f.b, f.roots
			var entered atomic.Bool
			result := make(chan error, 1)
			sec, width := byte(2), uint16(12)
			switch variant {
			case "pin":
				id.SPKISHA256[0] ^= 1
			case "name":
				id.ServerName = "wrong.example.test"
			case "untrustedCA":
				roots = x509.NewCertPool()
			case "wrongPassword":
				b.Password = "wr0ngP.w"
			case "anonymous":
				sec = 1
			case "ardDHOnly":
				sec = 30
			case "macAuthOnly":
				sec = 33
			case "oversize":
				width = 65535
			}
			dial := f.dial(func(c net.Conn) error {
				entered.Store(true)
				return rfbStart(c, "RFB 003.008\n", width, 8, sec)
			}, result)
			d, err := connect(ctx, b, id, dial, roots)
			if err == nil || d != nil {
				t.Fatalf("reached capability after %s", variant)
			}
			<-result
			if (variant == "pin" || variant == "name" || variant == "untrustedCA") && entered.Load() {
				t.Fatal("RFB started without authenticated tunnel")
			}
		})
	}
}

func TestPublicServeRejectsOtherEndpointBeforeDial(t *testing.T) {
	f := newFixture(t)
	f.b.Host = "wrong.example.test"
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	in, parentIn := io.Pipe()
	parentOut, out := io.Pipe()
	defer parentIn.Close()
	defer parentOut.Close()
	serveErr := make(chan error, 1)
	go func() { serveErr <- Serve(ctx, in, out, f.id) }()
	if err := json.NewEncoder(parentIn).Encode(map[string]any{"id": 1, "op": "open", "binding": f.b}); err != nil {
		t.Fatal(err)
	}
	var reply testResponse
	if err := json.NewDecoder(parentOut).Decode(&reply); err != nil {
		t.Fatal(err)
	}
	if reply.OK || reply.Error != "connect_failed" {
		t.Fatalf("approved binding not enforced: %+v", reply)
	}
	if err := <-serveErr; err != nil {
		t.Fatal(err)
	}
}

func TestIncompleteFrameAndCancellationCloseSession(t *testing.T) {
	f := newFixture(t)
	for _, stalled := range []bool{false, true} {
		t.Run(fmt.Sprint(stalled), func(t *testing.T) {
			result := make(chan error, 1)
			dial := f.dial(func(c net.Conn) error {
				if err := rfbStart(c, "RFB 003.008\n", 12, 8, 2); err != nil {
					return err
				}
				if err := frameReq(c); err != nil {
					return err
				}
				if stalled {
					var buf [1]byte
					_, err := c.Read(buf[:])
					return err
				}
				// Overlap deliberately sums to the full area, leaving holes.
				return rawFrame(c, image.Rect(0, 0, 6, 8), image.Rect(0, 0, 6, 8))
			}, result)
			d, err := connect(t.Context(), f.b, f.id, dial, f.roots)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			if frame, err := d.Screenshot(ctx); err == nil || len(frame.Bytes) != 0 {
				t.Fatal("untrusted or expired framebuffer was returned")
			}
			select {
			case <-d.done:
			case <-time.After(time.Second):
				t.Fatal("did not close the failed session")
			}
			<-result
		})
	}
}

func TestRelayAddressExcludesLocalFallback(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "0.0.0.0", "169.254.1.2", "fe80::5", "224.0.0.1", "8.8.8.8", "2001:4860:4860::8888"} {
		if relayAddress(netip.MustParseAddr(ip)) {
			t.Fatalf("accepted local or non-unicast address %s", ip)
		}
	}
	if !relayAddress(netip.MustParseAddr("192.168.0.5")) {
		t.Fatal("refused ordinary approved relay address")
	}
}

func TestReadOnlyAndSideChannelsDoNotReachSession(t *testing.T) {
	f := newFixture(t)
	f.b.AllowInput = false
	notify := make(chan struct{})
	result := make(chan error, 1)
	dial := f.dial(func(c net.Conn) error {
		if err := rfbStart(c, "RFB 003.008\n", 12, 8, 2); err != nil {
			return err
		}
		select {
		case <-notify:
		case <-time.After(3 * time.Second):
			return errors.New("fixture did not reach forbidden input check")
		}
		// ServerCutText message type alone is rejected before reading content.
		// The client must not send a hidden clipboard request or keyboard data.
		if err := writeBytes(c, 3); err != nil {
			return err
		}
		var b [1]byte
		_, err := c.Read(b[:])
		if !errors.Is(err, io.EOF) {
			return fmt.Errorf("unexpected side-channel reply %x or error %v", b, err)
		}
		return nil
	}, result)
	d, err := connect(t.Context(), f.b, f.id, dial, f.roots)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if status, err := d.Status(t.Context()); err != nil || !status.Authenticated || status.Input {
		t.Fatalf("unexpected read-only status: %+v %v", status, err)
	}
	if err := d.Input(t.Context(), managedrdp.Input{Kind: "text", Text: "restricted"}); err == nil {
		t.Fatal("read-only driver accepted input")
	}
	close(notify)
	select {
	case <-d.done:
	case <-time.After(3 * time.Second):
		t.Fatal("forbidden clipboard message did not close session")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}
