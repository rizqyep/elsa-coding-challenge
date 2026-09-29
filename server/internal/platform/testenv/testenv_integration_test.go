//go:build integration

package testenv_test

import (
	"context"
	"os"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/jackc/pgx/v5"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

// Faults injected through Toxiproxy must actually reach the client; later reliability tests rely on it.
func TestToxiproxy_InjectsLatencyAndOutagesOnRedis(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	if err := env.RedisViaProxy.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping via proxy: %v", err)
	}

	proxy := env.Proxy(t, testenv.RedisProxy)
	if _, err := proxy.AddToxic("latency", "latency", "downstream", 1, toxiclient.Attributes{"latency": 300}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := env.RedisViaProxy.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping with latency: %v", err)
	}
	if d := time.Since(start); d < 300*time.Millisecond {
		t.Errorf("ping took %v with a 300 ms latency toxic", d)
	}

	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := env.RedisViaProxy.Ping(cctx).Err(); err == nil {
		t.Error("ping succeeded while the proxy was disabled")
	}

	env.Reset(t) // removes toxics and re-enables proxies
	if err := env.RedisViaProxy.Ping(ctx).Err(); err != nil {
		t.Errorf("ping after reset: %v", err)
	}
}

func TestToxiproxy_ReachesPostgres(t *testing.T) {
	env.Reset(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, env.PostgresViaProxyDSN)
	if err != nil {
		t.Fatalf("connect via proxy: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var n int
	if err := conn.QueryRow(ctx, "select count(*) from question_sets").Scan(&n); err != nil || n != 3 {
		t.Errorf("seeded question sets via proxy: n=%d err=%v, want 3", n, err)
	}
}
