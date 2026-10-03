package status

import (
	"testing"
	"time"
)

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{0, "0d 0h 0m"},
		{59 * time.Second, "0d 0h 0m"},
		{90 * time.Minute, "0d 1h 30m"},
		{3*24*time.Hour + 4*time.Hour + 5*time.Minute + 6*time.Second, "3d 4h 5m"},
	}
	for _, tt := range tests {
		if got := formatUptime(tt.in); got != tt.want {
			t.Errorf("formatUptime(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
