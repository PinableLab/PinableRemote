//go:build windows && cgo && pinable_freerdp

// SPDX-License-Identifier: GPL-2.0-or-later

// pinable-rdp-view is a private, view-only child for Desktop. It has no
// interactive mode, listening socket, or model-facing tools.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/si-mods/MyVNC/internal/managedrdp"
)

const bootstrapLimit = 96 << 10

type enrollment struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Domain string `json:"domain"`
	User   string `json:"user"`
	CAPEM  []byte `json:"ca_pem"`
}

type input struct {
	*bufio.Reader
	io.Closer
}

func readEnrollment(r *bufio.Reader) (enrollment, error) {
	var e enrollment
	line, err := r.ReadSlice('\n')
	if err != nil || len(line) > bootstrapLimit {
		return e, errors.New("invalid RDP enrollment")
	}
	d := json.NewDecoder(bytes.NewReader(line))
	d.DisallowUnknownFields()
	var extra json.RawMessage
	if d.Decode(&e) != nil || d.Decode(&extra) != io.EOF || e.Port < 1 || e.Port > 65535 ||
		len(e.Host) == 0 || len(e.Host) > 253 || len(e.Domain) == 0 || len(e.User) == 0 ||
		len(e.Domain)+len(e.User) > 255 || len(e.CAPEM) == 0 || len(e.CAPEM) > 64<<10 {
		return enrollment{}, errors.New("invalid RDP enrollment")
	}
	return e, nil
}

func pipe(f *os.File) bool {
	typ, err := syscall.GetFileType(syscall.Handle(f.Fd()))
	return err == nil && typ == syscall.FILE_TYPE_PIPE
}

func run() int {
	if len(os.Args) != 1 || !pipe(os.Stdin) || !pipe(os.Stdout) {
		return 2
	}
	reader := bufio.NewReaderSize(os.Stdin, bootstrapLimit+1)
	// Time out even if Desktop dies while the bootstrap line is incomplete.
	type decoded struct {
		enrollment
		err error
	}
	result := make(chan decoded, 1)
	go func() {
		e, err := readEnrollment(reader)
		result <- decoded{e, err}
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	var e enrollment
	select {
	case parsed := <-result:
		if parsed.err != nil {
			return 2
		}
		e = parsed.enrollment
	case <-timer.C:
		return 2 // main exits even if an inherited Windows pipe read is still blocked
	}
	dial := managedrdp.NewFreeRDPViewer(e.Host, e.Port, e.Domain, e.User, e.CAPEM)
	if err := managedrdp.Serve(context.Background(), input{reader, os.Stdin}, os.Stdout, dial); err != nil {
		return 1
	}
	return 0
}

func main() { os.Exit(run()) }
