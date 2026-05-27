package transportctl

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/revolver-sys/xrouter/internal/config"
)

func StopOwned(ctx context.Context, cfg *config.Config, timeout time.Duration) error {
	pid, ok := readPID(cfg.TransportPidFile)
	if !ok {
		return fmt.Errorf("pidfile not found: %s", cfg.TransportPidFile)
	}
	if !processAlive(pid) {
		_ = os.Remove(cfg.TransportPidFile)
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}

	// polite stop first
	_ = proc.Signal(syscall.SIGTERM)

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()

	for {
		if !processAlive(pid) {
			_ = os.Remove(cfg.TransportPidFile)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			// force kill
			_ = proc.Signal(syscall.SIGKILL)
			time.Sleep(200 * time.Millisecond)
			_ = os.Remove(cfg.TransportPidFile)
			return fmt.Errorf("transport did not stop in time; killed pid=%d", pid)
		case <-tick.C:
		}
	}
}

func RestartOwned(ctx context.Context, cfg *config.Config) (*Status, error) {
	// Stop if owned; ignore if not running.
	_ = StopOwned(ctx, cfg, cfg.TransportStopTimeout)

	// Start / ensure running again (this should create a new utun)
	return EnsureRunning(ctx, cfg, cfg.TransportStartTimeout)
}
