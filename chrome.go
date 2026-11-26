// The window's own chrome, after GoRex: a title bar drawn by the app with
// the window's controls, icon buttons, cards and banners.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/si-mods/MyVNC/locales"
)

const (
	titleH        = 44 // also used by the native RDP viewport
	cardR         = 10
	controlRadius = 7
	controlMotion = 120 * time.Millisecond
)

// ownControls tells that the title bar draws the window's controls: on
// Windows, whose own would look like none of the rest of it.
var ownControls = runtime.GOOS == "windows"

// closeRed is the face of the close button under the pointer: Windows 11's.
var closeRed = ui.Hex("#c42b1c")

// titleBar draws a title bar that drags the window: fn's elements, then
// the window's controls.
func (a *app) titleBar(c *ui.Context, k *colors, fn func()) {
	bar := c.TitleBar()
	left := bar.Left
	if left == 0 {
		left = 12 // in full screen, or beside no controls
	} else {
		left += 14
	}
	ui.Row(c).Height(titleH).Shrink(0).Padding(0, max(bar.Right, 8), 0, left).Gap(10).AlignItems(ui.Center).DragWindow().Children(func() {
		fn()
		windowControls(c, k, a.win, a.rdpShown())
	})
}

// rdpShown reports whether a Remote Desktop control covers the window
// under the title bar.
func (a *app) rdpShown() bool { return a.active != nil && a.active.rdp != nil }

// barTip is e's tooltip in the title bar. Under the title bar a Remote
// Desktop control, a native window, would cover it: then it goes beside
// e, before it or, with after, after it.
func barTip(c *ui.Context, k *colors, e ui.Element, s string, beside, after bool) ui.Element {
	if !beside {
		return e.Tooltip(s)
	}
	e.Description(s)
	ui.TooltipBase(c, e, func(tip ui.Element) {
		if after {
			tip.AttachTo(e, ui.AnchorEnd, ui.AnchorStart).MarginStart(6)
		} else {
			tip.AttachTo(e, ui.AnchorStart, ui.AnchorEnd).MarginEnd(6)
		}
		tip.Padding(5, 8).Radius(5).Background(k.text).TextColor(k.card).FontSize(12).
			Shadow(0, 2, 8, 0, ui.RGBA(0, 0, 0, 0.2))
		tip.Children(func() { ui.Text(c, s) })
	})
	return e
}

// windowControls draws the buttons that minimize, maximize and close the
// window, when it has none of its own; close's face turns red, as
// Windows has it.
func windowControls(c *ui.Context, k *colors, win *mygo.Window, beside bool) {
	if !ownControls || win == nil || win.IsFullScreen() {
		return
	}
	maximized := win.IsMaximized()
	ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
		ui.Box(c).Size(1, 16).Margin(0, 6).Background(k.tabSep)
		if barTip(c, k, iconButton(c, k, "minus", locales.WindowMinimize(), 30, 16), locales.WindowMinimize(), beside, false).Clicked() {
			win.Minimize()
		}
		glyph, label := "square", locales.WindowMaximize()
		if maximized {
			glyph, label = "copy", locales.WindowRestore()
		}
		// MaximizeButton shows Windows 11's snap layouts over the button.
		if barTip(c, k, iconButton(c, k, glyph, label, 30, 13), label, beside, false).MaximizeButton().Clicked() {
			if maximized {
				win.Unmaximize()
			} else {
				win.Maximize()
			}
		}
		b := ui.ButtonBase(c).Size(30, 30).Radius(controlRadius).Cursor(ui.CursorPointer).Role(ui.RoleButton).
			Label(locales.WindowClose())
		barTip(c, k, b, locales.WindowClose(), beside, false)
		col := k.iconMuted
		if b.Pressed() {
			b.Background(closeRed.Alpha(0.8))
			col = ui.Hex("#ffffff")
		} else if b.Hovered() {
			b.Background(closeRed)
			col = ui.Hex("#ffffff")
		}
		focusOutline(b, k)
		b.Transition(ui.ElementTransition{Colors: true, Duration: controlMotion})
		b.Children(func() {
			ui.Icon(c, icon("x")).Size(17, 17).TextColor(col)
		})
		if b.Clicked() {
			win.Close()
		}
	})
}

