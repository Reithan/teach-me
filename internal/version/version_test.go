package version

import "testing"

// TestVersion ensures Version returns a non-empty string starting with a digit.
func TestVersion(t *testing.T) {
	v := Version()
	if v == "" {
		t.Fatal("Version() returned empty string")
	}
	// Version must start with a digit.
	if v[0] < '0' || v[0] > '9' {
		t.Fatalf("Version() = %q: does not start with a digit", v)
	}
}
