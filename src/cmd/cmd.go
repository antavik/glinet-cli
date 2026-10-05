// Package cmd holds what the CLI commands share. Each command lives in its
// own package under cmd/ and registers itself from init with Register; main
// imports the package for that side effect.
package cmd

import (
	"context"
	"flag"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/antavik/glinet-cli/src/internal/config"
	"github.com/antavik/glinet-cli/src/internal/glinet"
	"github.com/antavik/glinet-cli/src/internal/keychain"
)

// Command is a top-level command, such as "vpn", with its subcommands.
type Command struct {
	Name string
	// Usage has one help line per form of the command: the synopsis, a tab,
	// then a summary, e.g. "vpn [status]\tlist VPN tunnels".
	Usage []string
	// Parse returns the action for args after Name. It adds its own flags to
	// fs, which holds the global ones. Any error means bad usage.
	Parse func(fs *flag.FlagSet, args []string) (Action, error)
}

// registry holds the registered commands by name. Only init functions write
// it, and they run one at a time before main, so it needs no lock.
var registry = map[string]Command{}

// Register makes c available to main. Call it from the command package's
// init. It panics if c has no name or Parse, or if its name is taken.
func Register(c Command) {
	if c.Name == "" || c.Parse == nil {
		panic("cmd: Register needs a Name and a Parse")
	}
	if _, dup := registry[c.Name]; dup {
		panic("cmd: Register called twice for " + c.Name)
	}
	registry[c.Name] = c
}

// Lookup returns the command named name.
func Lookup(name string) (Command, bool) {
	c, ok := registry[name]
	return c, ok
}

// All returns the registered commands sorted by name, the order the help
// shows them.
func All() []Command {
	return slices.SortedFunc(maps.Values(registry), func(a, b Command) int {
		return strings.Compare(a.Name, b.Name)
	})
}

// IO is where a command reads input and writes output. main passes the
// process's standard streams; tests pass buffers.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// Action runs one parsed command.
type Action func(context.Context, config.Config, IO) error

// WithClient wraps fn in an action that logs in to the router first and logs
// out after fn returns, even when ctx is cancelled.
func WithClient(fn func(context.Context, *glinet.Client, IO) error) Action {
	return func(ctx context.Context, cfg config.Config, stdio IO) error {
		password, err := keychain.Password(cfg.Account())
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
		client := glinet.NewClient(cfg.URL)
		if err := client.Login(ctx, cfg.User, password); err != nil {
			return err
		}
		defer client.Logout()
		return fn(ctx, client, stdio)
	}
}
