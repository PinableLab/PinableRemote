//go:build windows && cgo && pinable_freerdp

// SPDX-License-Identifier: GPL-2.0-or-later

package managedrdp

import (
	"context"
	"errors"
	"testing"
)

func TestFreeRDPViewerRejectsBeforeHandshake(t *testing.T) {
	b := Binding{Host: "qa.example.test", Port: 3389, User: "QA\\tester", Password: "test-only", TTLSeconds: 60}
	connect := NewFreeRDPViewer(b.Host, b.Port, "QA", "tester", []byte("invalid CA"))
	cases := []Binding{b, b, b, b, b}
	cases[0].Host = "other.example.test"
	cases[1].User = "OTHER\\tester"
	cases[2].Port = 3390
	cases[3].AllowInput = true
	for _, b := range cases {
		if driver, err := connect(context.Background(), b); driver != nil || !errors.Is(err, errNativeSession) {
			t.Fatal("unexpected driver or error for invalid binding or unenrolled CA")
		}
	}
}

func TestFreeRDPViewerCannotEmitInput(t *testing.T) {
	if err := (&nativeViewer{}).Input(context.Background(), Input{Kind: "click"}); !errors.Is(err, errNativeSession) {
		t.Fatal("view-only driver accepted input")
	}
}
