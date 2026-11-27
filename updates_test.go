package main

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// The Updates page checks now through the plugin, flips the automatic
// check, and shows when the app last checked.
func TestSettingsUpdates(t *testing.T) {
	a := newTestApp(t)
	checks, auto, last := 0, true, time.Time{}
	a.upds = &updateHooks{
		check:   func() { checks++ },
		auto:    func() bool { return auto },
		setAuto: func(on bool) { auto = on },
		last:    func() time.Time { return last },
	}
	for _, step := range []string{"Settings", "Updates"} {
		if err := a.tt.Click(step); err != nil {
			t.Fatal(err)
		}
		a.tt.Frame()
	}
	if texts := strings.Join(a.tt.Texts(), "|"); !strings.Contains(texts, "Never checked for updates") {
		t.Errorf("no last check; texts %q", texts)
	}
	for _, step := range []string{"About", "Check for updates", "Close"} {
		if err := a.tt.Click(step); err != nil {
			t.Fatal(err)
		}
		a.tt.Frame()
	}
	if checks != 1 {
		t.Fatalf("checks %d, want 1", checks)
	}
	r, ok := a.tt.Find("Check for updates automatically")
	if !ok {
		t.Fatalf("no automatic switch; texts %q", a.tt.Texts())
	}
	for _, want := range []bool{false, true} {
		a.tt.ClickAt(r.X+r.W+20, r.Y+r.H/2)
		a.tt.Frame()
		if auto != want {
			t.Fatalf("automatic checks %v after the click, want %v", auto, want)
		}
	}
	// A successful check shows its time; the page names it.
	last = time.Date(2026, 10, 27, 9, 30, 0, 0, time.Local)
	a.tt.Frame()
	if texts := strings.Join(a.tt.Texts(), "|"); !strings.Contains(texts, "Last checked") || !strings.Contains(texts, "2026") {
		t.Errorf("no check time; texts %q", texts)
	}
}

func TestSettingsDesktopAboutNeverChecksStandalone(t *testing.T) {
	a := newTestApp(t)
	a.desktopManaged, a.version = "1", "1.2.3"
	a.upds = &updateHooks{check: func() { t.Fatal("Desktop helper called a standalone updater") }}
	for _, step := range []string{"Settings", "Updates", "About"} {
		if err := a.tt.Click(step); err != nil {
			t.Fatal(err)
		}
		a.tt.Frame()
	}
	texts := strings.Join(a.tt.Texts(), "|")
	if !strings.Contains(texts, "This component is updated with Pinable Desktop") || !strings.Contains(texts, "1.2.3") {
		t.Errorf("no Desktop update status or version; texts %q", texts)
	}
	if _, ok := a.tt.Find("View PinableRemote releases"); ok {
		t.Errorf("standalone check is offered to a Desktop helper; texts %q", texts)
	}
}

func TestSettingsStandaloneOffersOfficialManualReleases(t *testing.T) {
	a := newTestApp(t)
	for _, step := range []string{"Settings", "Updates", "About"} {
		if err := a.tt.Click(step); err != nil {
			t.Fatal(err)
		}
		a.tt.Frame()
	}
	if _, ok := a.tt.Find("View PinableRemote releases"); !ok {
		t.Fatalf("no manual release action; texts %q", a.tt.Texts())
	}
	if _, ok := a.tt.Find("Check for updates"); ok {
		t.Fatal("an installer check is available without an owned signed channel")
	}
}

func TestPackagedUpdaterOnlyTrustsOwnChannel(t *testing.T) {
	if hasOwnedSignedUpdates(packagedUpdateConfig) {
		t.Fatal("inherited MyVNC update config would activate autonomous upstream updates")
	}
	for _, tc := range []struct {
		json string
		want bool
	}{
		{`{"updates":{"github":"si-mods/MyVNC","publicKey":"signed-key"}}`, false},
		{`{"updates":{"github":"pinablelab/pinableremote"}}`, false},
		{`{"updates":{"github":"PinableLab/PinableRemote","publicKey":"aG9jN0hldEsvK041SCt4QzJEMWMwU0xkWXVxSEVuZ00="}}`, true},
		{`{"updates":`, false},
	} {
		if got := hasOwnedSignedUpdates([]byte(tc.json)); got != tc.want {
			t.Errorf("config %s: allow=%t, want %t", tc.json, got, tc.want)
		}
	}
	if desktopManaged("", "1") != "1" || desktopManaged("1", "") != "1" || desktopManaged("yes", "") != "" {
		t.Fatal("Desktop launch markers were not respected")
	}
}

// The TigerVNC page holds the import and export the title bar used to:
// each applies the settings, which leaves them, then runs the file dialog.
func TestSettingsTigerVNC(t *testing.T) {
	a := newTestApp(t)
	var calls []string
	a.importFile = func() { calls = append(calls, "import") }
	a.exportFile = func() { calls = append(calls, "export") }
	open := func() {
		t.Helper()
		for _, step := range []string{"Settings", "TigerVNC files"} {
			if err := a.tt.Click(step); err != nil {
				t.Fatal(err)
			}
			a.tt.Frame()
		}
	}
	open()
	if !strings.Contains(strings.Join(a.tt.Texts(), "|"), "holds a server address") {
		t.Errorf("no help text; texts %q", a.tt.Texts())
	}
	for _, step := range []string{"Import .tigervnc…", "Export .tigervnc…"} {
		open()
		if a.settingsOpen != true {
			t.Fatalf("settings closed before %q", step)
		}
		if err := a.tt.Click(step); err != nil {
			t.Fatal(err)
		}
		a.tt.Frame()
		if a.settingsOpen {
			t.Errorf("%q left the settings open", step)
		}
	}
	if want := []string{"import", "export"}; !slices.Equal(calls, want) {
		t.Errorf("calls %q, want %q", calls, want)
	}
}
