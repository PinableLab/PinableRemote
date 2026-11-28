// SPDX-License-Identifier: GPL-2.0-or-later

package managedrdp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeDriver struct {
	frame  Frame
	status Status
	inputs int
	closed bool
}

func (f *fakeDriver) Status(context.Context) (Status, error)    { return f.status, nil }
func (f *fakeDriver) Screenshot(context.Context) (Frame, error) { return f.frame, nil }
func (f *fakeDriver) Input(context.Context, Input) error        { f.inputs++; return nil }
func (f *fakeDriver) Close() error                              { f.closed = true; return nil }

func fixture(t *testing.T) *fakeDriver {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 16, 12))); err != nil {
		t.Fatal(err)
	}
	return &fakeDriver{frame: Frame{Bytes: b.Bytes(), MIME: "image/png", Width: 16, Height: 12}, status: Status{Connected: true, Authenticated: true, Screenshot: true, Input: true}}
}

func launch(t *testing.T, raw string, f *fakeDriver) ([]result, string) {
	t.Helper()
	return exchange(t, raw, func(_ context.Context, c Binding) (Driver, error) {
		if c.Host != "approved.example.test" || c.Port != 3389 || c.User != "tester" || c.Password != "mock-secret" {
			return nil, errors.New("unexpected fixture binding")
		}
		return f, nil
	})
}

func exchange(t *testing.T, raw string, connect Connect) ([]result, string) {
	t.Helper()
	in, writeIn := io.Pipe()
	readOut, out := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, in, out, connect) }()
	var outs []result
	var transcript strings.Builder
	d := json.NewDecoder(io.TeeReader(readOut, &transcript))
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if _, err := io.WriteString(writeIn, line+"\n"); err != nil {
			t.Fatal(err)
		}
		var r result
		if err := d.Decode(&r); err != nil {
			t.Fatal(err)
		}
		outs = append(outs, r)
	}
	_ = writeIn.Close()
	if err := <-done; err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	_ = readOut.Close()
	return outs, transcript.String()
}

const readOnlyOpen = `{"id":1,"op":"open","binding":{"host":"approved.example.test","port":3389,"user":"tester","password":"mock-secret","ttl_seconds":900}}`

func TestApprovedScopeScreenshotOnlyByDefault(t *testing.T) {
	f := fixture(t)
	out, raw := launch(t, readOnlyOpen+"\n"+`{"id":2,"op":"status"}
{"id":3,"op":"screenshot"}
{"id":4,"op":"input","input":{"kind":"click","frame_id":3,"x":4,"y":3,"button":"left"}}
{"id":5,"op":"clipboard_get"}
{"id":6,"op":"set_display"}
{"id":7,"op":"disconnect"}
`, f)
	if len(out) != 7 || out[1].Status == nil || out[1].Status.Input || !out[1].Status.Screenshot || out[2].Frame == nil || out[2].Frame.ID != 3 || out[3].Error != "input_not_authorized" ||
		out[4].Error != "operation_unavailable" || out[5].Error != "operation_unavailable" || !out[6].OK || f.inputs != 0 || !f.closed {
		t.Fatalf("unexpected response or driver state: %s", raw)
	}
	if strings.Contains(raw, "mock-secret") || strings.Contains(raw, "approved.example.test") {
		t.Fatal("binding leaked")
	}
}

func TestInputRequiresFreshAuthenticatedFrameAndCorrectBounds(t *testing.T) {
	f := fixture(t)
	request := strings.Replace(readOnlyOpen, `"ttl_seconds":900`, `"ttl_seconds":900,"allow_input":true`, 1)
	out, raw := launch(t, request+"\n"+`{"id":2,"op":"input","input":{"kind":"key","key":"Return"}}
{"id":3,"op":"screenshot"}
{"id":4,"op":"input","input":{"kind":"click","frame_id":3,"x":16,"y":3,"button":"left"}}
{"id":5,"op":"input","input":{"kind":"click","frame_id":3,"x":4,"y":3,"button":"left"}}
{"id":6,"op":"input","input":{"kind":"key","key":"Return"}}
{"id":7,"op":"disconnect"}
`, f)
	if len(out) != 7 || out[1].Error != "input_unavailable" || out[3].Error != "invalid_input" || !out[4].OK || out[5].Error != "input_unavailable" || f.inputs != 1 || !f.closed {
		t.Fatalf("unexpected gating: %s", raw)
	}
	f = fixture(t)
	f.status.Authenticated = false
	out, raw = launch(t, readOnlyOpen+"\n"+`{"id":2,"op":"status"}
{"id":3,"op":"screenshot"}
`, f)
	if len(out) != 3 || out[1].Status.Screenshot || out[2].Error != "session_not_ready" {
		t.Fatalf("pre-auth leaked: %s", raw)
	}
}

