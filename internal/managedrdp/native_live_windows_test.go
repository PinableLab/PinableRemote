//go:build windows && cgo && pinable_freerdp

// SPDX-License-Identifier: GPL-2.0-or-later

package managedrdp

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestFreeRDPViewerLive connects only when explicitly enabled by a Windows QA
// operator. It captures no files and performs no remote input.
func TestFreeRDPViewerLive(t *testing.T) {
	if os.Getenv("PINABLE_QA_RDP_LIVE") != "1" {
		t.Skip("requires explicit opt-in and an enrolled Windows QA endpoint")
	}
	host := os.Getenv("PINABLE_QA_RDP_HOST")
	domain := os.Getenv("PINABLE_QA_RDP_DOMAIN")
	user := os.Getenv("PINABLE_QA_RDP_USER")
	password := os.Getenv("PINABLE_QA_RDP_PASSWORD")
	port, err := strconv.Atoi(os.Getenv("PINABLE_QA_RDP_PORT"))
	ca, caErr := os.ReadFile(os.Getenv("PINABLE_QA_RDP_CA_PEM"))
	b := Binding{Host: host, Port: port, User: domain + `\` + user, Password: password, TTLSeconds: 60}
	if err != nil || caErr != nil || len(ca) == 0 || len(ca) > 64<<10 || !validBinding(b) {
		t.Fatal("invalid or incomplete live-test enrollment")
	}

	// Give the viewer its own handshake deadline: the session must remain
	// available after that context is canceled, until explicitly revoked.
	connect := NewFreeRDPViewer(host, port, domain, user, ca)
	handshake, cancelHandshake := context.WithTimeout(context.Background(), 20*time.Second)
	viewer, err := connect(handshake, b)
	cancelHandshake()
	b.Password = ""
	password = ""
	if err != nil {
		t.Fatal("RDP handshake failed; confirm endpoint, NLA, account, and enrolled CA")
	}
	defer viewer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := viewer.Status(ctx)
		if err != nil || !status.Connected {
			t.Fatal("RDP session disconnected before an authenticated frame")
		}
		if status.Input {
			t.Fatal("read-only viewer advertised input")
		}
		if status.Authenticated && status.Screenshot {
			frame, err := viewer.Screenshot(ctx)
			if err != nil || !checkedFrame(frame) {
				t.Fatal("invalid frame for authenticated RDP session")
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("RDP session never provided a named logon and painted frame")
		case <-ticker.C:
		}
	}
	if err := viewer.Close(); err != nil {
		t.Fatal("RDP lease failed to close")
	}
	if _, err := viewer.Screenshot(context.Background()); !errors.Is(err, errNativeSession) {
		t.Fatal("revoked viewer still exposed a screenshot")
	}
}
