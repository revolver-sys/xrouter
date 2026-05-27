package utun

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

var reUTUN = regexp.MustCompile(`(?m)^(utun[0-9]+):`)

// List returns utun interfaces seen in ifconfig output (e.g. ["utun0","utun66"]).
func List() ([]string, error) {
	out, err := exec.Command("ifconfig").Output()
	if err != nil {
		return nil, fmt.Errorf("ifconfig: %w", err)
	}
	m := reUTUN.FindAllSubmatch(out, -1)
	seen := make(map[string]struct{}, len(m))
	for _, mm := range m {
		if len(mm) >= 2 {
			seen[string(mm[1])] = struct{}{}
		}
	}
	var res []string
	for k := range seen {
		res = append(res, k)
	}
	sort.Strings(res)
	return res, nil
}

// Diff returns items present in after but not in before.
func Diff(before, after []string) []string {
	bm := make(map[string]struct{}, len(before))
	for _, b := range before {
		bm[b] = struct{}{}
	}
	var out []string
	for _, a := range after {
		if _, ok := bm[a]; !ok {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// DebugString is helpful when logging.
func DebugString(xs []string) string {
	return strings.Join(xs, ",")
}

// Optional: if you want the raw output quickly.
func ifconfig() ([]byte, error) {
	cmd := exec.Command("ifconfig")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ifconfig: %w (%s)", err, buf.String())
	}
	return buf.Bytes(), nil
}
