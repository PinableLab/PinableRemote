package rfb

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/si-mods/MyVNC/rfb/auth"
	"github.com/si-mods/MyVNC/rfb/pixel"
)

func TestRequiredScreenPrivacyAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		initial, next              uint32
		ask, receive, cancel, lost bool
		err                        error
	}{
		{name: "accepted then lost", initial: 6, next: 2, ask: true, receive: true, lost: true},
		{name: "already private little endian", initial: 0x02000000},
		{name: "no curtain support", initial: 4, err: ErrPrivacyUnsupported},
		{name: "no confirmation", initial: 6, ask: true, cancel: true, err: ErrPrivacyUnconfirmed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c1, c2 := net.Pipe()
			t.Cleanup(func() { c1.Close(); c2.Close() })
			c2.SetDeadline(time.Now().Add(3 * time.Second))
			s := &fakeServer{t: t, conn: c2, r: c2, w: c2}
			h := newHandler()
			type result struct {
				c   *Client
				err error
			}
			ready := make(chan result, 1)
			go func() {
				c, err := NewClient(context.Background(), c1, "fixture", Options{
					RequireScreenPrivacy: true, SecurityTypes: []uint32{SecTypeVncAuth},
					Password: func() (string, string, error) { return "", "fixture1", nil },
					Decoders: map[int32]pixel.Decoder{EncodingRaw: rawDecoder{}},
				}, h)
				ready <- result{c, err}
			}()
			s.send("RFB 003.889\n")
			s.expect([]byte("RFB 003.889\n"))
			s.send([]byte{1, byte(SecTypeVncAuth)})
			s.expect([]byte{byte(SecTypeVncAuth)})
			var ch [16]byte
			s.send(ch[:])
			want := auth.VncAuthResponse(ch, "fixture1")
			s.expect(want[:])
			s.send(uint32(0))
			s.expect([]byte{0xc1})
			s.send(serverInit(64, 32, []byte("Mac fixture")))
			s.expect(ardViewerInfo())
			s.expect(ardSetMode(1))
			s.expect(ardSetDisplay())
			s.expect(ardAutoPasteboard(false))
			s.recvInit()
			s.expect(framebufferUpdateRequest(false, 64, 32))
			layout := func(flags uint32) {
				msg := layout2(64, 32, 64, 32)
				msg[14] = flags
				s.send(msg[:2]...)
				s.expect(ardAutoFBUpdate(true, 64, 32))
				s.expect(framebufferUpdateRequest(true, 64, 32))
				s.send(msg[2:]...)
			}
			layout(tc.initial)
			if tc.ask {
				s.expect([]byte{12, 0, 0, 0, 0, 0})
				select {
				case got := <-ready:
					t.Fatalf("published before server acknowledgement: %+v", got)
				default:
				}
			}
			if tc.receive {
				layout(tc.next)
			}
			if tc.cancel {
				c2.Close()
			}
			if tc.err == nil {
				s.expect(framebufferUpdateRequest(false, 64, 32))
			}
			select {
			case e := <-h.ev:
				t.Fatalf("received event during preflight: %v", e)
			default:
			}
			got := <-ready
			if !errors.Is(got.err, tc.err) {
				t.Fatalf("got %v, want %v", got.err, tc.err)
			}
			if tc.err != nil {
				return
			}
			if !tc.lost {
				got.c.Close()
				var b [1]byte
				if n, err := c2.Read(b[:]); n != 0 || !errors.Is(err, io.EOF) {
					t.Fatalf("disconnect must not send a curtain restore: %d bytes, %v", n, err)
				}
				return
			}
			done := make(chan error, 1)
			go func() { done <- got.c.Run() }()
			h.next(t, "name")
			h.next(t, "resize")
			layout(6)
			if err := <-done; !errors.Is(err, ErrPrivacyLost) {
				t.Fatalf("Run: %v", err)
			}
		})
	}
}

func TestRequiredPrivacyRejectsGenericVNCBeforeAuth(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()
	done := make(chan error, 1)
	go func() {
		_, err := NewClient(context.Background(), c1, "fixture", Options{
			RequireScreenPrivacy: true,
			Password:             func() (string, string, error) { panic("must not authenticate generic VNC in privacy mode") },
		}, quietPrivacyHandler{})
		done <- err
	}()
	c2.Write([]byte("RFB 003.008\n"))
	if err := <-done; !errors.Is(err, ErrPrivacyUnsupported) {
		t.Fatal(err)
	}
}
