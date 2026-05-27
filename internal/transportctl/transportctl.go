package transportctl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/revolver-sys/xrouter/internal/config"
)

// tunNameFromConfig best-effort extracts the TUN interface name from a transport JSON config.
// It looks for an inbound with type=="tun" and a non-empty "interface_name".
func tunNameFromConfig(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return "", err
	}
	inb, ok := root["inbounds"].([]any)
	if !ok {
		return "", nil
	}
	for _, v := range inb {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		t, _ := m["type"].(string)
		if t != "tun" {
			continue
		}
		ifn, _ := m["interface_name"].(string)
		if ifn != "" {
			return ifn, nil
		}
	}
	return "", nil
}

func utunHasIPv4(name string) (bool, error) {
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return false, err
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return false, err
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP == nil {
			continue
		}
		if ip4 := ipnet.IP.To4(); ip4 != nil {
			return true, nil
		}
	}
	return false, nil
}

func waitForUTUNGone(name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := net.InterfaceByName(name)
		if err != nil {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("utun %q still exists after %s", name, timeout)
}

type Status struct {
	Running         bool
	PID             int
	OwnedByUs       bool
	AdoptedExternal bool
	NewUTUN         string
}

func EnsureRunning(ctx context.Context, cfg *config.Config, timeout time.Duration) (*Status, error) {
	// Snapshot current utun interfaces so we can detect a *new* one after we start transport.
	beforeSet, beforeNoIPv4, err := listUTUN()
	if err != nil {
		return nil, fmt.Errorf("list utun (before): %w", err)
	}
	// If transport config pins tun.interface_name (e.g. utun66), prefer waiting for that interface.
	preferUTUN, _ := tunNameFromConfig(cfg.TransportConfigPath)

	// Helper: if transport is already running (owned or external), we usually want the *current* utun,
	// not necessarily a *new* one.
	pickReady := func() (string, error) {
		afterSet, afterNoIPv4, err := listUTUN()
		if err != nil {
			return "", fmt.Errorf("list utun (after): %w", err)
		}
		if utun := pickAnyUTUNWithIPv4(afterSet, afterNoIPv4); utun != "" {
			return utun, nil
		}
		// If no utun has IPv4 yet, wait a bit for one to become ready.
		return waitForUTUNReady(beforeSet, beforeNoIPv4, timeout, preferUTUN)
	}

	// 1) pidfile + alive => owned
	if pid, ok := readPID(cfg.TransportPidFile); ok && processAlive(pid) {
		utun, err := pickReady()
		if err != nil {
			return nil, fmt.Errorf("transport running (owned) but no utun: %w", err)
		}
		return &Status{PID: pid, NewUTUN: utun, OwnedByUs: true, Running: true}, nil
	}

	// 2) Policy B: adopt external if enabled
	if boolVal(cfg.TransportAdoptExternal, true) {
		pid, ok := findExternalTransportPID(cfg)
		if ok && pid > 0 && processAlive(pid) {
			utun, err := pickReady()
			if err != nil {
				return nil, fmt.Errorf("adopted external transport pid=%d but no utun: %w", pid, err)
			}
			return &Status{PID: pid, NewUTUN: utun, OwnedByUs: false, Running: true}, nil
		}
	}

	// 3) Start new transport and become owner
	pid, err := startTransport(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := writePID(cfg.TransportPidFile, pid); err != nil {
		_ = stopPID(pid)
		return nil, fmt.Errorf("pidfile write: %w", err)
	}

	utun, err := waitForUTUNReady(beforeSet, beforeNoIPv4, timeout, preferUTUN)
	if err != nil {
		_ = stopPID(pid)
		_ = os.Remove(cfg.TransportPidFile)
		return nil, fmt.Errorf("transport started but no utun appeared before timeout: %w", err)
	}
	return &Status{PID: pid, NewUTUN: utun, OwnedByUs: true, Running: true}, nil
}

func pickNowReadyUTUN(beforeNoIPv4 map[string]bool, afterNoIPv4 map[string]bool) string {
	for name := range beforeNoIPv4 {
		if !afterNoIPv4[name] {
			return name
		}
	}
	return ""
}

// findBestUTUN returns the highest-numbered utun interface (e.g. utun66),
// which is typically the most recently created tunnel on macOS.
func findBestUTUN() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	bestName := ""
	bestNum := -1
	for _, ifc := range ifaces {
		if !strings.HasPrefix(ifc.Name, "utun") {
			continue
		}
		n, ok := utunNumber(ifc.Name)
		if !ok {
			continue
		}
		if n > bestNum {
			bestNum = n
			bestName = ifc.Name
		}
	}
	if bestName == "" {
		return "", errors.New("no utun interfaces found")
	}
	return bestName, nil
}

func utunNumber(name string) (int, bool) {
	if !strings.HasPrefix(name, "utun") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, "utun"))
	if err != nil {
		return 0, false
	}
	return n, true
}

