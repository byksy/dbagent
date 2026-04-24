package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/byksy/dbagent/internal/config"
	"github.com/spf13/cobra"
)

// newConfigTestFile writes a valid config to a temp dir and returns
// the path. Keeps each test case independent of the others.
func newConfigTestFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := config.Default()
	cfg.Database.Password = "hunter2"
	if err := config.Save(cfg, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return path
}

// newCmdForTest builds a bare cobra command with connected I/O
// buffers. Resets flagConfigPath so the --config override the test
// set doesn't leak into the next one.
func newCmdForTest(t *testing.T, stdin string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := &cobra.Command{}
	var stdout, stderr bytes.Buffer
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	t.Cleanup(func() { flagConfigPath = "" })
	return cmd, &stdout, &stderr
}

func TestConfigShow_ExistingConfig(t *testing.T) {
	path := newConfigTestFile(t)
	flagConfigPath = path

	cmd, stdout, _ := newCmdForTest(t, "")
	if err := runConfigShow(cmd); err != nil {
		t.Fatalf("runConfigShow: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "host: localhost") {
		t.Errorf("output missing host line:\n%s", out)
	}
	if !strings.Contains(out, "password: ***") {
		t.Errorf("output missing redaction marker:\n%s", out)
	}
	if strings.Contains(out, "hunter2") {
		t.Errorf("output leaked raw password:\n%s", out)
	}
	if !strings.Contains(out, "# Config file: "+path) {
		t.Errorf("output missing config-path footer:\n%s", out)
	}
}

func TestConfigShow_MissingConfig(t *testing.T) {
	dir := t.TempDir()
	flagConfigPath = filepath.Join(dir, "absent.yaml")

	cmd, _, _ := newCmdForTest(t, "")
	err := runConfigShow(cmd)
	if err == nil {
		t.Fatalf("expected ExitError, got nil")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitNoConfig {
		t.Errorf("expected ExitNoConfig, got %v", err)
	}
	if !strings.Contains(err.Error(), "dbagent init") {
		t.Errorf("error should point the user to init, got %q", err.Error())
	}
}

func TestConfigPath_PrintsResolvedPath(t *testing.T) {
	path := newConfigTestFile(t)
	flagConfigPath = path

	cmd, stdout, stderr := newCmdForTest(t, "")
	if err := runConfigPath(cmd); err != nil {
		t.Fatalf("runConfigPath: %v", err)
	}
	got := strings.TrimRight(stdout.String(), "\n")
	if got != path {
		t.Errorf("stdout = %q, want %q", got, path)
	}
	if strings.Contains(stderr.String(), "does not exist") {
		t.Errorf("stderr should be empty for existing file, got %q", stderr.String())
	}
}

func TestConfigPath_AnnotatesMissingFile(t *testing.T) {
	dir := t.TempDir()
	flagConfigPath = filepath.Join(dir, "absent.yaml")

	cmd, stdout, stderr := newCmdForTest(t, "")
	if err := runConfigPath(cmd); err != nil {
		t.Fatalf("runConfigPath: %v", err)
	}
	if strings.TrimRight(stdout.String(), "\n") != flagConfigPath {
		t.Errorf("stdout should still contain the path, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "does not exist") {
		t.Errorf("stderr should annotate missing file, got %q", stderr.String())
	}
}

func TestConfigReset_MissingFile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	flagConfigPath = filepath.Join(dir, "absent.yaml")

	cmd, _, stderr := newCmdForTest(t, "")
	if err := runConfigReset(cmd, &configResetFlags{force: true}); err != nil {
		t.Fatalf("reset on missing file should be idempotent, got %v", err)
	}
	if !strings.Contains(stderr.String(), "no config to reset") {
		t.Errorf("expected informational note, got %q", stderr.String())
	}
}

func TestConfigReset_Force_DeletesFile(t *testing.T) {
	path := newConfigTestFile(t)
	flagConfigPath = path

	cmd, stdout, _ := newCmdForTest(t, "")
	if err := runConfigReset(cmd, &configResetFlags{force: true}); err != nil {
		t.Fatalf("runConfigReset: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected file to be deleted, got stat err %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Config deleted") {
		t.Errorf("output missing success message:\n%s", out)
	}
}

// withNonTTYStdin swaps os.Stdin for the read end of a pipe so
// isTerminal(os.Stdin) returns false deterministically, regardless
// of whether the test runner has a TTY attached. Restored via
// t.Cleanup.
func withNonTTYStdin(t *testing.T) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
		_ = w.Close()
	})
}

