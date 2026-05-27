package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/revolver-sys/xrouter/internal/config"
	"github.com/revolver-sys/xrouter/internal/control"
	"github.com/revolver-sys/xrouter/internal/debugdump"
	"github.com/revolver-sys/xrouter/internal/healthcheck"
	"github.com/revolver-sys/xrouter/internal/status"
	"github.com/revolver-sys/xrouter/internal/transportctl"
)

// const version = "0.2.0"

var version = "dev"

func usage() {
	fmt.Fprintf(os.Stderr, `xrouter - LAN gateway control plane for TUN transport, pf/NAT policy, and fail-closed traffic protection

Usage:
  xrouter up       - start gateway policy and transport supervision
  xrouter down     - stop gateway policy and restore normal network state
  xrouter run      - run watchdog and recovery loop
  xrouter status   - show gateway, transport, pf, and health status
  xrouter -h       - show help

`)
	flag.PrintDefaults()
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("this command must run as root. Use: sudo xrouter <cmd>")
	}
	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	flag.Usage = usage
	showVersion := flag.Bool("version", false, "show version and exit")
	debug := flag.Bool("debug", false, "enable debug dumps (or set XROUTER_DEBUG=1)")
	wanIF := flag.String("wan", "", "override WAN interface (config default if empty)")
	lanIF := flag.String("lan", "", "override LAN interface (config default if empty)")
	healthURL := flag.String("health-url", "", "override watchdog health URL (config default if empty)")
	healthTimeout := flag.Duration("health-timeout", 0, "override watchdog health timeout (e.g. 2s)")

	// Global flag: config path
	defaultCfg, _ := config.DefaultPath()
	cfgPath := flag.String("config", defaultCfg, "path to config file")

	flag.Parse()

	debugdump.EnableFromEnv()
	if *debug {
		debugdump.Enable()
	}

	if *showVersion {
		fmt.Printf("xrouter version %s\n", version)
		return
	}

	if flag.NArg() < 1 {
		usage()
		os.Exit(1)
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Printf("config load failed: %v", err)
		os.Exit(1)
	}

	cmd := flag.Arg(0)

	effectiveHealthTimeout := cfg.HealthTimeout
	// If user provided --health-timeout (before OR after the subcommand), prefer it.
	// We set the flag default to 0 so "not provided" is distinguishable.
	if *healthTimeout != 0 {
		effectiveHealthTimeout = *healthTimeout
	}

	// allow CLI override for watchdog health URL
	effectiveHealthURL := cfg.HealthCheckURL
	if *healthURL != "" {
		effectiveHealthURL = *healthURL
	}

	// Per-run overrides for WAN/LAN IFs
	effectiveWAN := cfg.WANIF
	effectiveLAN := cfg.LANIF

	// Support flags placed *after* the subcommand, e.g.:
	//   xrouter run --health-timeout 2s
	// The standard flag package stops parsing at the first non-flag ("run"),
	// so we parse the remaining args again for run/status.
	if (cmd == "run" || cmd == "status") && len(flag.Args()) > 1 {
		fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
		fs.SetOutput(io.Discard) // avoid noisy output; we show our own messages
		extraHealthTimeout := fs.Duration("health-timeout", effectiveHealthTimeout, "health check timeout (overrides config)")
		extraHealthURL := fs.String("health-url", effectiveHealthURL, "health check URL (overrides config)")
		_ = fs.Parse(flag.Args()[1:])
		if fs.Parsed() {
			effectiveHealthTimeout = *extraHealthTimeout
			effectiveHealthURL = *extraHealthURL
		}
	}

	if *wanIF != "" {
		effectiveWAN = *wanIF
	}

	if *lanIF != "" {
		effectiveLAN = *lanIF
	}

	switch cmd {
	case "up":
		if err := cmdUp(cfg, *cfgPath, effectiveWAN, effectiveLAN); err != nil {
			log.Fatalf("up failed: %v", err)
		}
	case "down":
		if err := cmdDown(cfg); err != nil {
			log.Fatalf("down failed: %v", err)
		}
	case "run":
		if err := cmdRun(cfg, *cfgPath, effectiveHealthTimeout, effectiveHealthURL, effectiveWAN, effectiveLAN); err != nil {
			log.Fatalf("run failed: %v", err)
		}
	case "status":
		if err := cmdStatus(cfg, *cfgPath, effectiveHealthTimeout); err != nil {
			log.Fatalf("status failed: %v", err)
		}
	default:
		log.Printf("unknown command: %q\n", cmd)
		usage()
		os.Exit(1)
	}
}

