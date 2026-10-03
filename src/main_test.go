package main

import (
	"strings"
	"testing"

	"github.com/antavik/glinet-cli/src/cmd"
)

func TestParseCommand(t *testing.T) {
	valid := [][]string{
		{"auth"},
		{"auth", "login"},
		{"auth", "logout"},
		{"status"},
		{"vpn"},
		{"vpn", "status"},
		{"vpn", "on", "all"},
		{"vpn", "off", "Home/WG"},
		{"web"},
	}
	for _, args := range valid {
		if parseCommand(args) == nil {
			t.Errorf("parseCommand(%q) = nil, want action", args)
		}
	}

	invalid := [][]string{
		nil,
		{"auth", "whoami"},
		{"reboot"},
		{"status", "extra"},
		{"vpn", "on"},
		{"vpn", "toggle", "all"},
		{"vpn", "on", "a", "b"},
		{"web", "extra"},
	}
	for _, args := range invalid {
		if parseCommand(args) != nil {
			t.Errorf("parseCommand(%q) = action, want nil", args)
		}
	}
}

func TestCommands(t *testing.T) {
	for _, c := range cmd.All() {
		if len(c.Usage) == 0 {
			t.Errorf("command %q: needs Usage", c.Name)
		}
		for _, u := range c.Usage {
			if !strings.HasPrefix(u, c.Name) || !strings.Contains(u, "\t") {
				t.Errorf("command %q: usage %q, want %q, a tab, then a summary", c.Name, u, c.Name+"...")
			}
		}
	}
}
