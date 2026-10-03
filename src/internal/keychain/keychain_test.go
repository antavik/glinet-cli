package keychain

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

const account = "root@http://192.168.8.1"

func TestPassword(t *testing.T) {
	keyring.MockInit()
	t.Setenv("GLINET_PASSWORD", "")

	if _, err := Password(account); err == nil || !strings.Contains(err.Error(), `run "glinet-cli auth"`) {
		t.Fatalf("no saved password: error = %v, want hint to run auth", err)
	}

	if err := Save(account, "saved"); err != nil {
		t.Fatal(err)
	}
	if got, err := Password(account); err != nil || got != "saved" {
		t.Fatalf("saved password: got %q, %v; want %q", got, err, "saved")
	}

	t.Setenv("GLINET_PASSWORD", "from-env")
	if got, err := Password(account); err != nil || got != "from-env" {
		t.Fatalf("env override: got %q, %v; want %q", got, err, "from-env")
	}
}

func TestDelete(t *testing.T) {
	keyring.MockInit()
	if err := Save(account, "saved"); err != nil {
		t.Fatal(err)
	}

	if deleted, err := Delete(account); !deleted || err != nil {
		t.Fatalf("Delete() = %v, %v; want true, nil", deleted, err)
	}
	if deleted, err := Delete(account); deleted || err != nil {
		t.Fatalf("Delete() without saved password = %v, %v; want false, nil", deleted, err)
	}
}

func TestUnavailableHint(t *testing.T) {
	unavailable := errors.New("no keychain service")
	keyring.MockInitWithError(unavailable)
	t.Setenv("GLINET_PASSWORD", "")

	err := Save(account, "saved")
	if !errors.Is(err, unavailable) || !strings.Contains(err.Error(), "GLINET_PASSWORD") {
		t.Errorf("Save() error = %v, want wrapped error with GLINET_PASSWORD hint", err)
	}
	_, err = Password(account)
	if !errors.Is(err, unavailable) || !strings.Contains(err.Error(), "GLINET_PASSWORD") {
		t.Errorf("Password() error = %v, want wrapped error with GLINET_PASSWORD hint", err)
	}
}
