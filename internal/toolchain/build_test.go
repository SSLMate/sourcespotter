package toolchain

import (
	"testing"
)

func TestModernBootstrapLang(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"go1.21.5", ""},
		{"go1.24rc2", "go1.22"},
		{"go1.24.0", "go1.22"},
		{"go1.24.4", "go1.22"},
		{"go1.25rc1", "go1.22"},
		{"go1.25.0", "go1.22"},
		{"go1.25.5", "go1.22"},
		{"go1.26rc2", "go1.24"},
		{"go1.26.0", "go1.24"},
		{"go1.26.5", "go1.24"},
		{"go1.27rc3", "go1.24"},
		{"go1.27.0", "go1.24"},
		{"go1.27.5", "go1.24"},
	}

	for _, test := range tests {
		result := modernBootstrapLang(test.input)
		if result != test.expected {
			t.Errorf("modernBootstrapLang(%q) = %q; want %q", test.input, result, test.expected)
		}
	}
}
