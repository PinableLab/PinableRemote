package main

import (
	"strings"
	"testing"
)

func TestDesktopVNCBootstrapRejectsInvalidOrUnverifiedPrivacy(t *testing.T) {
	pin := strings.Repeat("a1", 32)
	valid := `{"host":"relay.example.test","port":7443,"server_name":"relay.example.test","spki_sha256":"` + pin + `","require_screen_privacy":false}`
	id, err := readDesktopVNCBootstrap(strings.NewReader(valid))
	if err != nil || id.Host != "relay.example.test" || id.Port != 7443 || id.ServerName != id.Host || id.SPKISHA256[0] != 0xa1 {
		t.Fatalf("bad bootstrap: %+v %v", id, err)
	}
	for i, bad := range []string{
		strings.Replace(valid, "false", "true", 1),
		strings.Replace(valid, pin, strings.Repeat("00", 32), 1),
		strings.Replace(valid, pin, "xx"+pin[2:], 1),
		strings.Replace(valid, "7443", "-1", 1),
		strings.Replace(valid, "relay.example.test", "https://host", 1),
		valid + "{}",
		strings.Repeat(" ", 4096) + valid,
		strings.Replace(valid, "false}", `false,"password":"bad"}`, 1),
	} {
		if _, err := readDesktopVNCBootstrap(strings.NewReader(bad)); err == nil {
			t.Errorf("accepted invalid bootstrap case %d", i)
		}
	}
}

func TestManagedDesktopInvocationDoesNotAcceptExtraArguments(t *testing.T) {
	if got := runManagedDesktopVNC([]string{managedDesktopSwitch, "other"}); got != 2 {
		t.Fatalf("exit = %d", got)
	}
}
