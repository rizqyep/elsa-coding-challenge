package health

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbe(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		ok     bool
	}{{"ready", http.StatusOK, true}, {"not ready", http.StatusServiceUnavailable, false}} {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path = r.URL.Path
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			err := Probe(strings.TrimPrefix(srv.URL, "http://"))
			if (err == nil) != tc.ok {
				t.Fatalf("Probe err = %v, want ok=%v", err, tc.ok)
			}
			if path != "/readyz" {
				t.Errorf("probed %q, want /readyz", path)
			}
		})
	}
}

func TestProbe_Unreachable(t *testing.T) {
	if err := Probe("127.0.0.1:1"); err == nil {
		t.Fatal("Probe of a closed port succeeded")
	}
}

func TestLocalURL(t *testing.T) {
	for addr, want := range map[string]string{
		":8080":        "http://127.0.0.1:8080/readyz",
		"0.0.0.0:9000": "http://127.0.0.1:9000/readyz",
		"[::]:8080":    "http://127.0.0.1:8080/readyz",
		"host:7000":    "http://host:7000/readyz",
	} {
		if got, err := localURL(addr); err != nil || got != want {
			t.Errorf("localURL(%q) = %q, %v; want %q", addr, got, err, want)
		}
	}
	if _, err := localURL("no-port"); err == nil {
		t.Error("localURL accepted an address without a port")
	}
}
