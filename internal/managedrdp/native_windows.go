//go:build windows && cgo && pinable_freerdp

// SPDX-License-Identifier: GPL-2.0-or-later

package managedrdp

/*

#cgo CFLAGS: -D__STDC_NO_THREADS__=1
#cgo LDFLAGS: -lfreerdp3 -lwinpr3 -lcrypto -lws2_32
#include <stdlib.h>
#include <string.h>
#include "native_rdp.h"
*/
import "C"

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode"
	"unsafe"
)

var errNativeSession = errors.New("native RDP session unavailable")

// NewFreeRDPViewer constructs a read-only driver for one enrolled identity.
// Trust comes from one explicitly selected CA (not env/system roots or TOFU).
// The caller must obtain authorization before calling Connect. This driver is
// not registered as a Desktop capability until native acceptance is complete.
func NewFreeRDPViewer(host string, port int, domain, user string, caPEM []byte) Connect {
	ca := bytes.Clone(caPEM)
	return func(ctx context.Context, b Binding) (Driver, error) {
		if !validBinding(b) || b.Host != host || b.Port != port ||
			b.User != domain+`\`+user || !validPrincipalPart(domain) || !validPrincipalPart(user) ||
			len(ca) == 0 || len(ca) > 64<<10 || b.AllowInput {
			return nil, errNativeSession
		}
		viewer := &nativeViewer{
			requests: make(chan viewerRequest),
			stop:     make(chan struct{}),
			done:     make(chan struct{}),
		}
		ready := make(chan bool, 1)
		go viewer.run(ctx, b, domain, user, ca, ready)
		select {
		case ok := <-ready:
			if ok && ctx.Err() == nil {
				return viewer, nil
			}
			_ = viewer.Close()
		case <-ctx.Done():
			viewer.stopOnce()
		}
		return nil, errNativeSession
	}
}

func validPrincipalPart(s string) bool {
	return s != "" && strings.TrimSpace(s) == s &&
		!strings.ContainsAny(s, `\/@`) && !strings.ContainsFunc(s, unicode.IsControl)
}

type nativeViewer struct {
	requests chan viewerRequest
	stop     chan struct{}
	done     chan struct{}
	stopped  sync.Once
}

type viewerRequest struct {
	ctx   context.Context
	frame bool
	reply chan viewerResponse
}

type viewerResponse struct {
	status Status
	frame  Frame
	err    error
}

func (v *nativeViewer) run(handshake context.Context, b Binding, domain, user string, ca []byte, ready chan<- bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(v.done)
	s := newNativeSession(b, domain, user, ca)
	b.Password = ""
	if s == nil {
		ready <- false
		return
	}
	defer C.pinable_rdp_free(s)

	aborted := make(chan struct{})
	stopHandshake := context.AfterFunc(handshake, func() {
		C.pinable_rdp_abort(s)
		close(aborted)
	})
	ok := handshake.Err() == nil && C.pinable_rdp_connect(s) != 0
	if !stopHandshake() {
		<-aborted
		ok = false
	}
	ready <- ok
	if !ok {
		return
	}
	// Interrupt FreeRDP even if it is handling an event when the lease closes.
	finished := make(chan struct{})
	interruptDone := make(chan struct{})
	go func() {
		defer close(interruptDone)
		select {
		case <-v.stop:
			C.pinable_rdp_abort(s)
		case <-finished:
		}
	}()
	defer func() {
		close(finished)
		<-interruptDone // the native context is still alive here
	}()
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-v.stop:
			return
		case <-ticker.C:
			if C.pinable_rdp_poll(s) == 0 {
				return
			}
		case req := <-v.requests:
			if C.pinable_rdp_poll(s) == 0 {
				return
			}
			if req.ctx.Err() != nil {
				req.reply <- viewerResponse{err: req.ctx.Err()}
				continue
			}
			req.reply <- snapshotNative(s, req.frame)
		}
	}
}

func newNativeSession(b Binding, domain, user string, ca []byte) *C.pinable_rdp {
	h, d, u, p := C.CString(b.Host), C.CString(domain), C.CString(user), C.CString(b.Password)
	defer C.free(unsafe.Pointer(h))
	defer C.free(unsafe.Pointer(d))
	defer C.free(unsafe.Pointer(u))
	defer func() {
		clear(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(b.Password)))
		C.free(unsafe.Pointer(p))
	}()
	return C.pinable_rdp_new(h, C.uint16_t(b.Port), d, u, p, (*C.char)(unsafe.Pointer(&ca[0])), C.size_t(len(ca)))
}

func snapshotNative(s *C.pinable_rdp, withFrame bool) viewerResponse {
	var w, h C.int
	canView := C.pinable_rdp_size(s, &w, &h) != 0
	res := viewerResponse{status: Status{Connected: true, Authenticated: canView, Screenshot: canView}}
	if !withFrame {
		return res
	}
	if !canView {
		res.err = errNativeSession
		return res
	}
	rect := image.Rect(0, 0, int(w), int(h))
	img := image.NewRGBA(rect)
	if C.pinable_rdp_copy(s, (*C.uchar)(unsafe.Pointer(&img.Pix[0])), C.size_t(len(img.Pix))) == 0 {
		res.err = errNativeSession
		return res
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 75}); err != nil || buf.Len() > 4<<20 {
		res.err = errNativeSession
		return res
	}
	res.frame = Frame{Bytes: buf.Bytes(), MIME: "image/jpeg", Width: rect.Dx(), Height: rect.Dy()}
	return res
}

func (v *nativeViewer) request(ctx context.Context, frame bool) viewerResponse {
	r := viewerRequest{ctx: ctx, frame: frame, reply: make(chan viewerResponse, 1)}
	select {
	case <-ctx.Done():
		return viewerResponse{err: ctx.Err()}
	case <-v.stop:
		return viewerResponse{err: errNativeSession}
	case <-v.done:
		return viewerResponse{err: errNativeSession}
	case v.requests <- r:
	}
	select {
	case <-ctx.Done():
		return viewerResponse{err: ctx.Err()}
	case <-v.stop:
		return viewerResponse{err: errNativeSession}
	case <-v.done:
		return viewerResponse{err: errNativeSession}
	case result := <-r.reply:
		if ctx.Err() != nil {
			return viewerResponse{err: ctx.Err()}
		}
		return result
	}
}

func (v *nativeViewer) Status(ctx context.Context) (Status, error) {
	res := v.request(ctx, false)
	return res.status, res.err
}

func (v *nativeViewer) Screenshot(ctx context.Context) (Frame, error) {
	res := v.request(ctx, true)
	return res.frame, res.err
}

func (*nativeViewer) Input(context.Context, Input) error { return errNativeSession }

func (v *nativeViewer) stopOnce() { v.stopped.Do(func() { close(v.stop) }) }

func (v *nativeViewer) Close() error {
	v.stopOnce()
	select {
	case <-v.done:
		return nil
	case <-time.After(2 * time.Second):
		return errNativeSession
	}
}
