// Command glinet-cli shows status and controls VPN tunnels on a GL.iNet
// router running firmware 4.8 or later.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/config"

	// commands
	_ "github.com/antavik/glinet-cli/src/cmd/auth"
	_ "github.com/antavik/glinet-cli/src/cmd/status"
	_ "github.com/antavik/glinet-cli/src/cmd/vpn"
	_ "github.com/antavik/glinet-cli/src/cmd/web"
)

// version is set at build time with -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	var cfg config.Config
	cfg.RegisterFlags(flag.CommandLine)
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		printUsage(flag.CommandLine.Output())
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println("glinet-cli", version)
		return
	}

	if cfg.Timeout <= 0 {
		fmt.Fprintln(os.Stderr, "glinet-cli: -timeout must be positive")
		os.Exit(2)
	}

	args, wait := extractWait(flag.Args())
	if wait {
		cfg.Wait = true
	}

	action := parseCommand(args)
	if action == nil {
		flag.Usage()
		os.Exit(2)
	}

	if err := run(cfg, action); err != nil {
		fmt.Fprintln(os.Stderr, "glinet-cli:", err)
		os.Exit(1)
	}
}

// extractWait removes every -wait (or --wait) from args and reports whether
// one was present, so the flag also works after a subcommand:
// "vpn restart all -wait" behaves like "-wait vpn restart all". Commands
// parse their arguments strictly, so the flag must be lifted out first.
func extractWait(args []string) ([]string, bool) {
	var rest []string
	wait := false
	for _, a := range args {
		if a == "-wait" || a == "--wait" {
			wait = true
			continue
		}
		rest = append(rest, a)
	}
	return rest, wait
}

// parseCommand maps command-line arguments to an action, or nil if they are invalid.
func parseCommand(args []string) cmd.Action {
	if len(args) == 0 {
		return nil
	}
	c, ok := cmd.Lookup(args[0])
	if !ok {
		return nil
	}
	return c.Parse(args[1:])
}

func run(cfg config.Config, action cmd.Action) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return action(ctx, cfg)
}

// printUsage writes the help text up to the flag list.
func printUsage(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "Usage: glinet-cli [flags] <command>\n\nCommands:\n")
	for _, c := range cmd.All() {
		for _, u := range c.Usage {
			fmt.Fprintf(tw, "  %s\n", u)
		}
	}
	fmt.Fprint(tw, "\nEnvironment:\n  GLINET_PASSWORD\trouter password; overrides the saved one\n\nFlags:\n")
	_ = tw.Flush() // Nowhere to report a failed write of the help text.
}
