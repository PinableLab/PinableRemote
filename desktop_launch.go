package main

const (
	desktopManagedEnv       = "PINABLE_REMOTE_DESKTOP"
	commonDesktopManagedEnv = "PINABLE_DESKTOP_MANAGED"
	desktopThemeEnv         = "PINABLE_THEME_MODE"
)

// desktopAppearance returns the active UI mode without rewriting the user's
// standalone preference. An invalid or absent handoff leaves that preference
// intact. The legacy empty string is how the viewer stores "system".
func desktopAppearance(saved, managed, supplied string) string {
	if managed == "1" {
		switch supplied {
		case "system":
			return ""
		case "light", "dark":
			return supplied
		}
	}
	return saved
}

func desktopBuildName(managed string) string {
	if managed == "1" {
		return "PinableRemote"
	}
	return appName
}

func useStandaloneUpdater(managed string, profiling bool) bool {
	return managed != "1" && !profiling
}

// desktopManaged accepts both the specific marker used by older Desktop
// builds and the marker common to current bundled Pinable components.
func desktopManaged(specific, common string) string {
	if specific == "1" || common == "1" {
		return "1"
	}
	return ""
}
