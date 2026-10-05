package main

import (
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/zalando/go-keyring"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/config"
	"github.com/antavik/glinet-cli/src/internal/glinet/glinettest"
)

// TestMain lets testdata/script run the CLI as "glinet-cli", with an
// in-memory keychain.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"glinet-cli": func() {
			keyring.MockInit()
			main()
		},
	})
}

// defaultTunnels are used unless a script has its own tunnels.json.
var defaultTunnels = []glinettest.Tunnel{
	{ID: 2001, Name: "Home/WG", Enabled: true, Status: 1},
	{ID: 2002, Name: "Work/OVPN"},
	{ID: 2003, Name: "Travel/WG", Enabled: true, Status: 2},
}

// TestScript runs testdata/script, each script against its own fake router.
// "exits <code> <cmd> [args...]" checks an exact exit code.
func TestScript(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/script",
		RequireExplicitExec: true,
		Setup: func(env *testscript.Env) error {
			tunnels := defaultTunnels
			data, err := os.ReadFile(filepath.Join(env.WorkDir, "tunnels.json"))
			switch {
			case err == nil:
				tunnels = nil
				if err := json.Unmarshal(data, &tunnels); err != nil {
					return err
				}
			case !errors.Is(err, fs.ErrNotExist):
				return err
			}
			handlers := glinettest.NewVPN(tunnels...).Handlers()
			handlers["system.get_status"] = func(json.RawMessage) any {
				return map[string]any{"system": map[string]any{"uptime": 90061.5}}
			}
			// The router stops when the script ends.
			router := glinettest.NewRouter(env.T().(testing.TB), handlers)
			env.Setenv("GLINET_URL", router.URL)
			env.Setenv("GLINET_USER", glinettest.User)
			env.Setenv("GLINET_PASSWORD", glinettest.Password)
			return nil
		},
		Cmds: map[string]func(*testscript.TestScript, bool, []string){
			"exits": exits,
		},
	})
}

// exits runs a command like exec and fails unless it exits with code.
func exits(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("unsupported: ! exits")
	}
	if len(args) < 2 {
		ts.Fatalf("usage: exits <code> <command> [args...]")
	}
	want, err := strconv.Atoi(args[0])
	ts.Check(err)
	got := 0
	if err := ts.Exec(args[1], args[2:]...); err != nil {
		exit, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			ts.Fatalf("%s: %v", args[1], err)
		}
		got = exit.ExitCode()
	}
	if got != want {
		ts.Fatalf("%s exited with code %d, want %d", args[1], got, want)
	}
}

func TestParseCommandLine(t *testing.T) {
	valid := [][]string{
		{"auth"},
		{"auth", "login"},
		{"auth", "logout"},
		{"status"},
		{"vpn"},
		{"vpn", "status"},
		{"vpn", "on", "-all"},
		{"vpn", "off", "Home/WG"},
		{"vpn", "restart", "-all"},
		{"vpn", "restart", "2001"},
		{"vpn", "restart", "Home/WG"},
		{"web"},
		// Global flags before the command, after it and after operands.
		{"-timeout", "5s", "vpn", "on", "-all"},
		{"vpn", "-timeout", "5s", "on", "-all"},
		{"vpn", "on", "-all", "-timeout=5s", "-url", "http://x"},
		{"-version"},
		{"vpn", "-version"},
		{"vpn", "-wait", "restart", "-all"},
		{"vpn", "restart", "-all", "-wait"},
		{"vpn", "on", "2001", "--wait"},
	}
	for _, args := range valid {
		var cfg config.Config
		if action, err := parseCommandLine(newFlagSet(&cfg), &cfg, args); action == nil || err != nil {
			t.Errorf("parseCommandLine(%q) = %v, %v; want action", args, action != nil, err)
		}
	}

	invalid := [][]string{
		{"auth", "whoami"},
		{"auth", "logout", "extra"},
		{"reboot"},
		{"status", "extra"},
		{"vpn", "on"},
		{"vpn", "status", "extra"},
		{"vpn", "toggle", "all"},
		{"vpn", "on", "a", "b"},
		{"vpn", "on", "-all", "2001"},
		{"vpn", "on", "-bogus"},
		{"vpn", "restart"},
		{"vpn", "restart", "a", "b"},
		{"web", "extra"},
		{"vpn", "on", "-all", "-bogus"},
		{"vpn", "-timeout", "0s"},
		{"-wait", "vpn", "on", "2001"},
		{"status", "-wait"},
		{"vpn", "-wait"},
		{"vpn", "off", "-all", "-wait"},
	}
	for _, args := range invalid {
		var cfg config.Config
		if action, err := parseCommandLine(newFlagSet(&cfg), &cfg, args); action != nil || err == nil {
			t.Errorf("parseCommandLine(%q) = action, want error", args)
		}
	}

	var cfg config.Config
	if _, err := parseCommandLine(newFlagSet(&cfg), &cfg, nil); !errors.Is(err, errNoCommand) {
		t.Errorf("parseCommandLine(nil) error = %v, want errNoCommand", err)
	}
	if _, err := parseCommandLine(newFlagSet(&cfg), &cfg, []string{"vpn", "-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("parseCommandLine(vpn -h) error = %v, want flag.ErrHelp", err)
	}
}

// TestCommandFlags catches command flags that clash with global ones.
func TestCommandFlags(t *testing.T) {
	for _, c := range cmd.All() {
		var cfg config.Config
		_, _ = parseCommandLine(newFlagSet(&cfg), &cfg, []string{c.Name})
	}
}
