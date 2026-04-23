package config

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultPath_XDGConfigHomeSet(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG path not used on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdgtest")

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join("/tmp/xdgtest", "dbagent", "config.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultPath_XDGConfigHomeUnset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG path not used on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/tmp/hometest")

	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join("/tmp/hometest", ".config", "dbagent", "config.yaml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDefaultPath_EndsWithConfigYAML(t *testing.T) {
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if !strings.HasSuffix(got, "config.yaml") {
		t.Errorf("expected path to end with config.yaml, got %q", got)
	}
	if !strings.Contains(got, "dbagent") {
		t.Errorf("expected path to contain dbagent directory, got %q", got)
	}
}
