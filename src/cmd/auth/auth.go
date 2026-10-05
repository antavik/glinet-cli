// Package auth implements "glinet-cli auth": saving the router password in
// the OS keychain and removing it.
package auth

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/antavik/glinet-cli/src/cmd"
	"github.com/antavik/glinet-cli/src/internal/config"
	"github.com/antavik/glinet-cli/src/internal/glinet"
	"github.com/antavik/glinet-cli/src/internal/keychain"
)

func init() {
	cmd.Register(cmd.Command{
		Name: "auth",
		Usage: []string{
			"auth [login]\tcheck password and save it in the OS keychain",
			"auth logout\tremove the saved password",
		},
		Parse: parse,
	})
}

func parse(fs *flag.FlagSet, args []string) (cmd.Action, error) {
	a, err := cmd.ParseArgs(fs, args)
	if err != nil {
		return nil, err
	}
	var action cmd.Action
	switch a.Sub {
	case "", "login":
		action = login
	case "logout":
		action = logout
	default:
		return nil, fmt.Errorf("unknown auth subcommand %q", a.Sub)
	}
	if len(a.Pos) != 0 {
		return nil, fmt.Errorf("auth %s takes no arguments", a.Sub)
	}
	return action, nil
}

// login asks for the password, checks it with the router and saves it in
// the OS keychain.
func login(ctx context.Context, cfg config.Config, stdio cmd.IO) error {
	password, err := readPassword(ctx, stdio, fmt.Sprintf("Password for %s: ", cfg.Account()))
	if err != nil {
		return err
	}
	if password == "" {
		return errors.New("empty password")
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	client := glinet.NewClient(cfg.URL)
	if err := client.Login(ctx, cfg.User, password); err != nil {
		return err
	}
	client.Logout()

	if err := keychain.Save(cfg.Account(), password); err != nil {
		return err
	}
	fmt.Fprintln(stdio.Out, "Logged in. Password saved to the OS keychain.")
	return nil
}

// logout removes the saved password.
func logout(_ context.Context, cfg config.Config, stdio cmd.IO) error {
	deleted, err := keychain.Delete(cfg.Account())
	switch {
	case err != nil:
		return err
	case deleted:
		fmt.Fprintln(stdio.Out, "Password removed from the OS keychain.")
	default:
		fmt.Fprintln(stdio.Out, "No saved password.")
	}
	return nil
}

// readPassword reads one line from stdio.In, prompting with hidden input on a
// terminal. It returns when ctx ends, so a silent pipe cannot hang the process.
func readPassword(ctx context.Context, stdio cmd.IO, prompt string) (string, error) {
	read := func() (string, error) {
		line, err := bufio.NewReader(stdio.In).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	if f, ok := stdio.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fd := int(f.Fd())
		// ReadPassword turns echo off until Enter; turn it back on if the
		// user presses Ctrl+C instead.
		state, err := term.GetState(fd)
		if err != nil {
			return "", err
		}
		defer func() {
			_ = term.Restore(fd, state) // Best effort: the read's error is the one to report.
			fmt.Fprintln(stdio.Err)
		}()
		fmt.Fprint(stdio.Err, prompt)
		read = func() (string, error) {
			p, err := term.ReadPassword(fd)
			return string(p), err
		}
	}

	type result struct {
		password string
		err      error
	}
	done := make(chan result, 1)
	go func() {
		p, err := read()
		done <- result{p, err}
	}()

	select {
	case r := <-done:
		return r.password, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
