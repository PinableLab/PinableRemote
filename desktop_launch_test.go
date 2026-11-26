package main

import "testing"

func TestDesktopAppearance(t *testing.T) {
	for _, tt := range []struct{ name, saved, managed, provided, want string }{
		{"standalone", "light", "", "dark", "light"},
		{"unknown parent", "light", "yes", "dark", "light"},
		{"dark", "light", "1", "dark", "dark"},
		{"light", "dark", "1", "light", "light"},
		{"system", "dark", "1", "system", ""},
		{"invalid", "dark", "1", "invalid", "dark"},
		{"absent", "dark", "1", "", "dark"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := desktopAppearance(tt.saved, tt.managed, tt.provided); got != tt.want {
				t.Fatalf("appearance = %q, want %q", got, tt.want)
			}
		})
	}
}
func TestDesktopManagedIdentityAndUpdater(t *testing.T) {
	if desktopBuildName("1") != "PinableRemote" || desktopBuildName("") != appName {
		t.Fatal("incorrect app name for the launch mode")
	}
	if useStandaloneUpdater("1", false) || useStandaloneUpdater("", true) || !useStandaloneUpdater("", false) {
		t.Fatal("only a non-profiling standalone launch should check for independent updates")
	}
}
