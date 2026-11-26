// Neutral native surfaces, text and interaction colors shared by the viewer.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import "github.com/egoist/mygo/ui"

// colors are the app's own, in light and dark windows.
type colors struct {
	background ui.Color

	card, cardFocused, cardBorder, cardBorderFocused ui.Color
	shadow, shadowFocused                            ui.Color

	text, textMuted, textFaint, iconMuted ui.Color
	hover, pressed, focus                 ui.Color

	tabActive, tabSep ui.Color

	// The security levels and banners: ok, warn and danger as dots and
	// text on the cards.
	ok, warn, danger ui.Color

	panel, panelBorder, backdrop ui.Color

	// accent is the color of what is chosen or on.
	accent, accentHover, accentPressed ui.Color
	// surface is the face of controls, opaque as the popovers' panel.
	surface, surfaceHover, surfacePressed ui.Color
}

var lightColors = colors{
	background:        ui.Hex("#f5f6f8"),
	card:              ui.Hex("#ffffff"),
	cardFocused:       ui.Hex("#f8fafd"),
	cardBorder:        ui.Hex("#dfe3e8"),
	cardBorderFocused: ui.Hex("#94a3b8"),
	shadow:            ui.RGBA(60, 40, 50, 0.07),
	shadowFocused:     ui.RGBA(60, 40, 50, 0.12),

	text:      ui.Hex("#1d1d1f"),
	textMuted: ui.Hex("#636366"),
	textFaint: ui.Hex("#5f6571"),
	iconMuted: ui.Hex("#5f6571"),
	hover:     ui.RGBA(0, 0, 0, 0.055),
	pressed:   ui.RGBA(0, 0, 0, 0.1),
	focus:     ui.Hex("#205bbc"),

	tabActive: ui.RGBA(255, 255, 255, 0.97),
	tabSep:    ui.RGBA(0, 0, 0, 0.13),

	ok:     ui.Hex("#1e8e3e"),
	warn:   ui.Hex("#b45309"),
	danger: ui.Hex("#d93025"),

	panel:       ui.Hex("#fcfcfd"),
	panelBorder: ui.Hex("#dce0e6"),
	backdrop:    ui.RGBA(40, 20, 40, 0.18),

	accent: ui.Hex("#2e6fd0"), accentHover: ui.Hex("#2a64bd"), accentPressed: ui.Hex("#2459a8"),
	surface: ui.Hex("#fcfcfd"), surfaceHover: ui.Hex("#efeff2"), surfacePressed: ui.Hex("#e3e3e8"),
}

var darkColors = colors{
	background:        ui.Hex("#131519"),
	card:              ui.Hex("#1c1f25"),
	cardFocused:       ui.Hex("#252a32"),
	cardBorder:        ui.Hex("#353941"),
	cardBorderFocused: ui.Hex("#697383"),
	shadow:            ui.RGBA(0, 0, 0, 0.25),
	shadowFocused:     ui.RGBA(0, 0, 0, 0.4),

	text:      ui.Hex("#f2f2f7"),
	textMuted: ui.Hex("#aeaeb2"),
	textFaint: ui.Hex("#8e8e93"),
	iconMuted: ui.Hex("#98989d"),
	hover:     ui.RGBA(255, 255, 255, 0.08),
	pressed:   ui.RGBA(255, 255, 255, 0.14),
	focus:     ui.Hex("#82bfff"),

	tabActive: ui.RGBA(255, 255, 255, 0.14),
	tabSep:    ui.RGBA(255, 255, 255, 0.14),

	ok:     ui.Hex("#4ade80"),
	warn:   ui.Hex("#fbbf24"),
	danger: ui.Hex("#ff6b6b"),

	panel:       ui.Hex("#262629"),
	panelBorder: ui.Hex("#424753"),
	backdrop:    ui.RGBA(0, 0, 0, 0.35),

	accent: ui.Hex("#6cb2ff"), accentHover: ui.Hex("#82bfff"), accentPressed: ui.Hex("#5aa2f0"),
	surface: ui.Hex("#262629"), surfaceHover: ui.Hex("#303035"), surfacePressed: ui.Hex("#3a3a40"),
}

// palette is the colors of a light or dark window.
func palette(dark bool) *colors {
	if dark {
		return &darkColors
	}
	return &lightColors
}

// colorsOf is the palette of the appearance in force.
func colorsOf(c *ui.Context) *colors { return palette(c.Theme().Dark) }

// appTheme builds the theme for the appearance in force. It is called
// once a frame; the base theme knows whether the system is dark.
func appTheme(c *ui.Context) *ui.Theme {
	return themeFor(c.Theme().Dark, c.Preferences().TextScale)
}

// themeFor is appTheme without a context, for tests.
func themeFor(dark bool, textScale float32) *ui.Theme {
	if textScale <= 0 {
		textScale = 1
	}
	base := ui.Theme{
		Dark:     dark,
		Spacing:  4,
		FontSize: 14 * textScale,
		Font:     "Segoe UI Variable Text",
	}
	k := palette(dark)
	t := widgetTheme(&base, k)
	t.Danger, t.Warning, t.Success = k.danger, k.warn, k.ok
	t.Inverse, t.InverseText = ui.Hex("#2b2b2e"), ui.Hex("#ffffff")
	t.Scrollbar = ui.RGBA(0, 0, 0, 0.28)
	if dark {
		t.Inverse, t.InverseText = ui.Hex("#f2f2f7"), ui.Hex("#1d1d1f")
		t.Scrollbar = ui.RGBA(255, 255, 255, 0.28)
	}
	return t
}

// widgetTheme is the stock widgets' theme in the app's colors, so that the
// buttons, switches, selects and dialogs look as the rest of the app. Its
// Background is the face of dialogs and a segmented control's chosen
// part, not the window's.
func widgetTheme(base *ui.Theme, k *colors) *ui.Theme {
	t := *base
	t.Background, t.Surface, t.SurfaceHover, t.SurfacePressed = k.surface, k.surface, k.surfaceHover, k.surfacePressed
	t.Border, t.Text, t.TextMuted = k.panelBorder, k.text, k.textMuted
	t.Accent, t.AccentHover, t.AccentPressed, t.AccentText = k.accent, k.accentHover, k.accentPressed, ui.Hex("#ffffff")
	if t.Dark {
		t.AccentText = ui.Hex("#10151c")
	}
	t.Selection, t.Focus = k.accent.Alpha(0.3), k.focus
	t.Radius = 9
	return &t
}
