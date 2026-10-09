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
	"slices"
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

// newFlagSet defines the global flags into cfg. Its own set keeps other
// packages' flags out of the help; it prints nothing itself.
func newFlagSet(cfg *config.Config) *flag.FlagSet {
	fs := flag.NewFlagSet("glinet-cli", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	cfg.RegisterFlags(fs)
	return fs
}

// errNoCommand means no command was given; main prints the help.
var errNoCommand = errors.New("no command")

// parseCommandLine parses global flags, the command and its arguments.
// -h, -help and "help" give an action that prints help. Any error means bad
// usage.
func parseCommandLine(fs *flag.FlagSet, cfg *config.Config, argv []string) (cmd.Action, error) {
	showVersion := fs.Bool("version", false, "print version and exit")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return helpAction(func(w io.Writer) { printUsage(w, fs) }), nil
		}
		return nil, err
	}
	if *showVersion {
		return printVersion, nil
	}
	if fs.NArg() == 0 {
		return nil, errNoCommand
	}
	if fs.Arg(0) == "help" {
		return parseHelp(fs, fs.Args()[1:])
	}
	c, ok := cmd.Lookup(fs.Arg(0))
	if !ok {
		return nil, fmt.Errorf("unknown command %q", fs.Arg(0))
	}
	// c parses into a copy of the global flags, so its own flags stay out
	// of fs and the top-level help.
	cfs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
	cfs.SetOutput(io.Discard)
	fs.VisitAll(func(f *flag.Flag) { cfs.Var(f.Value, f.Name, f.Usage) })
	action, err := c.Parse(cfs, fs.Args()[1:])
	if errors.Is(err, flag.ErrHelp) {
		return helpAction(func(w io.Writer) { printCommandUsage(w, c) }), nil
	}
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
	fmt.Fprint(w, "\nRun \"glinet-cli help <command>\" for command details.\n")
}

// parseHelp handles "help [command]".
func parseHelp(fs *flag.FlagSet, args []string) (cmd.Action, error) {
	if len(args) > 1 {
		return nil, errors.New("help takes at most one command")
	}
	if len(args) == 0 || slices.Contains([]string{"help", "-h", "-help", "--help"}, args[0]) {
		return helpAction(func(w io.Writer) { printUsage(w, fs) }), nil
	}
	c, ok := cmd.Lookup(args[0])
	if !ok {
		return nil, fmt.Errorf("unknown command %q", args[0])
	}
	return helpAction(func(w io.Writer) { printCommandUsage(w, c) }), nil
}

// helpAction prints requested help to stdout.
func helpAction(printHelp func(io.Writer)) cmd.Action {
	return func(_ context.Context, _ config.Config, stdio cmd.IO) error {
		printHelp(stdio.Out)
		return nil
	}
}

// printCommandUsage prints c's forms and its own flags. c.Parse defines its
// flags on a fresh set, without the global ones; "-h" stops it right after.
func printCommandUsage(w io.Writer, c cmd.Command) {
	fs := flag.NewFlagSet(c.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	_, _ = c.Parse(fs, []string{"-h"})

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "Usage:\n")
	for _, u := range c.Usage {
		fmt.Fprintf(tw, "  glinet-cli %s\n", u)
	}
	_ = tw.Flush()

	hasFlags := false
	fs.VisitAll(func(*flag.Flag) { hasFlags = true })
	if hasFlags {
		fmt.Fprint(w, "\nFlags:\n")
		fs.SetOutput(w)
		fs.PrintDefaults()
	}
}
