package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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

// handlerOverrides comes from an optional handlers.json in a script's work
// dir, alongside tunnels.json. fail makes the named methods return an error
// at once; block makes them sleep first, so a call made against a short
// -timeout dies on the deadline instead of on the router's error.
type handlerOverrides struct {
	Fail  []string `json:"fail"`
	Block []string `json:"block"`
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
			var overrides handlerOverrides
			data, err = os.ReadFile(filepath.Join(env.WorkDir, "handlers.json"))
			switch {
			case err == nil:
				if err := json.Unmarshal(data, &overrides); err != nil {
					return err
				}
			case !errors.Is(err, fs.ErrNotExist):
				return err
			}
			handlers := glinettest.NewVPN(tunnels...).Handlers()
			handlers["system.get_info"] = func(json.RawMessage) any {
				return map[string]any{
					"model":            "mt3000",
					"mac":              "94:83:C4:0C:74:9A",
					"firmware_version": "4.9.0",
					"board_info":       map[string]any{"hostname": "GL-MT3000-49a"},
				}
			}
			handlers["system.get_status"] = func(json.RawMessage) any {
				return map[string]any{
					"network": []map[string]any{
						{"interface": "wan", "up": true, "online": true},
						{"interface": "wwan", "up": false, "online": false},
						{"interface": "tethering", "up": false, "online": false},
						{"interface": "wan6", "up": false, "online": false},
					},
					"system": map[string]any{
						"uptime":            90061.5,
						"load_average":      []float64{0.12, 0.34, 0.56},
						"memory_total":      536870912,
						"memory_free":       295698432,
						"memory_buff_cache": 33554432,
						"flash_total":       134217728,
						"flash_free":        118489088,
					},
				}
			}
			handlers["upgrade.check_firmware_online"] = func(json.RawMessage) any {
				return map[string]any{"current_version": "4.9.0", "version_new": "4.10.0"}
			}
			handlers["cable.get_status"] = func(json.RawMessage) any {
				return map[string]any{
					"mode":     0,
					"status":   1,
					"protocol": "dhcp",
					"ipv4": map[string]any{
						"ip":      "192.168.1.5",
						"gateway": "192.168.1.1",
						"dns":     []string{"1.1.1.1", "8.8.8.8"},
					},
				}
			}
			for _, m := range overrides.Fail {
				handlers[m] = func(json.RawMessage) any {
					return glinettest.RPCError{Code: -32603, Message: "Internal error"}
				}
			}
			for _, m := range overrides.Block {
				handlers[m] = func(json.RawMessage) any {
					time.Sleep(4 * time.Second)
					return glinettest.RPCError{Code: -32603, Message: "Internal error"}
				}
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
		{"status", "-json"},
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
		// Help.
		{"-h"},
		{"-help"},
		{"help"},
		{"help", "help"},
		{"help", "-h"},
		{"help", "vpn"},
		{"vpn", "-h"},
		{"vpn", "on", "-help"},
		{"-timeout", "0", "vpn", "-h"},
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
		{"help", "reboot"},
		{"help", "vpn", "on"},
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
}

// TestCommandFlags catches command flags that clash with global ones.
func TestCommandFlags(t *testing.T) {
	for _, c := range cmd.All() {
		var cfg config.Config
		_, _ = parseCommandLine(newFlagSet(&cfg), &cfg, []string{c.Name})
	}
}

// TestCommandUsage checks each command's help shows its forms and none of
// the global flags; printCommandUsage relies on Parse defining flags first.
func TestCommandUsage(t *testing.T) {
	for _, c := range cmd.All() {
		var b strings.Builder
		printCommandUsage(&b, c)
		for _, u := range c.Usage {
			synopsis, _, _ := strings.Cut(u, "\t")
			if !strings.Contains(b.String(), "glinet-cli "+synopsis) {
				t.Errorf("%s help lacks %q:\n%s", c.Name, synopsis, b.String())
			}
		}
		if strings.Contains(b.String(), "-url") {
			t.Errorf("%s help shows global flags:\n%s", c.Name, b.String())
		}
	}
}
