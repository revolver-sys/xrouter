package healthcheck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Result struct {
	OK         bool          `json:"ok"`
	URL        string        `json:"url"`
	StatusCode int           `json:"status_code"`
	Body       string        `json:"body"`
	Latency    time.Duration `json:"latency"`
	Err        string        `json:"err"`
}

func Check(ctx context.Context, url string, timeout time.Duration) Result {
	res := Result{URL: url}

	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		res.Err = fmt.Sprintf("new request: %v", err)
		return res
	}

	client := &http.Client{
		Timeout: timeout, // secondary safety net (ctx is primary)
	}

	resp, err := client.Do(req)
	res.Latency = time.Since(start)

	if err != nil {
		res.Err = fmt.Sprintf("http do: %v", err)
		return res
	}
	defer resp.Body.Close()

	res.StatusCode = resp.StatusCode

	// Read only a limited amount to avoid huge bodies.
	const max = 4 * 1024
	b, _ := io.ReadAll(io.LimitReader(resp.Body, max))
	res.Body = strings.TrimSpace(string(b))

	// Define “OK”: HTTP 200 and non-empty body (simple + practical).
	if resp.StatusCode == 200 && res.Body != "" {
		res.OK = true
	}

	return res
}

// CheckExpected runs the same HTTP probe as Check, but only reports OK if the
// response body matches one of expectedIPs (when expectedIPs is non-empty).
// This is used for transport-path health semantics: the check endpoint must
// return one of the expected secure transport egress addresses when configured.
func CheckExpected(ctx context.Context, url string, timeout time.Duration, expectedIPs []string) Result {
	res := Check(ctx, url, timeout)
	if !res.OK {
		return res
	}
	if len(expectedIPs) == 0 {
		return res
	}
	body := strings.TrimSpace(res.Body)
	for _, ip := range expectedIPs {
		if strings.TrimSpace(ip) == body {
			return res
		}
	}
	// HTTP is reachable but egress is not one of expected IPs => treat as FAIL.
	res.OK = false
	res.Err = fmt.Sprintf("unexpected egress ip %q (expected one of %v)", body, expectedIPs)
	return res
}
