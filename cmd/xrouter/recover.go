package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/revolver-sys/xrouter/internal/config"
	"github.com/revolver-sys/xrouter/internal/control"
	"github.com/revolver-sys/xrouter/internal/debugdump"
	"github.com/revolver-sys/xrouter/internal/transportctl"
)

func doRecovery(ctx context.Context, cfg *config.Config, effectiveWAN, effectiveLAN string) error {
	sb0, _ := transportctl.Inspect(cfg)
	debugdump.Dump("transport_before_recover", sb0)

	// Only restart if we own it. Never kill an external transport.
	if sb0 != nil && sb0.OwnedByUs {
		if err := transportctl.StopIfOwned(cfg); err != nil {
			return fmt.Errorf("stop transport (owned): %w", err)
		}
	}

	sb, err := transportctl.EnsureRunning(ctx, cfg, cfg.TransportStartTimeout)
	if err != nil {
		return fmt.Errorf("ensure transport: %w", err)
	}
	debugdump.Dump("transport_after_ensure", sb)
	if sb == nil || !sb.Running || sb.NewUTUN == "" {
		return fmt.Errorf("transport not running or utun not detected")
	}

	args := []string{
		fmt.Sprintf("utun=%s", sb.NewUTUN),
		fmt.Sprintf("wan=%s", strings.TrimSpace(effectiveWAN)),
		fmt.Sprintf("lan=%s", strings.TrimSpace(effectiveLAN)),
		fmt.Sprintf("transport_server_ips=%q", strings.Join(cfg.TransportServerIPs, ",")),
		fmt.Sprintf("wan_dns=%q", strings.Join(cfg.WANDNSIPs, ",")),
		fmt.Sprintf("allow_ntp=%t", cfg.AllowWANNTP),
	}
	_, err = control.RunScript(ctx, cfg.GatewayPFApplyPath, cfg.CommandTimeout, args...)

	if err != nil {
		return fmt.Errorf("pf_apply: %w", err)
	}
	return nil
}
