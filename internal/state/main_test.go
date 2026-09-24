package state

import (
	"os"
	"testing"
)

// TestMain points XDG_CONFIG_HOME at a scratch directory so no test reads or
// writes the developer's real ~/.config/tm/config. Tests that need a fresh
// config on top of this call t.Setenv("XDG_CONFIG_HOME", t.TempDir()).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tm-test-config-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