func TestInputFrameIDCannotBeReplacedByCaller(t *testing.T) {
	f := fixture(t)
	request := strings.Replace(readOnlyOpen, `"ttl_seconds":900`, `"ttl_seconds":900,"allow_input":true`, 1)
	out, raw := launch(t, request+"\n"+`{"id":2,"op":"screenshot"}
{"id":3,"op":"screenshot"}
{"id":4,"op":"input","input":{"kind":"key","frame_id":2,"key":"Return"}}
{"id":5,"op":"input","input":{"kind":"key","frame_id":3,"key":"Return"}}
{"id":6,"op":"input","input":{"kind":"key","frame_id":3,"key":"Return"}}
`, f)
	if len(out) != 6 || out[3].Error != "stale_frame" || !out[4].OK || out[5].Error != "input_unavailable" || f.inputs != 1 {
		t.Fatalf("unexpected frame use: %s", raw)
	}
}

func TestNoRawDriverErrorOrPasswordInResponse(t *testing.T) {
	out, raw := exchange(t, readOnlyOpen, func(context.Context, Binding) (Driver, error) {
		return nil, errors.New("raw platform error with mock-secret")
	})
	if len(out) != 1 || out[0].Error != "connect_failed" || strings.Contains(raw, "mock-secret") || strings.Contains(raw, "platform") {
		t.Fatalf("unexpected error response: %s", raw)
	}
}

func TestBrokerCannotRebind(t *testing.T) {
	f := fixture(t)
	out, raw := launch(t, readOnlyOpen+"\n"+strings.Replace(readOnlyOpen, `"id":1`, `"id":2`, 1), f)
	if len(out) != 2 || out[1].Error != "scope_violation" || !f.closed {
		t.Fatalf("rebind accepted: %s", raw)
	}
	out, raw = launch(t, readOnlyOpen+"\n"+`{"id":1,"op":"status"}`, fixture(t))
	if len(out) != 2 || out[1].Error != "scope_violation" {
		t.Fatalf("replayed identifier accepted: %s", raw)
	}
}

func TestInvalidFirstBindingCannotDial(t *testing.T) {
	for _, raw := range []string{`{"id":1,"op":"screenshot"}`, strings.Replace(readOnlyOpen, `"mock-secret"`, `""`, 1),
		strings.Replace(readOnlyOpen, "approved.example.test", "rdp://other", 1),
		strings.Replace(readOnlyOpen, "approved.example.test", "localhost", 1),
		strings.Replace(readOnlyOpen, "900", "99999999", 1),
	} {
		n := 0
		out, body := exchange(t, raw, func(context.Context, Binding) (Driver, error) {
			n++
			return nil, errors.New("invalid binding attempted")
		})
		if n != 0 || len(out) != 1 || out[0].Error != "invalid_binding" {
			t.Fatalf("invalid binding accepted: %s", body)
		}
	}
}

func TestBrokerCancelClosesIdleAndBlockedOutput(t *testing.T) {
	for _, consume := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		in, writeIn := io.Pipe()
		readOut, out := io.Pipe()
		started := make(chan struct{})
		f := fixture(t)
		done := make(chan error, 1)
		go func() {
			done <- Serve(ctx, in, out, func(context.Context, Binding) (Driver, error) { close(started); return f, nil })
		}()
		if _, e := io.WriteString(writeIn, readOnlyOpen+"\n"); e != nil {
			t.Fatal(e)
		}
		<-started
		if consume {
			var response result
			if e := json.NewDecoder(readOut).Decode(&response); e != nil || !response.OK {
				t.Fatal(e, response)
			}
		}
		cancel()
		select {
		case e := <-done:
			if e != nil && !errors.Is(e, io.ErrClosedPipe) {
				t.Fatal(e)
			}
			if !f.closed {
				t.Fatal("driver not closed")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("cancel did not unblock private pipe")
		}
		_ = writeIn.Close()
		_ = readOut.Close()
	}
}

func TestBadFrameIsNotReturned(t *testing.T) {
	f := fixture(t)
	f.frame.Width++
	out, raw := launch(t, readOnlyOpen+"\n"+`{"id":2,"op":"screenshot"}`, f)
	if len(out) != 2 || out[1].Error != "capture_failed" || out[1].Frame != nil {
		t.Fatalf("malformed frame returned: %s", raw)
	}
}

func TestEOFRevokesDuringDial(t *testing.T) {
	in, writer := io.Pipe()
	recv, out := io.Pipe()
	defer recv.Close()
	didStart := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), in, out, func(ctx context.Context, _ Binding) (Driver, error) {
			close(didStart)
			<-ctx.Done()
			return nil, ctx.Err()
		})
	}()
	if _, err := io.WriteString(writer, readOnlyOpen+"\n"); err != nil {
		t.Fatal(err)
	}
	<-didStart
	_ = writer.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, io.ErrClosedPipe) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("EOF did not cancel in-flight dial")
	}
}

func TestStoredBindingCarriesNoPassword(t *testing.T) {
	b := Binding{Host: "qa.example.test", Port: 3389, User: "tester", TTLSeconds: 60}
	if !validStoredBinding(b) || validBinding(b) {
		t.Fatal("a stored binding has no password")
	}
	b.Password = "secret"
	if validStoredBinding(b) || !validBinding(b) {
		t.Fatal("a stored binding must not carry a password")
	}
}