func cmdUp(cfg *config.Config, cfgPath string, wanIF string, lanIF string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	// 0) Setup LAN + dnsmasq + pf anchors (slow). This script may have its own WAN/LAN defaults.
	setupRes, err := control.RunScript(context.Background(), cfg.GatewaySetupPath, cfg.CommandTimeout)
	if err != nil {
		return formatScriptFailure("setup", setupRes, err)
	}
	printScriptSuccess("setup", setupRes)

	// If WAN/LAN were not provided (config.yaml commented out), try to parse them from setup stdout.
	effectiveWAN := strings.TrimSpace(wanIF)
	effectiveLAN := strings.TrimSpace(lanIF)
	if effectiveWAN == "" || effectiveLAN == "" {
		// Example line: "WAN: en5  LAN: en8"
		re := regexp.MustCompile(`(?m)^WAN:\s*(\S+)\s+LAN:\s*(\S+)\s*$`)
		if m := re.FindStringSubmatch(setupRes.Stdout); len(m) == 3 {
			if effectiveWAN == "" {
				effectiveWAN = m[1]
			}
			if effectiveLAN == "" {
				effectiveLAN = m[2]
			}
		}
	}
	if effectiveWAN == "" || effectiveLAN == "" {
		return fmt.Errorf("wan_if/lan_if not set (config %q). Set them in config.yaml or pass --wan-if/--lan-if", cfgPath)
	}

	// 1) Ensure transport is running (Policy B adoption supported) and get the tunnel interface.
	var utun string
	if cfg.TransportAutoStart {
		st, err := transportctl.EnsureRunning(context.Background(), cfg, cfg.TransportStartTimeout)
		if err != nil {
			return fmt.Errorf("transport ensure running: %w", err)
		}
		log.Printf("[xrouter] transport status: pid=%d owned=%t utun=%q", st.PID, st.OwnedByUs, st.NewUTUN)
		utun = st.NewUTUN
	}
	if utun == "" {
		return fmt.Errorf("no utun interface detected (transport auto-start disabled or failed)")
	}

	// 2) Apply pf NAT + fail-closed rules (fast).
	args := []string{
		fmt.Sprintf("utun=%s", utun),
		fmt.Sprintf("wan=%s", effectiveWAN),
		fmt.Sprintf("lan=%s", effectiveLAN),
		fmt.Sprintf("transport_server_ips=%q", strings.Join(cfg.TransportServerIPs, ",")),
		fmt.Sprintf("wan_dns=%q", strings.Join(cfg.WANDNSIPs, ",")),
		fmt.Sprintf("allow_ntp=%t", cfg.AllowWANNTP),
	}
	log.Printf("[xrouter] pf_apply args: %s", strings.Join(args, " "))
	res, err := control.RunScript(context.Background(), cfg.GatewayPFApplyPath, cfg.CommandTimeout, args...)
	if err != nil {
		return formatScriptFailure("pf_apply", res, err)
	}
	printScriptSuccess("pf_apply", res)

	log.Printf("[xrouter] router UP; utun=%s", utun)
	return nil
}

func cmdDown(cfg *config.Config) error {
	if err := requireRoot(); err != nil {
		return err
	}

	// 0) Stop transport if xrouter owns it
	if err := transportctl.StopIfOwned(cfg); err != nil {
		return fmt.Errorf("transport stop: %w", err)
	}

	// 1) Restore router state
	res, err := control.RunScript(context.Background(), cfg.GatewayDownPath, cfg.CommandTimeout)
	if err != nil {
		return formatScriptFailure("down", res, err)
	}
	printScriptSuccess("down", res)
	return nil
}

