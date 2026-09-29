package config_test

import (
	"bufio"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/config"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
)

// env builds a Lookup from a map, so tests never touch the real environment.
func env(m map[string]string) config.Lookup {
	return func(key string) (string, bool) { v, ok := m[key]; return v, ok }
}

// required returns the variables every service needs, set to valid values.
func required(extra ...string) map[string]string {
	m := map[string]string{
		"POSTGRES_DSN":     "postgres://quiz@postgres:5432/quiz?sslmode=disable",
		"AUTH_SIGNING_KEY": strings.Repeat("k", 32),
	}
	for i := 0; i+1 < len(extra); i += 2 {
		m[extra[i]] = extra[i+1]
	}
	return m
}

func TestLoadAPI_DefaultsMatchTRD(t *testing.T) {
	cfg, err := config.LoadAPI(env(required()))
	if err != nil {
		t.Fatal(err)
	}
	checkCommonDefaults(t, cfg.Common)
	want := config.API{
		Common:                cfg.Common,
		DevTokensEnabled:      true,
		AuthTokenTTL:          15 * time.Minute,
		QuestionWindowDefault: 15 * time.Second,
		RevealDefault:         5 * time.Second,
		LobbyTimeout:          30 * time.Minute,
		QuizCodeMaxAttempts:   5,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v\nwant %+v", cfg, want)
	}
}

func TestLoadGateway_DefaultsMatchTRD(t *testing.T) {
	cfg, err := config.LoadGateway(env(required()))
	if err != nil {
		t.Fatal(err)
	}
	checkCommonDefaults(t, cfg.Common)
	want := config.Gateway{
		Common:               cfg.Common,
		AllowedOrigins:       []string{"http://localhost:5173"},
		MaxMessageBytes:      4096,
		ReadBufferBytes:      1024,
		WriteBufferBytes:     1024,
		SendQueueSize:        64,
		PingInterval:         25 * time.Second,
		PongTimeout:          60 * time.Second,
		RatePerSec:           20,
		RateBurst:            40,
		JoinAdmissionPerSec:  500,
		MaxConnections:       20000,
		RegistryShards:       64,
		PresenceRefresh:      10 * time.Second,
		PresenceTTL:          30 * time.Second,
		QuestionCacheMaxSets: 256,
		RedisWaitReplicas:    0,
		RedisWaitTimeout:     50 * time.Millisecond,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v\nwant %+v", cfg, want)
	}
}

func TestLoadWorker_DefaultsMatchTRD(t *testing.T) {
	cfg, err := config.LoadWorker(env(required()))
	if err != nil {
		t.Fatal(err)
	}
	checkCommonDefaults(t, cfg.Common)
	want := config.Worker{
		Common:                 cfg.Common,
		TransitionPoll:         100 * time.Millisecond,
		LeaderboardTick:        200 * time.Millisecond,
		FlushPoll:              time.Second,
		ClaimBatch:             100,
		FlushVisibilityTimeout: 30 * time.Second,
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("got %+v\nwant %+v", cfg, want)
	}
}

func checkCommonDefaults(t *testing.T, c config.Common) {
	t.Helper()
	want := config.Common{
		AppEnv:          "local",
		HTTPAddr:        ":8080",
		LogLevel:        slog.LevelInfo,
		RedisAddr:       "redis:6379",
		RedisPassword:   "",
		PostgresDSN:     required()["POSTGRES_DSN"],
		AuthSigningKey:  []byte(required()["AUTH_SIGNING_KEY"]),
		ShutdownTimeout: 30 * time.Second,
		DBRetry:         retry.Policy{Base: 100 * time.Millisecond, Cap: 5 * time.Second, Budget: 10 * time.Second},
		OTLPEndpoint:    "",
		QuizDataTTL:     24 * time.Hour,
	}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("common: got %+v\nwant %+v", c, want)
	}
}

func TestLoad_ReportsEveryProblemAtOnce(t *testing.T) {
	_, err := config.LoadGateway(env(map[string]string{
		"LOG_LEVEL":        "verbose",
		"WS_RATE_PER_SEC":  "-1",
		"WS_PING_INTERVAL": "soon",
	}))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, key := range []string{"POSTGRES_DSN", "AUTH_SIGNING_KEY", "LOG_LEVEL", "WS_RATE_PER_SEC", "WS_PING_INTERVAL"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s:\n%v", key, err)
		}
	}
}

