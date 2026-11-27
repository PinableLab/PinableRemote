package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/si-mods/MyVNC/viewer"
)

func TestLegacyAgentSettingsRemoved(t *testing.T) {
	a := newTestApp(t)
	if err := a.tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	for _, page := range a.settingsPages() {
		if page.id == "agent" {
			t.Fatal("legacy Agent settings page remains")
		}
	}
	if a.tt.HasText("Agent (MCP)") {
		t.Fatal("legacy Agent page visible")
	}
}

// TestSettingsVideoControls checks the WinUI-style settings navigation and
// that the video mode sets the HP/encoding pair.
func TestSettingsVideoControls(t *testing.T) {
	a := newTestApp(t)
	if err := a.tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	texts := strings.Join(a.tt.Texts(), "|")
	for _, want := range []string{"Interface", "Video", "Clipboard", "Input", "Connection", "Window"} {
		if !strings.Contains(texts, want) {
			t.Errorf("settings lack the %q page; texts %q", want, texts)
		}
	}
	if err := a.tt.Click("Video"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	texts = strings.Join(a.tt.Texts(), "|")
	for _, want := range []string{"Preset", "Video mode", "ARD High Performance", "H.264 over RFB", "JPEG (Tight)",
		"Max fps", "H.264 kbps", "H.264 GOP", "Orientation", "Default", "90°", "180°", "270°"} {
		if !strings.Contains(texts, want) {
			t.Errorf("video page lacks %q; texts %q", want, texts)
		}
	}
	if err := a.tt.Click("JPEG (Tight)"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	if !strings.Contains(strings.Join(a.tt.Texts(), "|"), "Legacy MJPEG can use") {
		t.Errorf("legacy mode lacks the bandwidth warning: %q", a.tt.Texts())
	}
	if err := a.tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	if a.cfg.HighPerformance || a.cfg.Encoding != "Tight" {
		t.Errorf("legacy mode: hp %v encoding %q", a.cfg.HighPerformance, a.cfg.Encoding)
	}
	if err := a.tt.Click("Settings"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	if err := a.tt.Click("Video"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	if err := a.tt.Click("ARD High Performance"); err != nil {
		t.Fatal(err)
	}
	if err := a.tt.Click("Back"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	if !a.cfg.HighPerformance || a.cfg.Encoding != "Auto" {
		t.Errorf("ARD mode: hp %v encoding %q", a.cfg.HighPerformance, a.cfg.Encoding)
	}
}

// The settings have no OK button: a control's value is saved as it
// changes, and Back and Escape leave the page.
func TestSettingsApplyLive(t *testing.T) {
	a := newTestApp(t)
	for _, step := range []string{"Settings", "Interface"} {
		if err := a.tt.Click(step); err != nil {
			t.Fatal(err)
		}
		a.tt.Frame()
	}
	if strings.Contains(strings.Join(a.tt.Texts(), "|"), "|OK|") {
		t.Errorf("the settings have an OK button; texts %q", a.tt.Texts())
	}
	zoom := a.cfg.Zoom
	if err := a.tt.Click("Increase"); err != nil {
		t.Fatal(err)
	}
	a.tt.Frame()
	saved, err := viewer.LoadConfig(filepath.Join(a.dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if a.cfg.Zoom <= zoom || saved.Zoom != a.cfg.Zoom {
		t.Fatalf("zoom %d in memory, %d on disk, from %d", a.cfg.Zoom, saved.Zoom, zoom)
	}
	a.tt.Key(0, ui.KeyEscape)
	a.tt.Frame()
	if a.settingsOpen {
		t.Fatal("Escape left the settings open")
	}
	if !strings.Contains(strings.Join(a.tt.Texts(), "|"), "Add computer") {
		t.Errorf("no home view; texts %q", a.tt.Texts())
	}
}

func TestSettingsScreenPrivacySwitch(t *testing.T) {
	a := newTestApp(t)
	for _, step := range []string{"Settings", "Connection"} {
		if err := a.tt.Click(step); err != nil {
			t.Fatal(err)
		}
		a.tt.Frame()
	}
	r, ok := a.tt.Find("Require privacy screen")
	if !ok || a.cfg.RequireScreenPrivacy {
		t.Fatalf("missing opt-in switch or enabled by default: %v", a.tt.Texts())
	}
	a.tt.ClickAt(r.X+r.W+20, r.Y+r.H/2)
	a.tt.Frame()
	saved, err := viewer.LoadConfig(filepath.Join(a.dir, "config.json"))
	if err != nil || !saved.RequireScreenPrivacy || !a.cfg.RequireScreenPrivacy {
		t.Fatalf("privacy preference not saved: %v, %v / %v", a.cfg.RequireScreenPrivacy, saved.RequireScreenPrivacy, err)
	}
}
