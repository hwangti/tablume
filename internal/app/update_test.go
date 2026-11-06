package app

import "testing"

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		latest, current string
		want            bool
	}{
		{"1.0.0", "0.9.0", true},
		{"0.9.1", "0.9.0", true},
		{"0.10.0", "0.9.9", true},
		{"0.9.0", "0.9.0", false},
		{"0.8.9", "0.9.0", false},
		{"v1.0.0", "0.9.0", true},
		{"1.0.0", "1.0.0-rc1", true},
		{"1.0.0-rc2", "1.0.0-rc1", true},
		{"1.0.0-rc1", "1.0.0", false},
		{"1.0.0", "dev", false},
		{"1.0", "0.9.0", false},
		{"1.0.0+build5", "0.9.0", true},
	} {
		if got := newerVersion(c.latest, c.current); got != c.want {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.latest, c.current, got, c.want)
		}
	}
}
