//go:build integration

// Package testenv starts real Redis, PostgreSQL, and Toxiproxy containers for integration tests (TRD §10.2).
package testenv

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	tctoxiproxy "github.com/testcontainers/testcontainers-go/modules/toxiproxy"
	"github.com/testcontainers/testcontainers-go/network"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/migrations"
)

// Images match docker-compose.yml.
const (
	redisImage     = "redis:7.4-alpine"
	postgresImage  = "postgres:16-alpine"
	toxiproxyImage = "ghcr.io/shopify/toxiproxy:2.12.0"
)

// Proxy names in Toxiproxy.
const (
	RedisProxy    = "redis"
	PostgresProxy = "postgres"
)

// Env holds connections to the containers. Direct clients bypass Toxiproxy; the *ViaProxy
// ones go through it, so tests can inject latency and outages.
type Env struct {
	Redis               *redis.Client
	RedisViaProxy       *redis.Client
	Postgres            *pgxpool.Pool
	PostgresDSN         string
	PostgresViaProxyDSN string
	Toxiproxy           *toxiclient.Client

	network *testcontainers.DockerNetwork
}

// Run starts the containers, applies migrations with seed data, runs the package's tests,
// and cleans up. Call it from TestMain: os.Exit(testenv.Run(m, &env)).
func Run(m *testing.M, env **Env) int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	e, cleanup, err := start(ctx)
	cancel()
	defer cleanup()
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	*env = e
	return m.Run()
}

func start(ctx context.Context) (*Env, func(), error) {
	var cleanups []func()
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	fail := func(err error) (*Env, func(), error) { return nil, cleanup, err }

	nw, err := network.New(ctx)
	if err != nil {
		return fail(fmt.Errorf("network: %w", err))
	}
	cleanups = append(cleanups, func() { _ = nw.Remove(context.Background()) })

	rc, err := tcredis.Run(ctx, redisImage, network.WithNetwork([]string{"redis"}, nw))
	cleanups = append(cleanups, func() { _ = testcontainers.TerminateContainer(rc) })
	if err != nil {
		return fail(fmt.Errorf("redis: %w", err))
	}
	pc, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("quiz"), tcpostgres.WithUsername("quiz"), tcpostgres.WithPassword("quiz"),
		tcpostgres.BasicWaitStrategies(),
		network.WithNetwork([]string{"postgres"}, nw))
	cleanups = append(cleanups, func() { _ = testcontainers.TerminateContainer(pc) })
	if err != nil {
		return fail(fmt.Errorf("postgres: %w", err))
	}
	tc, err := tctoxiproxy.Run(ctx, toxiproxyImage,
		tctoxiproxy.WithProxy(RedisProxy, "redis:6379"),       // listens on 8666
		tctoxiproxy.WithProxy(PostgresProxy, "postgres:5432"), // listens on 8667
		network.WithNetwork([]string{"toxiproxy"}, nw))
	cleanups = append(cleanups, func() { _ = testcontainers.TerminateContainer(tc) })
	if err != nil {
		return fail(fmt.Errorf("toxiproxy: %w", err))
	}

	e := &Env{network: nw}
	redisURL, err := rc.ConnectionString(ctx)
	if err != nil {
		return fail(err)
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return fail(err)
	}
	e.Redis = redis.NewClient(opts)
	cleanups = append(cleanups, func() { _ = e.Redis.Close() })

	host, port, err := tc.ProxiedEndpoint(8666)
	if err != nil {
		return fail(err)
	}
	e.RedisViaProxy = redis.NewClient(&redis.Options{Addr: host + ":" + port, MaxRetries: -1})
	cleanups = append(cleanups, func() { _ = e.RedisViaProxy.Close() })

	if e.PostgresDSN, err = pc.ConnectionString(ctx, "sslmode=disable"); err != nil {
		return fail(err)
	}
	host, port, err = tc.ProxiedEndpoint(8667)
	if err != nil {
		return fail(err)
	}
	e.PostgresViaProxyDSN = fmt.Sprintf("postgres://quiz:quiz@%s:%s/quiz?sslmode=disable", host, port)

	if err := migrate(ctx, e.PostgresDSN); err != nil {
		return fail(err)
	}
	if e.Postgres, err = pgxpool.New(ctx, e.PostgresDSN); err != nil {
		return fail(err)
	}
	cleanups = append(cleanups, e.Postgres.Close)

	uri, err := tc.URI(ctx)
	if err != nil {
		return fail(err)
	}
	e.Toxiproxy = toxiclient.NewClient(uri)
	return e, cleanup, nil
}

func migrate(ctx context.Context, dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := migrations.Apply(ctx, db, true); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Reset clears Redis and the quiz tables (question sets stay seeded) and removes every injected fault. Call it at the start of each test.
func (e *Env) Reset(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	if err := e.Redis.FlushAll(ctx).Err(); err != nil {
		t.Fatalf("flush redis: %v", err)
	}
	if err := e.Toxiproxy.ResetState(); err != nil {
		t.Fatalf("reset toxiproxy: %v", err)
	}
	if _, err := e.Postgres.Exec(ctx, "TRUNCATE answers, quiz_results, quizzes"); err != nil {
		t.Fatalf("truncate postgres: %v", err)
	}
}

// Proxy returns a Toxiproxy proxy by name, for injecting faults.
func (e *Env) Proxy(t *testing.T, name string) *toxiclient.Proxy {
	t.Helper()
	p, err := e.Toxiproxy.Proxy(name)
	if err != nil {
		t.Fatalf("proxy %s: %v", name, err)
	}
	return p
}

// StartReplica starts a Redis replica of the main instance and returns a client for it.
// The replica is removed when the test ends.
func (e *Env) StartReplica(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()
	c, err := tcredis.Run(ctx, redisImage,
		network.WithNetwork([]string{"redis-replica"}, e.network),
		testcontainers.WithCmd("redis-server", "--replicaof", "redis", "6379"))
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })
	if err != nil {
		t.Fatalf("replica: %v", err)
	}
	url, err := c.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	rc := redis.NewClient(opts)
	t.Cleanup(func() { _ = rc.Close() })
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if info := rc.Info(ctx, "replication").Val(); strings.Contains(info, "master_link_status:up") {
			return rc
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("replica never connected to the primary")
	return nil
}
