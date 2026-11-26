// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/si-mods/MyVNC/locales"
)

func TestChromeIconButtonKeyboardActivation(t *testing.T) {
	pressed := 0
	view := func(c *ui.Context) {
		k := palette(false)
		if iconButton(c, k, "settings", "Preferences", 30, 16).Clicked() {
			pressed++
		}
	}
	tt := ui.NewTester(view, 240, 120)
	tt.Frame()
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("Preferences") {
		t.Fatal("icon-only button must expose its accessible name and keyboard focus")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Key(0, ui.KeySpace)
	if pressed != 2 {
		t.Fatalf("activation count = %d, want 2", pressed)
	}
}

func TestSettingsNavigationByKeyboard(t *testing.T) {
	a := newTestApp(t)
	a.openSettings()
	a.tt.Frame()
	for i := 0; i < 12 && !a.tt.Focused(locales.Video()); i++ {
		a.tt.Key(0, ui.KeyTab)
	}
	if !a.tt.Focused(locales.Video()) {
		t.Fatal("Video tab should be keyboard-focusable")
	}
	a.tt.Key(0, ui.KeyEnter)
	if a.settingsPage != "video" {
		t.Fatalf("selected page = %q, want video", a.settingsPage)
	}
}

func TestComputerGridColumns(t *testing.T) {
	for _, tt := range []struct {
		width float32
		want  int
	}{{0, 1}, {420, 1}, {480, 2}, {800, 3}, {1024, 4}, {2000, 4}} {
		if got := computerGridColumns(tt.width); got != tt.want {
			t.Errorf("width %.0f: %d columns, want %d", tt.width, got, tt.want)
		}
	}
}

func TestNeutralChromeContrast(t *testing.T) {
	for _, k := range []*colors{&lightColors, &darkColors} {
		for _, bg := range []ui.Color{k.background, k.card, k.panel} {
			for _, fg := range []ui.Color{k.text, k.textMuted, k.textFaint, k.iconMuted} {
				if ratio := contrastRatio(fg, bg); ratio < 4.5 {
					t.Errorf("foreground %v on %v: contrast %.2f, want >= 4.5", fg, bg, ratio)
				}
			}
			if ratio := contrastRatio(k.focus, bg); ratio < 3 {
				t.Errorf("focus %v on %v: contrast %.2f, want >= 3", k.focus, bg, ratio)
			}
		}
	}
}

func contrastRatio(fg, bg ui.Color) float64 {
	linear := func(c float64) float64 {
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	luma := func(c ui.Color) float64 {
		return 0.2126*linear(float64(c.R)) + 0.7152*linear(float64(c.G)) + 0.0722*linear(float64(c.B))
	}
	a, b := luma(fg), luma(bg)
	return (max(a, b) + 0.05) / (min(a, b) + 0.05)
}
