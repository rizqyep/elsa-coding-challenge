// Package health runs a service's own readiness check, for container healthchecks on distroless images (TRD §11.2).
package health

// AI-assisted: AI-034 (docs/ai-collaboration/log.md).

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

const timeout = 2 * time.Second

// Main handles `<binary> healthcheck`: it probes /readyz and exits with the result; for any other arguments it returns.
func Main(args []string) {
	if len(args) < 2 || args[1] != "healthcheck" {
		return
	}
	if err := Probe(os.Getenv("HTTP_ADDR")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Probe GETs /readyz on the service listening at addr and fails unless it answers 200.
func Probe(addr string) error {
	url, err := localURL(addr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	return nil
}

// localURL turns a listen address into a URL on this host; unspecified hosts become 127.0.0.1.
func localURL(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("HTTP_ADDR %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/readyz", nil
}