func TestConfigReset_NonInteractive_WithoutForce_Fails(t *testing.T) {
	// Guarantee non-TTY stdin even when `go test` is invoked from an
	// interactive terminal — otherwise the test would drive the
	// confirmation prompt path instead of the refusal path.
	withNonTTYStdin(t)

	path := newConfigTestFile(t)
	flagConfigPath = path

	cmd, _, _ := newCmdForTest(t, "")
	err := runConfigReset(cmd, &configResetFlags{force: false})
	if err == nil {
		t.Fatalf("expected usage error, got nil")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitUsageError {
		t.Errorf("expected ExitUsageError, got %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("config file should still exist after refused reset, got %v", statErr)
	}
}

func TestGuardOverwrite_Force(t *testing.T) {
	cmd, _, _ := newCmdForTest(t, "")
	proceed, err := guardOverwrite(cmd, &initFlags{force: true}, "/dev/null")
	if err != nil || !proceed {
		t.Errorf("force should unconditionally proceed, got proceed=%v err=%v", proceed, err)
	}
}

func TestGuardOverwrite_NoPromptRefuses(t *testing.T) {
	cmd, _, _ := newCmdForTest(t, "")
	proceed, err := guardOverwrite(cmd, &initFlags{noPrompt: true}, "/tmp/x")
	if proceed {
		t.Errorf("no-prompt without --force should refuse, got proceed=true")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitUsageError {
		t.Errorf("expected ExitUsageError, got %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force as the fix, got %q", err.Error())
	}
}

func TestInit_RefusesOverwriteOfCorruptConfigWithoutForce(t *testing.T) {
	// Regression for the guard gap Copilot flagged: when Load() fails
	// on an existing-but-unparseable file, the old code left
	// existing=nil and skipped guardOverwrite, silently clobbering
	// whatever was on disk. The fix bases the guard on ConfigExists.
	withNonTTYStdin(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.yaml")
	if err := os.WriteFile(path, []byte("@@@ not valid yaml @@@"), 0o600); err != nil {
		t.Fatal(err)
	}
	flagConfigPath = path

	cmd, _, _ := newCmdForTest(t, "")
	err := runInit(cmd, &initFlags{noPrompt: true})
	if err == nil {
		t.Fatalf("expected refusal, got nil")
	}
	var ee *ExitError
	if !errors.As(err, &ee) || ee.Code != ExitUsageError {
		t.Errorf("expected ExitUsageError, got %v", err)
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error should mention --force as the fix, got %q", err.Error())
	}
	// And the corrupt bytes must still be on disk — no silent clobber.
	b, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("config file vanished: %v", readErr)
	}
	if !strings.Contains(string(b), "not valid yaml") {
		t.Errorf("corrupt config was overwritten; file is now: %s", b)
	}
}

func TestConfirm_YesVariants(t *testing.T) {
	for _, input := range []string{"y\n", "Y\n", "yes\n", "YES\n", " yes \n"} {
		var buf bytes.Buffer
		got, err := confirm(strings.NewReader(input), &buf, "?")
		if err != nil {
			t.Fatalf("confirm(%q): %v", input, err)
		}
		if !got {
			t.Errorf("confirm(%q) = false, want true", input)
		}
	}
}

func TestConfirm_NoAndDefaults(t *testing.T) {
	for _, input := range []string{"", "n\n", "no\n", "\n", "garbage\n"} {
		var buf bytes.Buffer
		got, err := confirm(strings.NewReader(input), &buf, "?")
		if err != nil {
			t.Fatalf("confirm(%q): %v", input, err)
		}
		if got {
			t.Errorf("confirm(%q) = true, want false", input)
		}
	}
}
