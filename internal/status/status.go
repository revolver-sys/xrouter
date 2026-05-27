package status

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/revolver-sys/xrouter/internal/config"
	"github.com/revolver-sys/xrouter/internal/healthcheck"
	"github.com/revolver-sys/xrouter/internal/transportctl"
)

type Snapshot struct {
	TimeUTC string `json:"time_utc"`

	ConfigPath string `json:"config_path"`

	Transport         *transportctl.Status `json:"transport"`
	TransportExternal *transportctl.Status `json:"transport_external"`

	UTUNs []string `json:"utuns"`

	PFEnabled bool   `json:"pf_enabled"`
	PFInfo    string `json:"pf_info"`
	PFErr     string `json:"pf_err"`

	Health healthcheck.Result `json:"health"`
}

func Collect(ctx context.Context, cfg *config.Config, cfgPath string, healthTimeout time.Duration) Snapshot {
	s := Snapshot{
		TimeUTC:    time.Now().UTC().Format(time.RFC3339),
		ConfigPath: cfgPath,
	}

	// transport (owned pidfile status)
	sb, _ := transportctl.Inspect(cfg)
	s.Transport = sb

	ext, _ := transportctl.InspectExternal(ctx, cfg)
	s.TransportExternal = ext

	// utun list (all)
	if us, err := ListUTUN(); err == nil {
		s.UTUNs = us
	}

	// pf info (best-effort)
	s.PFEnabled, s.PFInfo, s.PFErr = pfInfo(ctx)

	// healthcheck (always)
	s.Health = healthcheck.Check(ctx, cfg.HealthCheckURL, healthTimeout)

	return s
}

func pfInfo(ctx context.Context) (enabled bool, info string, errStr string) {
	cmd := exec.CommandContext(ctx, "pfctl", "-s", "info")

	var out bytes.Buffer
	var errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb

	err := cmd.Run()
	info = strings.TrimSpace(out.String())

	if err != nil {
		// Not fatal: user might not be root, or pfctl might be restricted.
		errStr = strings.TrimSpace(errb.String())
		if errStr == "" {
			errStr = err.Error()
		}
		return false, info, errStr
	}

	// Parse a simple indicator
	// "Status: Enabled" appears on macOS
	if strings.Contains(info, "Status: Enabled") || strings.Contains(info, "Enabled") {
		enabled = true
	}
	return enabled, info, ""
}
