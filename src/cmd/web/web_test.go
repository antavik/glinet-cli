package web

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestBrowserCommand(t *testing.T) {
	tests := []struct {
		name     string
		goos     string
		url      string
		wantName string
		wantArgs []string
		wantErr  string
	}{
		{
			name:     "darwin uses open",
			goos:     "darwin",
			url:      "http://10.0.0.5",
			wantName: "open",
			wantArgs: []string{"http://10.0.0.5"},
		},
		{
			name:     "linux uses xdg-open",
			goos:     "linux",
			url:      "http://10.0.0.5",
			wantName: "xdg-open",
			wantArgs: []string{"http://10.0.0.5"},
		},
		{
			name:     "windows uses rundll32",
			goos:     "windows",
			url:      "http://10.0.0.5",
			wantName: "rundll32",
			wantArgs: []string{"url.dll,FileProtocolHandler", "http://10.0.0.5"},
		},
		{
			name:    "unsupported platform",
			goos:    "plan9",
			url:     "http://10.0.0.5",
			wantErr: `web: unsupported platform "plan9"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, args, err := browserCommand(tt.goos, tt.url)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("browserCommand(%q, %q) = (%q, %q, nil), want error %q", tt.goos, tt.url, name, args, tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("browserCommand(%q, %q) error = %q, want %q", tt.goos, tt.url, err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("browserCommand(%q, %q) unexpected error: %v", tt.goos, tt.url, err)
			}
			if name != tt.wantName {
				t.Errorf("browserCommand(%q, %q) name = %q, want %q", tt.goos, tt.url, name, tt.wantName)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("browserCommand(%q, %q) args = %q, want %q", tt.goos, tt.url, args, tt.wantArgs)
			}
		})
	}
}

func TestLaunchValidation(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{name: "empty url", url: ""},
		{name: "no scheme", url: "192.168.8.1"},
		{name: "javascript scheme", url: "javascript:alert(1)"},
		{name: "ftp scheme", url: "ftp://10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			run := func(ctx context.Context, name string, args ...string) error {
				called = true
				return nil
			}
			err := launch(context.Background(), "darwin", tt.url, run)
			if called {
				t.Fatal("launch ran the browser command for an invalid URL; want validation to reject it first")
			}
			if err == nil {
				t.Fatalf("launch(%q) = nil, want error", tt.url)
			}
			want := `web: invalid router URL "` + tt.url + `"`
			if err.Error() != want {
				t.Errorf("launch(%q) error = %q, want %q", tt.url, err.Error(), want)
			}
		})
	}
}

func TestLaunchRunsBrowserCommand(t *testing.T) {
	var gotName string
	var gotArgs []string
	run := func(ctx context.Context, name string, args ...string) error {
		gotName = name
		gotArgs = args
		return nil
	}
	err := launch(context.Background(), "darwin", "https://router.local", run)
	if err != nil {
		t.Fatalf("launch returned unexpected error: %v", err)
	}
	if gotName != "open" {
		t.Errorf("runner name = %q, want %q", gotName, "open")
	}
	if !reflect.DeepEqual(gotArgs, []string{"https://router.local"}) {
		t.Errorf("runner args = %q, want %q", gotArgs, []string{"https://router.local"})
	}
}

func TestLaunchPropagatesRunnerError(t *testing.T) {
	sentinel := errors.New("boom")
	run := func(ctx context.Context, name string, args ...string) error {
		return sentinel
	}
	err := launch(context.Background(), "darwin", "https://router.local", run)
	if !errors.Is(err, sentinel) {
		t.Errorf("launch error = %v, want %v", err, sentinel)
	}
}
