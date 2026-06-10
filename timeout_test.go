package main

import (
	"testing"
	"time"
)

func TestParseTimeout(t *testing.T) {
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"123", 123 * time.Second},
		{"123s", 123 * time.Second},
		{"123 s", 123 * time.Second},
		{"123m", 123 * time.Minute},
		{"123 min", 123 * time.Minute},
		{"2h", 2 * time.Hour},
		{"2 hours", 2 * time.Hour},
		{"3d", 3 * 24 * time.Hour},
		{"1h 5m", time.Hour + 5*time.Minute},
		{"1h5m", time.Hour + 5*time.Minute},
		{"1d 2h 3m 4s", 24*time.Hour + 2*time.Hour + 3*time.Minute + 4*time.Second},
		{"  10m  ", 10 * time.Minute},
	}
	for _, c := range cases {
		got, err := parseTimeout(c.in)
		if err != nil {
			t.Errorf("parseTimeout(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("parseTimeout(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseTimeoutErrors(t *testing.T) {
	bad := []string{"", "   ", "abc", "10x", "10 foo", "0", "0s", "-5"}
	for _, in := range bad {
		if _, err := parseTimeout(in); err == nil {
			t.Errorf("parseTimeout(%q) expected error, got nil", in)
		}
	}
}
