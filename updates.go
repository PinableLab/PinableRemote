// The Settings → Updates page, and the hooks the updater plugin gives it.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"crypto/ed25519"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/si-mods/MyVNC/locales"
)

// updateHooks is what the Updates page asks of the updater plugin; nil
// when this build has none, as in the tests and in --perftest runs.
type updateHooks struct {
	check   func()           // opens the update window and checks
	auto    func() bool      // whether checks run in the background
	setAuto func(on bool)    // turns them on or off
	last    func() time.Time // when the app last checked, zero for never
}

// updatesView is the Updates page: checking now, the automatic check, and
// when the app last checked. The page says why when the build cannot
// update itself.
func (a *app) updatesView(c *ui.Context, k *colors) {
	settingRow(c, k, "PinableRemote", a.version, func() {
		if ui.Button(c, aboutText(c.Locale(), "About", "关于")).Clicked() {
			a.aboutOpen = true
		}
	})
	h := a.upds
	if h == nil || a.desktopManaged == "1" {
		return
	}
	if h.auto != nil && h.setAuto != nil {
		on := h.auto()
		if settingSwitch(c, k, locales.AutoUpdate(), locales.AutoUpdateHint(), &on) {
			h.setAuto(on)
			a.svc.Invalidate() // the switch's own state is the plugin's
		}
	}
	if h.last != nil {
		settingRow(c, k, locales.LastCheckLabel(), "", func() {
			text := locales.NeverChecked()
			if when := h.last(); !when.IsZero() {
				text = locales.LastChecked(when.Local())
			}
			ui.Text(c, text).FontSize(12.5).TextColor(k.textMuted).SingleLine()
		})
	}
}

const ownUpdateRepository = "PinableLab/PinableRemote"

// The fork's package config still references the upstream updater. Its
// signed releases are not releases of PinableRemote. Do not register the
// installer plugin unless the package targets a Pinable-owned signer/channel.
//
//go:embed mygo.json
var packagedUpdateConfig []byte

func hasOwnedSignedUpdates(data []byte) bool {
	var cfg struct {
		Updates struct {
			GitHub    string
			PublicKey string
		}
	}
	if json.Unmarshal(data, &cfg) != nil || !strings.EqualFold(cfg.Updates.GitHub, ownUpdateRepository) {
		return false
	}
	key, err := base64.StdEncoding.DecodeString(cfg.Updates.PublicKey)
	return err == nil && len(key) == ed25519.PublicKeySize
}

func aboutText(locale, en, zh string) string {
	if strings.HasPrefix(strings.ToLower(locale), "zh") {
		return zh
	}
	return en
}

// Keep the standalone build native and buildable from the public repository:
// the shared Pinable Design library is not publicly available.
func (a *app) aboutDialog(c *ui.Context) {
	k := colorsOf(c)
	label := func(en, zh string) string { return aboutText(c.Locale(), en, zh) }
	dialog(c, k, &a.aboutOpen, label("About", "关于"), 440, func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if b := appIcon(); b != nil {
				ui.Image(c, b).Size(28, 28)
			}
			ui.Text(c, "PinableRemote").FontSize(18).FontWeight(700).TextColor(k.text)
		})
		version := a.version
		if version == "" {
			version = label("Development build", "开发构建")
		}
		ui.Text(c, label("Version: ", "版本：")+version).TextColor(k.textMuted).Wrap()
		switch {
		case a.desktopManaged == "1":
			ui.Text(c, label("This component is updated with Pinable Desktop.", "此组件随 Pinable Desktop 更新。")).TextColor(k.textMuted).Wrap()
		case a.upds != nil && a.upds.check != nil:
			if ui.Button(c, locales.CheckForUpdates()).Clicked() {
				a.upds.check()
			}
		default:
			ui.Text(c, label("Automatic installation is unavailable. New releases can be installed manually.", "此构建不支持自动安装，可以手动安装发布版本。")).TextColor(k.textMuted).Wrap()
			if ui.Button(c, label("View PinableRemote releases", "查看 PinableRemote 发布版本")).Clicked() {
				c.OpenURL("https://github.com/" + ownUpdateRepository + "/releases")
			}
		}
		ui.Row(c).Gap(8).Wrap().Children(func() {
			if ui.Button(c, label("Open source license", "开源许可")).Clicked() {
				c.OpenURL("https://github.com/" + ownUpdateRepository + "/blob/main/LICENSE")
			}
			if ui.Button(c, label("Close", "关闭")).Clicked() {
				a.aboutOpen = false
			}
		})
	})
}
