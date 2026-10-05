package main

import (
	"slices"
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
		{"vpn", "restart", "all"},
		{"vpn", "restart", "2001"},
		{"vpn", "restart", "Home/WG"},
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
		{"vpn", "restart"},
		{"vpn", "restart", "a", "b"},
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

func TestExtractWait(t *testing.T) {
	tests := []struct {
		name string
		args []string
		rest []string
		wait bool
	}{
		{"absent", []string{"vpn", "restart", "all"}, []string{"vpn", "restart", "all"}, false},
		{"trailing", []string{"vpn", "restart", "all", "-wait"}, []string{"vpn", "restart", "all"}, true},
		{"middle", []string{"vpn", "-wait", "restart", "all"}, []string{"vpn", "restart", "all"}, true},
		{"double dash", []string{"vpn", "on", "2001", "--wait"}, []string{"vpn", "on", "2001"}, true},
		{"repeated", []string{"-wait", "vpn", "--wait", "status"}, []string{"vpn", "status"}, true},
		{"empty", nil, nil, false},
		{"lookalike kept", []string{"vpn", "on", "-waiting"}, []string{"vpn", "on", "-waiting"}, false},
		{"only flag", []string{"-wait"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rest, wait := extractWait(tt.args)
			if !slices.Equal(rest, tt.rest) {
				t.Errorf("extractWait(%q) rest = %q, want %q", tt.args, rest, tt.rest)
			}
			if wait != tt.wait {
				t.Errorf("extractWait(%q) wait = %v, want %v", tt.args, wait, tt.wait)
			}
		})
	}
}

// -wait after a subcommand must still reach a valid action:
// "vpn restart all -wait" parses like "vpn restart all".
func TestExtractWaitThenParseCommand(t *testing.T) {
	rest, wait := extractWait([]string{"vpn", "restart", "all", "-wait"})
	if !wait {
		t.Error("extractWait() wait = false, want true")
	}
	if parseCommand(rest) == nil {
		t.Errorf("parseCommand(%q) = nil, want action", rest)
	}
}
