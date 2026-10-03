// Package web implements "glinet-cli web": open the router web interface in
// the default browser.
package web

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/config"
)

func init() {
	cmd.Register(cmd.Command{
		Name:  "web",
		Usage: []string{"web\topen the router web interface in browser"},
		Parse: parse,
	})
}

func parse(args []string) cmd.Action {
	if len(args) != 0 {
		return nil
	}
	return open
}

func open(ctx context.Context, cfg config.Config) error {
	return launch(ctx, runtime.GOOS, cfg.URL, execRunner)
}

// runner starts the program name with args and waits for it to finish.
type runner func(ctx context.Context, name string, args ...string) error

func execRunner(ctx context.Context, name string, args ...string) error {
	// The URL is validated as http(s) with a host by launch before it reaches
	// this point, so it cannot name another program or scheme.
	return exec.CommandContext(ctx, name, args...).Run() //nolint:gosec // G204: see comment above
}

// launch opens routerURL in the default browser of the operating system goos
// by calling run. It rejects anything but an http or https URL with a host
// before running anything, so the URL cannot name another program or scheme.
func launch(ctx context.Context, goos, routerURL string, run runner) error {
	u, err := url.Parse(routerURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("web: invalid router URL %q", routerURL)
	}
	name, args, err := browserCommand(goos, routerURL)
	if err != nil {
		return err
	}
	return run(ctx, name, args...)
}

// browserCommand returns the program and arguments that open target in the
// default browser on the operating system goos.
func browserCommand(goos, target string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "open", []string{target}, nil
	case "linux":
		return "xdg-open", []string{target}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", target}, nil
	default:
		return "", nil, fmt.Errorf("web: unsupported platform %q", goos)
	}
}
