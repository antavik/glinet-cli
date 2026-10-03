// Package auth implements "glinet-cli auth": saving the router password in
// the OS keychain and removing it.
package auth

import (
	"bufio"
	"context"
	"errors"
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

func parse(args []string) cmd.Action {
	switch {
	case len(args) == 0, len(args) == 1 && args[0] == "login":
		return login
	case len(args) == 1 && args[0] == "logout":
		return logout
	}
	return nil
}

// login asks for the password, checks it with the router and saves it in
// the OS keychain.
func login(ctx context.Context, cfg config.Config) error {
	password, err := readPassword(ctx, fmt.Sprintf("Password for %s: ", cfg.Account()))
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
	fmt.Println("Logged in. Password saved to the OS keychain.")
	return nil
}

// logout removes the saved password.
func logout(_ context.Context, cfg config.Config) error {
	deleted, err := keychain.Delete(cfg.Account())
	switch {
	case err != nil:
		return err
	case deleted:
		fmt.Println("Password removed from the OS keychain.")
	default:
		fmt.Println("No saved password.")
	}
	return nil
}

// readPassword reads one line from stdin. On a terminal it prompts on stderr
// and hides input; piped input lets a password manager feed it. Either way
// Ctrl+C or SIGTERM ends the wait: main catches them, so a read that ignored
// ctx would leave the process hanging on a pipe that never closes.
func readPassword(ctx context.Context, prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	read := func() (string, error) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	if term.IsTerminal(fd) {
		// ReadPassword turns echo off until Enter; turn it back on if the
		// user presses Ctrl+C instead.
		state, err := term.GetState(fd)
		if err != nil {
			return "", err
		}
		defer func() {
			_ = term.Restore(fd, state) // Best effort: the read's error is the one to report.
			fmt.Fprintln(os.Stderr)
		}()
		fmt.Fprint(os.Stderr, prompt)
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
