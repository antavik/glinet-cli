// Package keychain keeps router passwords in the OS keychain, one per
// account (see config.Config.Account).
package keychain

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/zalando/go-keyring"
)

// service names this tool's OS keychain entries.
const service = "glinet-cli"

// hint follows keychain errors. Most come from having no keychain to talk
// to, typically Linux without a Secret Service (headless, SSH, containers),
// where GLINET_PASSWORD is the way in.
var hint = func() string {
	if runtime.GOOS == "linux" {
		return "is a Secret Service such as GNOME Keyring or KWallet running? Without one, set GLINET_PASSWORD"
	}
	return "set GLINET_PASSWORD to skip the keychain"
}()

func Password(account string) (string, error) {
	if p := os.Getenv("GLINET_PASSWORD"); p != "" {
		return p, nil
	}
	p, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", fmt.Errorf(`no password for %s: run "glinet-cli auth" or set GLINET_PASSWORD`, account)
	}
	if err != nil {
		return "", fmt.Errorf("read password from OS keychain: %w (%s)", err, hint)
	}
	return p, nil
}

func Save(account, password string) error {
	if err := keyring.Set(service, account, password); err != nil {
		return fmt.Errorf("save password to OS keychain: %w (%s)", err, hint)
	}
	return nil
}

func Delete(account string) (bool, error) {
	err := keyring.Delete(service, account)
	switch {
	case errors.Is(err, keyring.ErrNotFound):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("remove password from OS keychain: %w", err)
	}
	return true, nil
}