func TestLoad_RejectsInvalidValues(t *testing.T) {
	type loader func(config.Lookup) error
	api := func(l config.Lookup) error { _, err := config.LoadAPI(l); return err }
	gw := func(l config.Lookup) error { _, err := config.LoadGateway(l); return err }
	wk := func(l config.Lookup) error { _, err := config.LoadWorker(l); return err }

	cases := []struct {
		load       loader
		key, value string
	}{
		// common
		{api, "APP_ENV", "staging"},
		{api, "AUTH_SIGNING_KEY", strings.Repeat("k", 31)}, // HS256 secret must be ≥ 32 bytes
		{api, "DB_RETRY_BASE", "6s"},                       // base above DB_RETRY_MAX (5s)
		{api, "DB_RETRY_BUDGET", "0s"},
		{api, "SHUTDOWN_TIMEOUT", "-1s"},
		{api, "HTTP_ADDR", ""},
		// REST API
		{api, "QUIZ_QUESTION_WINDOW_DEFAULT", "4s"}, // allowed 5–120 s (FR-7)
		{api, "QUIZ_QUESTION_WINDOW_DEFAULT", "121s"},
		{api, "QUIZ_REVEAL_DEFAULT", "1s"}, // allowed 2–30 s
		{api, "QUIZ_REVEAL_DEFAULT", "31s"},
		{api, "QUIZ_CODE_MAX_ATTEMPTS", "0"},
		{api, "AUTH_TOKEN_TTL", "0s"},
		{api, "DEV_TOKENS_ENABLED", "maybe"},
		// gateway
		{gw, "WS_PONG_TIMEOUT", "20s"}, // must exceed WS_PING_INTERVAL (25s)
		{gw, "PRESENCE_TTL", "10s"},    // must exceed PRESENCE_REFRESH (10s)
		{gw, "WS_SEND_QUEUE_SIZE", "0"},
		{gw, "WS_MAX_CONNECTIONS", "0"},
		{gw, "REGISTRY_SHARDS", "0"},
		{gw, "WS_MAX_MESSAGE_BYTES", "0"},
		{gw, "WS_ALLOWED_ORIGINS", ""},
		{gw, "WS_ALLOWED_ORIGINS", "localhost:5173"}, // no scheme
		{gw, "REDIS_WAIT_REPLICAS", "-1"},
		{gw, "QUESTION_CACHE_MAX_SETS", "0"},
		// worker
		{wk, "SCHED_CLAIM_BATCH", "0"},
		{wk, "SCHED_TRANSITION_POLL", "0s"},
		{wk, "FLUSH_VISIBILITY_TIMEOUT", "0s"},
	}
	for _, tc := range cases {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			err := tc.load(env(required(tc.key, tc.value)))
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Errorf("got %v, want an error naming %s", err, tc.key)
			}
		})
	}
}

func TestLoad_EachServiceValidatesOnlyItsOwnVariables(t *testing.T) {
	vars := required("WS_RATE_PER_SEC", "-1", "SCHED_CLAIM_BATCH", "0")
	if _, err := config.LoadAPI(env(vars)); err != nil {
		t.Errorf("API should ignore gateway and worker variables: %v", err)
	}
	_, err := config.LoadWorker(env(vars))
	if err == nil || !strings.Contains(err.Error(), "SCHED_CLAIM_BATCH") || strings.Contains(err.Error(), "WS_RATE_PER_SEC") {
		t.Errorf("worker: got %v, want an error about SCHED_CLAIM_BATCH only", err)
	}
}

func TestLoad_ParsesOverrides(t *testing.T) {
	cfg, err := config.LoadGateway(env(required(
		"LOG_LEVEL", "debug",
		"WS_ALLOWED_ORIGINS", " http://localhost:8080 , https://quiz.example.org ",
		"DB_RETRY_BASE", "50ms", "DB_RETRY_MAX", "2s", "DB_RETRY_BUDGET", "4s",
		"REDIS_WAIT_REPLICAS", "1",
	)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
	if want := []string{"http://localhost:8080", "https://quiz.example.org"}; !reflect.DeepEqual(cfg.AllowedOrigins, want) {
		t.Errorf("AllowedOrigins = %q, want %q", cfg.AllowedOrigins, want)
	}
	if want := (retry.Policy{Base: 50 * time.Millisecond, Cap: 2 * time.Second, Budget: 4 * time.Second}); cfg.DBRetry != want {
		t.Errorf("DBRetry = %+v, want %+v", cfg.DBRetry, want)
	}
	if cfg.RedisWaitReplicas != 1 {
		t.Errorf("RedisWaitReplicas = %d", cfg.RedisWaitReplicas)
	}
}

// .env.example must document exactly the variables the services read (plus Compose-only ones).
func TestEnvExample_ListsEveryVariable(t *testing.T) {
	composeOnly := map[string]bool{
		"REDIS_HOST_PORT": true, "POSTGRES_HOST_PORT": true,
		"POSTGRES_USER": true, "POSTGRES_PASSWORD": true, "POSTGRES_DB": true,
	}
	f, err := os.Open(filepath.Join("..", "..", "..", "..", ".env.example"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	documented := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if key, _, ok := strings.Cut(line, "="); ok && !composeOnly[key] {
			documented[key] = true
		}
	}
	known := map[string]bool{}
	for _, v := range config.Variables() {
		known[v] = true
		if !documented[v] {
			t.Errorf("%s is read by a service but missing from .env.example", v)
		}
	}
	for v := range documented {
		if !known[v] {
			t.Errorf("%s is in .env.example but no service reads it", v)
		}
	}
}