func cmdRun(cfg *config.Config, cfgPath string, healthTimeout time.Duration, healthURL string, effectiveWAN, effectiveLAN string) error {
	if err := requireRoot(); err != nil {
		return err
	}

	// If caller overrides health timeout (CLI), use it as the polling interval too.
	// (Otherwise user sees "timeout=2s" but still waits 10s between checks.)
	interval := cfg.CheckInterval
	if healthTimeout > 0 {
		interval = healthTimeout
	}

	log.Printf("watchdog running; interval=%s health_url=%s failure_threshold=%d",
		interval, healthURL, cfg.FailureThreshold)

	t := time.NewTicker(interval)
	defer t.Stop()

	consecutiveFails := 0
	recoveries := 0

	for {
		h := healthcheck.CheckExpected(context.Background(), healthURL, healthTimeout, cfg.TransportServerIPs)
		debugdump.Dump("health", h)

		if h.OK {
			if consecutiveFails > 0 {
				log.Printf("health recovered after %d fails; body=%q latency=%s", consecutiveFails, h.Body, h.Latency)
			}
			consecutiveFails = 0
		} else {
			consecutiveFails++
			log.Printf("health FAIL #%d: status=%d err=%q body=%q latency=%s",
				consecutiveFails, h.StatusCode, h.Err, h.Body, h.Latency)
		}

		if consecutiveFails >= cfg.FailureThreshold {
			if recoveries >= cfg.MaxRecoveries {
				log.Printf("recovery budget exhausted (recoveries=%d); manual intervention required", recoveries)
			} else {
				recoveries++
				log.Printf("attempting recovery #%d...", recoveries)

				// (Optional) snapshot before recovery
				snap := status.Collect(context.Background(), cfg, cfgPath, healthTimeout)
				debugdump.Dump("status_before_recover", snap)

				recErr := doRecovery(context.Background(), cfg, effectiveWAN, effectiveLAN)
				if recErr != nil {
					log.Printf("recovery #%d failed: %v", recoveries, recErr)
				} else {
					log.Printf("recovery #%d executed", recoveries)
				}

				time.Sleep(cfg.RecoverCooldown)

				h2 := healthcheck.CheckExpected(context.Background(), healthURL, healthTimeout, cfg.TransportServerIPs)
				debugdump.Dump("health_after_recover", h2)
				if h2.OK {
					if recErr == nil {
						log.Printf("recovery #%d succeeded; health OK", recoveries)
					} else {
						log.Printf("health OK after failed recovery #%d (not counted as recovery success)", recoveries)
					}
					consecutiveFails = 0
				} else {
					log.Printf("recovery #%d did not restore health: status=%d err=%q body=%q",
						recoveries, h2.StatusCode, h2.Err, h2.Body)
				}
			}
		}

		<-t.C
	}
}

func cmdStatus(cfg *config.Config, cfgPath string, healthTimeout time.Duration) error {
	s := status.Collect(context.Background(), cfg, cfgPath, healthTimeout)

	// Human-friendly lines
	fmt.Printf("[xrouter] time: %s\n", s.TimeUTC)
	fmt.Printf("[xrouter] config: %s\n", s.ConfigPath)

	if s.Transport != nil && s.Transport.OwnedByUs {
		fmt.Printf("[xrouter] transport: owned pid=%d running=%v utun_hint=%q\n",
			s.Transport.PID, s.Transport.Running, s.Transport.NewUTUN)
	} else {
		fmt.Printf("[xrouter] transport: owned pidfile missing\n")
	}

	if s.TransportExternal != nil && s.TransportExternal.Running {
		fmt.Printf("[xrouter] transport: external pid=%d running=%v (matches config)\n",
			s.TransportExternal.PID, s.TransportExternal.Running)
	}

	if len(s.UTUNs) > 0 {
		fmt.Printf("[xrouter] utuns: %v\n", s.UTUNs)
	} else {
		fmt.Printf("[xrouter] utuns: none\n")
	}

	fmt.Printf("[xrouter] pf: enabled=%v\n", s.PFEnabled)
	if s.PFErr != "" {
		fmt.Printf("[xrouter] pf err: %s\n", s.PFErr)
	}

	fmt.Printf("[xrouter] health: ok=%v status=%d latency=%s body=%q err=%q\n",
		s.Health.OK, s.Health.StatusCode, s.Health.Latency, s.Health.Body, s.Health.Err)

	// Optional debug dump (full struct)
	debugdump.Dump("status_snapshot", s)

	return nil
}

// helper functions

func printScriptSuccess(tag string, res *control.Result) {
	// Minimal user-friendly output.
	// Logs already contain full details.
	if res == nil {
		fmt.Printf("[xrouter] %s: ok\n", tag)
		return
	}

	if res.Stdout != "" && res.Stderr != "" {
		fmt.Printf("[xrouter] %s: ok\nstdout:\n%s\nstderr:\n%s\n", tag, res.Stdout, res.Stderr)
		return
	}
	if res.Stdout != "" {
		fmt.Printf("[xrouter] %s: ok\n%s\n", tag, res.Stdout)
		return
	}
	if res.Stderr != "" {
		fmt.Printf("[xrouter] %s: ok\n%s\n", tag, res.Stderr)
		return
	}

	fmt.Printf("[xrouter] %s: ok\n", tag)
}

func formatScriptFailure(tag string, res *control.Result, err error) error {
	// Build a rich error message that includes captured outputs.
	if res == nil {
		return fmt.Errorf("%s: %w", tag, err)
	}

	msg := fmt.Sprintf("%s failed: %v (exit=%d)", tag, err, res.ExitCode)
	if res.Stdout != "" {
		msg += "\nstdout:\n" + res.Stdout
	}
	if res.Stderr != "" {
		msg += "\nstderr:\n" + res.Stderr
	}
	return fmt.Errorf("%s", msg)
}
