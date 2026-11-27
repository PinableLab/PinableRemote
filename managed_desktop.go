// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/si-mods/MyVNC/internal/managedvnc"
)

// This private, versioned invocation is not a user-facing command. It receives
// an independently enrolled relay identity on fd 3 and exactly one task-bound
// session on stdin/stdout. The main path never reads user settings or opens UI
// for this invocation; Desktop owns local authorization and the child lifetime.
const managedDesktopSwitch = "--internal-desktop-vnc-v1"

type desktopVNCBootstrap struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	ServerName string `json:"server_name"`
	SPKI       string `json:"spki_sha256"`
	Privacy    bool   `json:"require_screen_privacy"`
}

var errDesktopVNCBootstrap = errors.New("invalid or unsupported Desktop VNC bootstrap")

func readDesktopVNCBootstrap(r io.Reader) (managedvnc.Identity, error) {
	const limit = 4096
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil || len(data) > limit {
		return managedvnc.Identity{}, errDesktopVNCBootstrap
	}
	var b desktopVNCBootstrap
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var extra json.RawMessage
	if dec.Decode(&b) != nil || dec.Decode(&extra) != io.EOF ||
		b.Privacy || b.Port < 1 || b.Port > 65535 ||
		!bootstrapName(b.Host) || !bootstrapName(b.ServerName) {
		return managedvnc.Identity{}, errDesktopVNCBootstrap
	}
	var pin [32]byte
	if len(b.SPKI) != 2*len(pin) {
		return managedvnc.Identity{}, errDesktopVNCBootstrap
	}
	if _, err := hex.Decode(pin[:], []byte(b.SPKI)); err != nil || pin == ([32]byte{}) {
		return managedvnc.Identity{}, errDesktopVNCBootstrap
	}
	return managedvnc.Identity{Host: b.Host, Port: b.Port, ServerName: b.ServerName, SPKISHA256: pin}, nil
}

func bootstrapName(s string) bool {
	return s != "" && len(s) <= 253 && !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == ':')
	})
}

func inheritedPipe(f *os.File) bool {
	if f == nil {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeNamedPipe != 0
}

func runManagedDesktopVNC(args []string) int {
	// Publish a macOS candidate first. Do not reinterpret a missing Windows
	// RDP driver as an available VNC device or fall back to the manual viewer.
	if runtime.GOOS != "darwin" || len(args) != 1 || !inheritedPipe(os.Stdin) || !inheritedPipe(os.Stdout) {
		return 2
	}
	bootstrap := os.NewFile(3, "desktop-vnc-bootstrap")
	if !inheritedPipe(bootstrap) {
		return 2
	}
	// A damaged or abandoned bootstrap must not wait forever, dial a device,
	// or cause the normal GUI/credential loading path to run.
	type result struct {
		id  managedvnc.Identity
		err error
	}
	decoded := make(chan result, 1)
	go func() {
		id, err := readDesktopVNCBootstrap(bootstrap)
		decoded <- result{id, err}
	}()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	var id managedvnc.Identity
	select {
	case value := <-decoded:
		if value.err != nil {
			return 2
		}
		id = value.id
	case <-timer.C:
		// main exits immediately, even if a blocking fd 3 read has not returned.
		return 2
	}
	_ = bootstrap.Close()
	if err := managedvnc.Serve(context.Background(), os.Stdin, os.Stdout, id); err != nil {
		return 1
	}
	return 0
}
