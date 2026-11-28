// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/si-mods/MyVNC/viewer"
)

func TestSavedRDPComputersNeedPasswordAndTrustedCertificate(t *testing.T) {
	fp := viewer.CertFingerprint([]byte("cert"))
	cfg := &viewer.Config{
		Servers: []string{"rdp://pc", "rdp://nopass:3390", "rdp://nocert", "vnc://mac", "rdp://local"},
		Computers: map[string]viewer.Computer{
			"rdp://pc":    {Name: "Office PC"},
			"rdp://local": {Username: "me"},
		},
	}
	creds := map[string]credential{
		"pc:3389":     {User: `CORP\alice`, Pass: "secret"},
		"nopass:3390": {User: "bob"},
		"nocert:3389": {User: "carol", Pass: "x"},
		"mac:5900":    {User: "dave", Pass: "x"},
		"local:3389":  {Pass: "y"},
	}
	certs := map[string]string{"pc:3389": fp, "nopass:3390": fp, "mac:5900": fp, "local:3389": strings.ToLower(fp)}
	got := savedRDPComputers(cfg,
		func(addr string) (credential, bool) { c, ok := creds[addr]; return c, ok },
		func(addr string) (string, error) { return certs[addr], nil })
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	pc, local := got[0], got[1]
	if pc.Name != "Office PC" || pc.Host != "pc" || pc.Port != 3389 || pc.User != `CORP\alice` ||
		pc.domain != "CORP" || pc.account != "alice" || pc.password != "secret" || pc.pin[0] == 0 {
		t.Fatalf("pc = %+v", pc)
	}
	if local.Name != "local" || local.User != "me" || local.domain != "" || local.pin != pc.pin {
		t.Fatalf("local = %+v", local)
	}
	if pc.ID == local.ID || len(pc.ID) != 32 || strings.Contains(pc.ID, "pc") {
		t.Fatalf("ids %q %q", pc.ID, local.ID)
	}
	failing := savedRDPComputers(cfg,
		func(addr string) (credential, bool) { c, ok := creds[addr]; return c, ok },
		func(string) (string, error) { return "", errors.New("unreadable") })
	if len(failing) != 0 {
		t.Fatal("listed a computer whose certificate could not be read")
	}
}
