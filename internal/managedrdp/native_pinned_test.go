//go:build (windows && cgo && pinable_freerdp) || (darwin && cgo && freerdp)

// SPDX-License-Identifier: GPL-2.0-or-later

package managedrdp

import (
	"context"
	"errors"
	"testing"
)

func TestPinnedFreeRDPViewerRejectsBeforeHandshake(t *testing.T) {
	pin := [32]byte{1}
	b := Binding{Host: "qa.example.test", Port: 3389, User: "tester", TTLSeconds: 60}
	connect := NewPinnedFreeRDPViewer(b.Host, b.Port, "", "tester", "stored", pin)
	cases := []Binding{b, b, b, b, b}
	cases[0].Host = "other.example.test"
	cases[1].User = `QA\tester`
	cases[2].Port = 3390
	cases[3].AllowInput = true
	cases[4].Password = "from-the-pipe"
	for i, b := range cases {
		if driver, err := connect(context.Background(), b); driver != nil || !errors.Is(err, errNativeSession) {
			t.Fatalf("case %d: unexpected driver or error", i)
		}
	}
	for i, c := range []Connect{
		NewPinnedFreeRDPViewer(b.Host, b.Port, "", "tester", "", pin),
		NewPinnedFreeRDPViewer(b.Host, b.Port, "", "tester", "stored", [32]byte{}),
	} {
		if driver, err := c(context.Background(), b); driver != nil || !errors.Is(err, errNativeSession) {
			t.Fatalf("enrollment %d accepted", i)
		}
	}
}
