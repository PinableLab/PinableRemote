// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/si-mods/MyVNC/viewer"
)

// Private, versioned invocations for Desktop tasks on macOS. The list one
// prints the saved Remote Desktop computers a task may view; the session one
// serves exactly one of them on stdin/stdout. Neither opens UI, and the
// password never leaves this process: Desktop gets only the identity it shows
// the user and binds the session to.
const (
	managedDesktopRDPListSwitch = "--internal-desktop-rdp-list-v1"
	managedDesktopRDPSwitch     = "--internal-desktop-rdp-v1"
)

// desktopRDPDevice is a saved computer as Desktop sees it.
type desktopRDPDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Host string `json:"host"`
	Port int    `json:"port"`
	User string `json:"user"`
}

// savedRDP is a saved computer with what a session needs, all of it the
// user's own choices in this app: the remembered account and password, and
// the certificate trusted at its first connection.
type savedRDP struct {
	desktopRDPDevice
	domain, account, password string
	pin                       [32]byte
}

// desktopRDPID names a saved server without revealing it.
func desktopRDPID(server string) string {
	h := sha256.Sum256([]byte("pinable-desktop-rdp\x00" + server))
	return hex.EncodeToString(h[:16])
}

// savedRDPComputers returns the saved Remote Desktop computers that can be
// connected without asking anything: those with a remembered password and
// a trusted certificate. Others are left out, not offered half-configured.
func savedRDPComputers(cfg *viewer.Config, cred func(addr string) (credential, bool), cert func(addr string) (string, error)) []savedRDP {
	var out []savedRDP
	for _, server := range cfg.Servers {
		t, err := viewer.ParseURL(server)
		if err != nil || t.Scheme != viewer.SchemeRDP {
			continue
		}
		addr := t.Addr()
		c, ok := cred(addr)
		if !ok || c.Pass == "" {
			continue
		}
		user := c.User
		if user == "" {
			user = cfg.Computers[server].Username
		}
		domain, account := "", user
		if i := strings.LastIndex(user, `\`); i >= 0 {
			domain, account = user[:i], user[i+1:]
		}
		if account == "" || strings.ContainsAny(account, `\/@`) || strings.ContainsAny(domain, `/@`) {
			continue
		}
		fp, err := cert(addr)
		if err != nil || fp == "" {
			continue
		}
		var pin [32]byte
		if b, err := hex.DecodeString(strings.ReplaceAll(fp, ":", "")); err != nil || len(b) != len(pin) {
			continue
		} else {
			copy(pin[:], b)
		}
		name := cfg.Computers[server].Name
		if name == "" {
			name = t.Host
		}
		principal := account
		if domain != "" {
			principal = domain + `\` + account
		}
		out = append(out, savedRDP{
			desktopRDPDevice: desktopRDPDevice{ID: desktopRDPID(server), Name: name, Host: t.Host, Port: t.Port, User: principal},
			domain:           domain, account: account, password: c.Pass, pin: pin,
		})
	}
	return out
}