func StopIfOwned(cfg *config.Config) error {

	pid, ok := readPID(cfg.TransportPidFile)
	if !ok {
		return nil // we don't own anything
	}
	if !processAlive(pid) {
		_ = os.Remove(cfg.TransportPidFile)
		return nil
	}
	if err := stopPID(pid); err != nil {
		return err
	}
	_ = os.Remove(cfg.TransportPidFile)
	return nil
}

func stopPID(pid int) error {
	// transport is started in its own process group (Setpgid: true).
	// Prefer signaling the *process group* so we don't leave helpers/zombies behind.
	term := func() { _ = syscall.Kill(-pid, syscall.SIGTERM) }
	kill := func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }

	// Best-effort TERM (pgid first, then pid).
	term()
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscall.SIGTERM)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	// Force kill (pgid first, then pid).
	kill()
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Signal(syscall.SIGKILL)
	}
	time.Sleep(200 * time.Millisecond)
	if processAlive(pid) {
		return fmt.Errorf("failed to stop transport pid %d", pid)
	}
	return nil
}

func processAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	// kill(pid, 0) checks existence/permission without sending a signal
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	// EPERM means "process exists but you don't have permission".
	// xrouter typically runs as root, but treat EPERM as alive to avoid false negatives.
	if errors.Is(err, syscall.EPERM) {
		return true
	}
	return false
}

func readPID(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	s := strings.TrimSpace(string(b))
	n, err := strconv.Atoi(s)
	if err != nil || n <= 1 {
		return 0, false
	}
	return n, true
}

func writePID(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}

func pickAnyUTUNWithIPv4(present map[string]bool, noIPv4 map[string]bool) string {
	for ifname := range present {
		if ifname == "" {
			continue
		}
		if !noIPv4[ifname] {
			return ifname
		}
	}
	return ""
}

// listUTUN returns:
// - a set of utun interface names that exist now
// - a set of utun interface names that exist now BUT do not yet have an IPv4 address
func listUTUN() (map[string]bool, map[string]bool, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, nil, err
	}
	set := map[string]bool{}
	noIPv4 := map[string]bool{}
	for _, iface := range ifaces {
		if !strings.HasPrefix(iface.Name, "utun") {
			continue
		}
		set[iface.Name] = true
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		hasV4 := false
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil {
				continue
			}
			if ip.To4() != nil {
				hasV4 = true
				break
			}
		}
		if !hasV4 {
			noIPv4[iface.Name] = true
		}
	}
	return set, noIPv4, nil
}