// iconButton is a borderless button of an icon, with a face on hover;
// label names it for screen readers.
func iconButton(c *ui.Context, k *colors, name, label string, size, iconSize float32) ui.Element {
	b := ui.ButtonBase(c).Size(size, size).Radius(controlRadius).Cursor(ui.CursorPointer).Role(ui.RoleButton).Label(label)
	controlInteraction(b, k, false)
	col := k.iconMuted
	if b.Hovered() {
		col = k.text
	}
	b.Children(func() {
		ui.Icon(c, icon(name)).Size(iconSize, iconSize).TextColor(col)
	})
	return b
}

// focusOutline preserves a stable control footprint while exposing keyboard focus.
func focusOutline(e ui.Element, k *colors) {
	e.FocusRingColor(k.focus).FocusRingOffset(-2)
}

func controlInteraction(e ui.Element, k *colors, selected bool) {
	switch {
	case e.Pressed():
		e.Background(k.pressed)
	case selected:
		e.Background(k.tabActive)
	case e.Hovered():
		e.Background(k.hover)
	}
	focusOutline(e, k)
	e.Transition(ui.ElementTransition{Colors: true, Duration: controlMotion})
}

func cardInteraction(e ui.Element, k *colors) ui.Element {
	if e.Pressed() || e.Hovered() {
		e.Background(k.cardFocused).BorderColor(k.cardBorderFocused)
	}
	focusOutline(e, k)
	return e.Transition(ui.ElementTransition{Colors: true, Duration: controlMotion})
}

func card(e ui.Element, k *colors) ui.Element {
	return e.Radius(cardR).Background(k.card).Border(1, k.cardBorder)
}

// panelLook gives e the look of a popover's or dialog's panel.
func panelLook(e ui.Element, k *colors) ui.Element {
	return e.Radius(cardR).Background(k.panel).Border(1, k.panelBorder).Shadow(0, 12, 32, 0, k.shadowFocused)
}

// textButton is a borderless button of text in the accent color, as a
// banner's or a transfer's.
func textButton(c *ui.Context, k *colors, label string) ui.Element {
	b := ui.ButtonBase(c).Role(ui.RoleButton).Label(label).Padding(4, 10).Radius(controlRadius).Shrink(0)
	controlInteraction(b, k, false)
	b.Children(func() {
		ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(k.accent).SingleLine()
	})
	return b
}

// menuItem is a row of the session panel that does something: an icon
// and its label, in color when it is not the text's.
func menuItem(c *ui.Context, k *colors, glyph, label string, color ui.Color) ui.Element {
	b := ui.ButtonBase(c).Role(ui.RoleButton).Label(label).Padding(7, 10).Radius(controlRadius).Gap(10).AlignItems(ui.Center)
	controlInteraction(b, k, false)
	if color.A == 0 {
		color = k.text
	}
	b.Children(func() {
		ui.Icon(c, icon(glyph)).Size(16, 16).TextColor(color)
		ui.Text(c, label).FontSize(13.5).TextColor(color).Grow(1).MinWidth(0)
	})
	return b
}

// switchRow is a label beside a switch bound to v; a click anywhere on
// the row flips it. It reports whether this build flipped it.
func switchRow(c *ui.Context, k *colors, label string, v *bool) bool {
	row := ui.Row(c).Gap(12).Padding(5, 2).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	changed := false
	row.Children(func() {
		ui.Text(c, label).FontSize(13.5).TextColor(k.text).Grow(1).MinWidth(0).Wrap()
		changed = ui.Switch(c, v).Label(label).Changed()
	})
	if !changed && row.Clicked() {
		*v = !*v
		changed = true
	}
	return changed
}

// banner is a message on a card, its color's dot before it, and a button.
func banner(c *ui.Context, k *colors, color ui.Color, text, button string, act func()) {
	panelLook(ui.Row(c).Gap(10).Padding(8, 8, 8, 14).AlignItems(ui.Center), k).Radius(cardR).Children(func() {
		ui.Box(c).Size(8, 8).Radius(4).Background(color).Shrink(0)
		ui.Text(c, text).FontSize(13).TextColor(k.text).Wrap().Grow(1).MinWidth(0)
		if textButton(c, k, button).Clicked() {
			act()
		}
	})
}
