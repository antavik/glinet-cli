package cmd

import (
	"errors"
	"flag"
	"io"
	"slices"
	"testing"
	"time"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		args    []string
		sub     string
		pos     []string
		timeout time.Duration
		force   bool
	}{
		{args: nil},
		{args: []string{"on"}, sub: "on"},
		{args: []string{"on", "home", "office"}, sub: "on", pos: []string{"home", "office"}},
		// Flags before, between and after operands.
		{args: []string{"-force", "on", "home"}, sub: "on", pos: []string{"home"}, force: true},
		{args: []string{"on", "-timeout", "5s", "home"}, sub: "on", pos: []string{"home"}, timeout: 5 * time.Second},
		{args: []string{"on", "home", "-force", "-timeout=9s"}, sub: "on", pos: []string{"home"}, timeout: 9 * time.Second, force: true},
		{args: []string{"-force=false", "off"}, sub: "off"},
	}
	for _, tt := range tests {
		fs, timeout, force := testFlagSet()
		a, err := ParseArgs(fs, tt.args)
		if err != nil {
			t.Errorf("ParseArgs(%q) error = %v", tt.args, err)
			continue
		}
		if a.Sub != tt.sub || !slices.Equal(a.Pos, tt.pos) {
			t.Errorf("ParseArgs(%q) = %+v, want Sub %q, Pos %q", tt.args, a, tt.sub, tt.pos)
		}
		if *timeout != tt.timeout || *force != tt.force {
			t.Errorf("ParseArgs(%q) set -timeout %v -force %v, want %v %v", tt.args, *timeout, *force, tt.timeout, tt.force)
		}
	}
}

func TestParseArgsErrors(t *testing.T) {
	for _, args := range [][]string{
		{"on", "-bogus"},
		{"on", "-timeout"},
		{"on", "-timeout", "soon"},
	} {
		fs, _, _ := testFlagSet()
		if _, err := ParseArgs(fs, args); err == nil {
			t.Errorf("ParseArgs(%q) error = nil, want flag error", args)
		}
	}

	fs, _, _ := testFlagSet()
	if _, err := ParseArgs(fs, []string{"on", "-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("ParseArgs(-h) error = %v, want flag.ErrHelp", err)
	}
}

func testFlagSet() (*flag.FlagSet, *time.Duration, *bool) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs, fs.Duration("timeout", 0, ""), fs.Bool("force", false, "")
}