func waitForUTUNReady(
	beforeSet map[string]bool,
	beforeNoIPv4 map[string]bool,
	timeout time.Duration,
	preferUTUN string,
) (string, error) {
	deadline := time.Now().Add(timeout)

	// If transport config pins interface_name, wait for *that* interface to exist and have IPv4.
	if preferUTUN != "" {
		for time.Now().Before(deadline) {
			ok, err := utunHasIPv4(preferUTUN)
			if err == nil && ok {
				return preferUTUN, nil
			}
			time.Sleep(200 * time.Millisecond)
		}
		return "", fmt.Errorf("preferred utun %q not ready before timeout", preferUTUN)
	}

	for time.Now().Before(deadline) {
		utun, err := findUTUNWithIPv4()
		if err == nil && utun != "" {
			if beforeSet == nil {
				return utun, nil
			}

			// Accept:
			// - a brand new utun (not in the snapshot), OR
			// - a previously-existing utun that didn't have IPv4 yet (became ready)
			if !beforeSet[utun] || (beforeNoIPv4 != nil && beforeNoIPv4[utun]) {
				return utun, nil
			}

			// If only one tunnel exists, this is still good enough.
			return utun, nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	return "", fmt.Errorf("no utun with IPv4 within %s", timeout)
}

func findUTUNWithIPv4() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, ifc := range ifaces {
		if !strings.HasPrefix(ifc.Name, "utun") {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ip, _, err := net.ParseCIDR(a.String())
			if err != nil {
				continue
			}
			if ip.To4() != nil {
				return ifc.Name, nil // first utun that has IPv4
			}
		}
	}

	return "", errors.New("no utun interface with IPv4 found")
}

func Inspect(cfg *config.Config) (*Status, error) {
	pid, ok := readPID(cfg.TransportPidFile)
	if !ok {
		return &Status{Running: false, PID: 0, OwnedByUs: false}, nil
	}
	if processAlive(pid) {
		return &Status{Running: true, PID: pid, OwnedByUs: true}, nil
	}
	// pidfile exists but process dead
	return &Status{Running: false, PID: pid, OwnedByUs: true}, nil
}

func InspectExternal(ctx context.Context, cfg *config.Config) (*Status, error) {
	// Look for: transport run -c <cfg.TransportConfigPath>
	// pgrep -f searches the full command line.
	pattern := fmt.Sprintf("transport run -c %s", cfg.TransportConfigPath)

	out, err := exec.CommandContext(ctx, "pgrep", "-f", pattern).Output()
	if err != nil {
		// Not found is normal (pgrep exits non-zero).
		return &Status{Running: false, PID: 0, OwnedByUs: false}, nil
	}

	// pgrep can return multiple PIDs (one per line); take the first.
	lines := strings.Fields(string(out))
	if len(lines) == 0 {
		return &Status{Running: false, PID: 0, OwnedByUs: false}, nil
	}

	pid, convErr := strconv.Atoi(lines[0])
	if convErr != nil || pid <= 1 {
		return &Status{Running: false, PID: 0, OwnedByUs: false}, nil
	}

	return &Status{
		Running:   processAlive(pid),
		PID:       pid,
		OwnedByUs: false,
	}, nil
}

func findExternalTransportPID(cfg *config.Config) (int, bool) {
	// Uses pgrep to find: transport run -c <cfg.TransportConfigPath>
	out, err := exec.Command("pgrep", "-af", "transport").Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Split(string(out), "\n")
	needle := "transport run -c " + cfg.TransportConfigPath
	for _, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.Contains(ln, needle) {
			// line format: "<pid> <cmdline...>"
			fields := strings.Fields(ln)
			if len(fields) < 2 {
				continue
			}
			pid, err := strconv.Atoi(fields[0])
			if err != nil {
				continue
			}
			if processAlive(pid) {
				return pid, true
			}
		}
	}
	return 0, false
}

func boolVal(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func pickNewUTUN(before, after []string) string {
	beforeSet := map[string]struct{}{}
	for _, x := range before {
		beforeSet[x] = struct{}{}
	}
	for _, x := range after {
		if _, ok := beforeSet[x]; !ok {
			return x
		}
	}
	// fallback: if nothing “new” was detected, return last after
	if len(after) > 0 {
		return after[len(after)-1]
	}
	return ""
}

// adopt external transport process by matching "-c <configpath>" in process args
func findExternalByConfig(configPath string) (int, bool) {
	out, err := exec.Command("ps", "aux").Output()
	if err != nil {
		return 0, false
	}
	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		if !strings.Contains(ln, "transport run") {
			continue
		}
		if !strings.Contains(ln, "-c") || !strings.Contains(ln, configPath) {
			continue
		}
		// ps aux: USER PID ...
		fields := strings.Fields(ln)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		return pid, true
	}
	return 0, false
}

func startTransport(ctx context.Context, cfg *config.Config) (int, error) {
	cmd := exec.CommandContext(ctx, cfg.TransportPath, "run", "-c", cfg.TransportConfigPath)

	// Do NOT inherit xrouter's stdout/stderr, otherwise transport logs will "mix" into xrouter output.
	// If TransportLogFile is set, append logs there. Otherwise discard.
	if cfg.TransportLogFile != "" {
		f, err := os.OpenFile(cfg.TransportLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return 0, fmt.Errorf("open transport log file %q: %w", cfg.TransportLogFile, err)
		}
		cmd.Stdout = f
		cmd.Stderr = f
	} else {
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
	}

	// Put transport in its own process group so we can signal it cleanly later if needed.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("transport start: %w", err)
	}
	pid := cmd.Process.Pid

	// Reap the child; otherwise it can become a zombie after exit.
	go func() { _ = cmd.Wait() }()

	return pid, nil
}
