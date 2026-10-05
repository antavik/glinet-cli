package cmd

import (
	"context"
	"errors"
	"flag"
	"slices"
	"testing"
	"time"

	"github.com/antavik/glinet-cli/src/internal/config"
	"github.com/antavik/glinet-cli/src/internal/glinet"
	"github.com/antavik/glinet-cli/src/internal/glinet/glinettest"
)

func TestWithClientLogsOutAfterCancel(t *testing.T) {
	t.Setenv("GLINET_PASSWORD", glinettest.Password)
	router := glinettest.NewRouter(t, nil)
	cfg := config.Config{URL: router.URL, User: glinettest.User, Timeout: time.Minute}

	ctx, cancel := context.WithCancel(t.Context())
	err := WithClient(func(ctx context.Context, _ *glinet.Client, _ IO) error {
		cancel() // Ctrl+C mid-command
		return ctx.Err()
	})(ctx, cfg, IO{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithClient() error = %v, want context.Canceled", err)
	}
	if !router.LoggedOut() {
		t.Error("WithClient() did not log out after the command was cancelled")
	}
}

func TestWithClientTimeout(t *testing.T) {
	t.Setenv("GLINET_PASSWORD", glinettest.Password)
	cfg := config.Config{URL: glinettest.NewRouter(t, nil).URL, User: glinettest.User, Timeout: 42 * time.Second}

	start := time.Now()
	err := WithClient(func(ctx context.Context, _ *glinet.Client, _ IO) error {
		deadline, ok := ctx.Deadline()
		if !ok {
			return errors.New("context has no deadline")
		}
		// Set between start and now, so it lands in [start+T, now+T].
		if deadline.Before(start.Add(cfg.Timeout)) || deadline.After(time.Now().Add(cfg.Timeout)) {
			return errors.New("deadline is not -timeout after the command started")
		}
		return nil
	})(t.Context(), cfg, IO{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRegister(t *testing.T) {
	saved := registry
	registry = map[string]Command{}
	t.Cleanup(func() { registry = saved })

	parse := func(*flag.FlagSet, []string) (Action, error) { return nil, nil }
	Register(Command{Name: "vpn", Parse: parse})
	Register(Command{Name: "auth", Parse: parse})

	if _, ok := Lookup("vpn"); !ok {
		t.Error(`Lookup("vpn") not found`)
	}
	if _, ok := Lookup("reboot"); ok {
		t.Error(`Lookup("reboot") found an unregistered command`)
	}
	var names []string
	for _, c := range All() {
		names = append(names, c.Name)
	}
	if want := []string{"auth", "vpn"}; !slices.Equal(names, want) {
		t.Errorf("All() names = %q, want %q", names, want)
	}

	for _, c := range []Command{
		{Name: "vpn", Parse: parse}, // duplicate
		{Name: "", Parse: parse},
		{Name: "status"},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Register(%q) did not panic", c.Name)
				}
			}()
			Register(c)
		}()
	}
}
