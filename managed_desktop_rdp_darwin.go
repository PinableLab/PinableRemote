//go:build darwin && cgo && freerdp

// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/si-mods/MyVNC/internal/managedrdp"
	"github.com/si-mods/MyVNC/viewer"
)

// loadSavedRDP reads this app's own settings, Keychain and known_hosts.json.
func loadSavedRDP() ([]savedRDP, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return nil, err
	}
	cfg, err := viewer.LoadConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		return nil, err
	}
	known := &viewer.KnownHosts{Path: filepath.Join(dir, "known_hosts.json")}
	cred := func(addr string) (credential, bool) {
		b, err := mygo.Secrets.Get(secretKey(addr))
		if err != nil {
			return credential{}, false
		}
		return decodeCredential(b)
	}
	return savedRDPComputers(&cfg, cred, known.CertFingerprintFor), nil
}

func runManagedDesktopRDPList(args []string) int {
	if len(args) != 1 || !inheritedPipe(os.Stdout) {
		return 2
	}
	saved, err := loadSavedRDP()
	if err != nil {
		return 1
	}
	devices := make([]desktopRDPDevice, 0, len(saved))
	for _, s := range saved {
		devices = append(devices, s.desktopRDPDevice)
	}
	if json.NewEncoder(os.Stdout).Encode(struct {
		Devices []desktopRDPDevice `json:"devices"`
	}{devices}) != nil {
		return 1
	}
	return 0
}

// readDesktopRDPBootstrap takes the chosen device's ID, nothing else.
func readDesktopRDPBootstrap(r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, 257))
	if err != nil || len(data) > 256 {
		return "", errDesktopVNCBootstrap
	}
	var b struct {
		ID string `json:"id"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var extra json.RawMessage
	if dec.Decode(&b) != nil || dec.Decode(&extra) != io.EOF || len(b.ID) != 32 {
		return "", errDesktopVNCBootstrap
	}
	return b.ID, nil
}

func runManagedDesktopRDP(args []string) int {
	if len(args) != 1 || !inheritedPipe(os.Stdin) || !inheritedPipe(os.Stdout) {
		return 2
	}
	bootstrap := os.NewFile(3, "desktop-rdp-bootstrap")
	if !inheritedPipe(bootstrap) {
		return 2
	}
	decoded := make(chan string, 1)
	go func() {
		id, _ := readDesktopRDPBootstrap(bootstrap)
		decoded <- id
	}()
	var id string
	select {
	case id = <-decoded:
	case <-time.After(5 * time.Second):
		return 2
	}
	_ = bootstrap.Close()
	if id == "" {
		return 2
	}
	saved, err := loadSavedRDP()
	if err != nil {
		return 1
	}
	for _, s := range saved {
		if s.ID != id {
			continue
		}
		connect := managedrdp.NewPinnedFreeRDPViewer(s.Host, s.Port, s.domain, s.account, s.password, s.pin)
		if managedrdp.ServeStored(context.Background(), os.Stdin, os.Stdout, connect) != nil {
			return 1
		}
		return 0
	}
	return 2
}
