package control

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/revolver-sys/xrouter/internal/debugdump"
)

type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func RunScript(ctx context.Context, path string, timeout time.Duration, args ...string) (*Result, error) {
	// 'args ...string' is a slice of strings → “zero or more string arguments”
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cctx, path, args...)
	// 'args...' means unpack the slice back into arguments → “expand a slice into arguments"

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	res := &Result{
		ExitCode: exitCode(err),
		Stdout:   strings.TrimSpace(stdout.String()),
		Stderr:   strings.TrimSpace(stderr.String()),
	}

	// Log everything in one place (useful for debugging).
	// log.Printf("run %q exit=%d\nstdout:\n%s\nstderr:\n%s", path, res.ExitCode, res.Stdout, res.Stderr)

	log.Printf("run %q exit=%d", path, res.ExitCode)

	if debugdump.Enabled() {
		debugdump.Dump("script_stdout", res.Stdout)
		debugdump.Dump("script_stderr", res.Stderr)
	}

	if cctx.Err() == context.DeadlineExceeded {
		return res, fmt.Errorf("command timed out after %s: %s", timeout, path)
	}
	if err != nil {
		return res, fmt.Errorf("command failed (exit=%d): %s", res.ExitCode, path)
	}
	return res, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	// For errors like "file not found", "permission denied", etc.
	return -1
}
