package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/zalando/go-keyring"

	"github.com/antavik/glinet-cli/src/internal/glinet/glinettest"
)

// TestMain lets testdata/script run the real CLI as "glinet-cli": the test
// binary re-runs itself as that command. The keychain is an in-memory mock,
// so scripts never read or write the OS keychain.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"glinet-cli": func() {
			keyring.MockInit()
			main()
		},
	})
}

// defaultTunnels is what the fake router's vpn-client module holds unless a
// script brings its own tunnels.json (router JSON format).
var defaultTunnels = []glinettest.Tunnel{
	{ID: 2001, Name: "Home/WG", Enabled: true, Status: 1},
	{ID: 2002, Name: "Work/OVPN"},
	{ID: 2003, Name: "Travel/WG", Enabled: true, Status: 2},
}

// TestScript runs the end-to-end scripts in testdata/script. Each script gets
// its own fake router with GLINET_URL, GLINET_USER and GLINET_PASSWORD
// pointing at it, and an "exits <code> <command> [args...]" command that
// checks an exact exit code.
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
			// Under testscript.Run, env.T() is this script's testing.TB, so
			// the router stops when the script ends.
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

// exits runs a command like exec and fails unless it exits with the given
// code. Its output is kept for stdout and stderr checks, as with exec.
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

func TestParseCommand(t *testing.T) {
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
		{"vpn", "on", "-all", "2001"},
		{"vpn", "on", "-bogus"},
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
