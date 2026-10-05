// Command glinet-cli shows status and controls VPN tunnels on a GL.iNet
// router running firmware 4.8 or later.
package main

import (
	"context"
	"errors"
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
	fs := newFlagSet(&cfg)
	action, err := parseCommandLine(fs, &cfg, os.Args[1:])
	switch {
	case errors.Is(err, flag.ErrHelp):
		printUsage(os.Stderr, fs)
		return
	case errors.Is(err, errNoCommand):
		printUsage(os.Stderr, fs)
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(os.Stderr, "glinet-cli:", err)
		printUsage(os.Stderr, fs)
		os.Exit(2)
	}

	if err := run(cfg, action); err != nil {
		fmt.Fprintln(os.Stderr, "glinet-cli:", err)
		os.Exit(1)
	}
}

// newFlagSet defines the global flags, filling cfg. A flag set of its own,
// not flag.CommandLine, keeps flags other packages register (such as the test
// binary's) out of the help. It prints nothing itself: main reports parse
// errors and help once, with printUsage.
func newFlagSet(cfg *config.Config) *flag.FlagSet {
	fs := flag.NewFlagSet("glinet-cli", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg.RegisterFlags(fs)
	return fs
}

// errNoCommand reports a command line without a command. main answers it
// with the help alone.
var errNoCommand = errors.New("no command")

// parseCommandLine reads the global flags, the command name, then the
// command's arguments, among which global flags are accepted too. It returns
// flag.ErrHelp for -h. Any other error means bad usage.
func parseCommandLine(fs *flag.FlagSet, cfg *config.Config, argv []string) (cmd.Action, error) {
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(argv); err != nil {
		return nil, err
	}
	if *showVersion {
		return printVersion, nil
	}
	if fs.NArg() == 0 {
		return nil, errNoCommand
	}
	c, ok := cmd.Lookup(fs.Arg(0))
	if !ok {
		return nil, fmt.Errorf("unknown command %q", fs.Arg(0))
	}
	action, err := c.Parse(fs, fs.Args()[1:])
	if err != nil {
		return nil, err
	}
	if *showVersion { // "vpn -version"
		return printVersion, nil
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("-timeout must be positive")
	}
	return action, nil
}

func printVersion(_ context.Context, _ config.Config, stdio cmd.IO) error {
	_, err := fmt.Fprintln(stdio.Out, "glinet-cli", version)
	return err
}

func run(cfg config.Config, action cmd.Action) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return action(ctx, cfg, cmd.IO{In: os.Stdin, Out: os.Stdout, Err: os.Stderr})
}

func printUsage(w io.Writer, fs *flag.FlagSet) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "Usage: glinet-cli [flags] <command>\n\nCommands:\n")
	for _, c := range cmd.All() {
		for _, u := range c.Usage {
			fmt.Fprintf(tw, "  %s\n", u)
		}
	}
	fmt.Fprint(tw, "\nEnvironment:\n  GLINET_PASSWORD\trouter password; overrides the saved one\n\nFlags:\n")
	_ = tw.Flush() // Nowhere to report a failed write of the help text.
	fs.SetOutput(w)
	fs.PrintDefaults()
}
