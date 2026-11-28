// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/si-mods/MyVNC/viewer"
)

func TestSavedRDPComputersNeedRememberedAccountAndTrustedCertificate(t *testing.T) {
	fp := viewer.CertFingerprint([]byte("cert"))
	now := time.Now()
	cfg := &viewer.Config{
		Remember: true,
		Servers:  []string{"rdp://pc", "rdp://never:3390", "rdp://nocert", "vnc://mac", "rdp://local"},
		Computers: map[string]viewer.Computer{
			"rdp://pc":     {Name: "Office PC", Username: `CORP\alice`, Last: now},
			"rdp://never":  {Username: "bob"},
			"rdp://nocert": {Username: "carol", Last: now},
			"vnc://mac":    {Username: "dave", Last: now},
			"rdp://local":  {Username: "me", Last: now},
		},
	}
	certs := map[string]string{"pc:3389": fp, "never:3390": fp, "mac:5900": fp, "local:3389": strings.ToLower(fp)}
	cert := func(addr string) (string, error) { return certs[addr], nil }
	got := savedRDPComputers(cfg, cert)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	pc, local := got[0], got[1]
	if pc.Name != "Office PC" || pc.Host != "pc" || pc.Port != 3389 || pc.User != `CORP\alice` ||
		pc.addr != "pc:3389" || pc.domain != "CORP" || pc.account != "alice" || pc.pin[0] == 0 {
		t.Fatalf("pc = %+v", pc)
	}
	if local.Name != "local" || local.User != "me" || local.domain != "" || local.pin != pc.pin {
		t.Fatalf("local = %+v", local)
	}
	if pc.ID == local.ID || len(pc.ID) != 32 || strings.Contains(pc.ID, "pc") {
		t.Fatalf("ids %q %q", pc.ID, local.ID)
	}
	if len(savedRDPComputers(cfg, func(string) (string, error) { return "", errors.New("unreadable") })) != 0 {
		t.Fatal("listed a computer whose certificate could not be read")
	}
	cfg.Remember = false
	if len(savedRDPComputers(cfg, cert)) != 0 {
		t.Fatal("listed computers while passwords are not remembered")
	}
}
