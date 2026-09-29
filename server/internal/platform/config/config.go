package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
)

// Lookup reads one environment variable. Use os.LookupEnv outside tests.
type Lookup func(key string) (string, bool)

// FromEnv reads the process environment.
var FromEnv Lookup = os.LookupEnv

// Common is configuration shared by every service (TRD §2.1).
type Common struct {
	AppEnv          string
	HTTPAddr        string
	LogLevel        slog.Level
	RedisAddr       string
	RedisPassword   string
	PostgresDSN     string
	AuthSigningKey  []byte
	ShutdownTimeout time.Duration
	DBRetry         retry.Policy
	OTLPEndpoint    string
	QuizDataTTL     time.Duration
}

// API is the REST API's configuration (TRD §2.2).
type API struct {
	Common
	DevTokensEnabled      bool
	AuthTokenTTL          time.Duration
	QuestionWindowDefault time.Duration
	RevealDefault         time.Duration
	LobbyTimeout          time.Duration
	QuizCodeMaxAttempts   int
}

// Gateway is the WebSocket gateway's configuration (TRD §2.3).
type Gateway struct {
	Common
	AllowedOrigins       []string
	MaxMessageBytes      int
	ReadBufferBytes      int
	WriteBufferBytes     int
	SendQueueSize        int
	PingInterval         time.Duration
	PongTimeout          time.Duration
	RatePerSec           int
	RateBurst            int
	JoinAdmissionPerSec  int
	RegistryShards       int
	PresenceRefresh      time.Duration
	PresenceTTL          time.Duration
	QuestionCacheMaxSets int
	RedisWaitReplicas    int
	RedisWaitTimeout     time.Duration
}

// Worker is the worker's configuration (TRD §2.4).
type Worker struct {
	Common
	TransitionPoll         time.Duration
	LeaderboardTick        time.Duration
	FlushPoll              time.Duration
	ClaimBatch             int
	FlushVisibilityTimeout time.Duration
}

// LoadAPI loads and validates the REST API's configuration, reporting every problem at once.
func LoadAPI(l Lookup) (API, error) {
	p := parser{lookup: l}
	c := API{
		Common:                p.common(),
		DevTokensEnabled:      p.boolean("DEV_TOKENS_ENABLED", true),
		AuthTokenTTL:          p.positive("AUTH_TOKEN_TTL", 15*time.Minute),
		QuestionWindowDefault: p.between("QUIZ_QUESTION_WINDOW_DEFAULT", 15*time.Second, 5*time.Second, 120*time.Second),
		RevealDefault:         p.between("QUIZ_REVEAL_DEFAULT", 5*time.Second, 2*time.Second, 30*time.Second),
		LobbyTimeout:          p.positive("QUIZ_LOBBY_TIMEOUT", 30*time.Minute),
		QuizCodeMaxAttempts:   p.atLeast("QUIZ_CODE_MAX_ATTEMPTS", 5, 1),
	}
	return c, p.err()
}

// LoadGateway loads and validates the WebSocket gateway's configuration.
func LoadGateway(l Lookup) (Gateway, error) {
	p := parser{lookup: l}
	c := Gateway{
		Common:               p.common(),
		AllowedOrigins:       p.origins("WS_ALLOWED_ORIGINS", "http://localhost:5173"),
		MaxMessageBytes:      p.atLeast("WS_MAX_MESSAGE_BYTES", 4096, 1),
		ReadBufferBytes:      p.atLeast("WS_READ_BUFFER_BYTES", 1024, 1),
		WriteBufferBytes:     p.atLeast("WS_WRITE_BUFFER_BYTES", 1024, 1),
		SendQueueSize:        p.atLeast("WS_SEND_QUEUE_SIZE", 64, 1),
		PingInterval:         p.positive("WS_PING_INTERVAL", 25*time.Second),
		PongTimeout:          p.positive("WS_PONG_TIMEOUT", 60*time.Second),
		RatePerSec:           p.atLeast("WS_RATE_PER_SEC", 20, 1),
		RateBurst:            p.atLeast("WS_RATE_BURST", 40, 1),
		JoinAdmissionPerSec:  p.atLeast("WS_JOIN_ADMISSION_PER_SEC", 500, 1),
		RegistryShards:       p.atLeast("REGISTRY_SHARDS", 64, 1),
		PresenceRefresh:      p.positive("PRESENCE_REFRESH", 10*time.Second),
		PresenceTTL:          p.positive("PRESENCE_TTL", 30*time.Second),
		QuestionCacheMaxSets: p.atLeast("QUESTION_CACHE_MAX_SETS", 256, 1),
		RedisWaitReplicas:    p.atLeast("REDIS_WAIT_REPLICAS", 0, 0),
		RedisWaitTimeout:     p.positive("REDIS_WAIT_TIMEOUT", 50*time.Millisecond),
	}
	if c.PongTimeout <= c.PingInterval {
		p.problem("WS_PONG_TIMEOUT", "must be longer than WS_PING_INTERVAL (%s)", c.PingInterval)
	}
	if c.PresenceTTL <= c.PresenceRefresh {
		p.problem("PRESENCE_TTL", "must be longer than PRESENCE_REFRESH (%s)", c.PresenceRefresh)
	}
	return c, p.err()
}

// LoadWorker loads and validates the worker's configuration.
func LoadWorker(l Lookup) (Worker, error) {
	p := parser{lookup: l}
	c := Worker{
		Common:                 p.common(),
		TransitionPoll:         p.positive("SCHED_TRANSITION_POLL", 100*time.Millisecond),
		LeaderboardTick:        p.positive("SCHED_LEADERBOARD_TICK", 200*time.Millisecond),
		FlushPoll:              p.positive("SCHED_FLUSH_POLL", time.Second),
		ClaimBatch:             p.atLeast("SCHED_CLAIM_BATCH", 100, 1),
		FlushVisibilityTimeout: p.positive("FLUSH_VISIBILITY_TIMEOUT", 30*time.Second),
	}
	return c, p.err()
}

// Variables lists every variable any service reads, sorted. .env.example is tested against it.
func Variables() []string {
	seen := map[string]bool{}
	record := func(key string) (string, bool) { seen[key] = true; return "", false }
	_, _ = LoadAPI(record)
	_, _ = LoadGateway(record)
	_, _ = LoadWorker(record)
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// Error lists every configuration problem found.
type Error struct{ Problems []string }

func (e *Error) Error() string {
	return "invalid configuration:\n  " + strings.Join(e.Problems, "\n  ")
}

// parser reads variables and collects problems instead of stopping at the first one.
type parser struct {
	lookup   Lookup
	problems []string
}

func (p *parser) problem(key, format string, args ...any) {
	p.problems = append(p.problems, key+": "+fmt.Sprintf(format, args...))
}

func (p *parser) err() error {
	if len(p.problems) == 0 {
		return nil
	}
	return &Error{Problems: p.problems}
}

func (p *parser) common() Common {
	c := Common{
		AppEnv:          p.oneOf("APP_ENV", "local", "local", "production"),
		HTTPAddr:        p.nonEmpty("HTTP_ADDR", ":8080"),
		LogLevel:        p.logLevel("LOG_LEVEL", slog.LevelInfo),
		RedisAddr:       p.nonEmpty("REDIS_ADDR", "redis:6379"),
		RedisPassword:   p.str("REDIS_PASSWORD", ""),
		PostgresDSN:     p.required("POSTGRES_DSN"),
		AuthSigningKey:  []byte(p.required("AUTH_SIGNING_KEY")),
		ShutdownTimeout: p.positive("SHUTDOWN_TIMEOUT", 30*time.Second),
		DBRetry: retry.Policy{
			Base:   p.positive("DB_RETRY_BASE", 100*time.Millisecond),
			Cap:    p.positive("DB_RETRY_MAX", 5*time.Second),
			Budget: p.positive("DB_RETRY_BUDGET", 10*time.Second),
		},
		OTLPEndpoint: p.str("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		QuizDataTTL:  p.positive("QUIZ_DATA_TTL", 24*time.Hour),
	}
	if n := len(c.AuthSigningKey); n > 0 && n < 32 {
		p.problem("AUTH_SIGNING_KEY", "must be at least 32 bytes (got %d)", n)
	}
	if c.DBRetry.Base > c.DBRetry.Cap {
		p.problem("DB_RETRY_BASE", "must not exceed DB_RETRY_MAX (%s)", c.DBRetry.Cap)
	}
	return c
}

func (p *parser) str(key, def string) string {
	if v, ok := p.lookup(key); ok {
		return v
	}
	return def
}

func (p *parser) required(key string) string {
	v, ok := p.lookup(key)
	if !ok || v == "" {
		p.problem(key, "is required")
	}
	return v
}

func (p *parser) nonEmpty(key, def string) string {
	v := p.str(key, def)
	if v == "" {
		p.problem(key, "must not be empty")
	}
	return v
}

func (p *parser) oneOf(key, def string, allowed ...string) string {
	v := p.str(key, def)
	if !slices.Contains(allowed, v) {
		p.problem(key, "must be one of %s (got %q)", strings.Join(allowed, ", "), v)
	}
	return v
}

func (p *parser) boolean(key string, def bool) bool {
	v, ok := p.lookup(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		p.problem(key, "must be true or false (got %q)", v)
		return def
	}
	return b
}

func (p *parser) integer(key string, def int) int {
	v, ok := p.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		p.problem(key, "must be an integer (got %q)", v)
		return def
	}
	return n
}

func (p *parser) atLeast(key string, def, lowest int) int {
	n := p.integer(key, def)
	if n < lowest {
		p.problem(key, "must be at least %d (got %d)", lowest, n)
	}
	return n
}

func (p *parser) duration(key string, def time.Duration) (time.Duration, bool) {
	v, ok := p.lookup(key)
	if !ok {
		return def, true
	}
	d, err := time.ParseDuration(strings.TrimSpace(v))
	if err != nil {
		p.problem(key, "must be a duration such as 500ms or 15s (got %q)", v)
		return def, false
	}
	return d, true
}

func (p *parser) positive(key string, def time.Duration) time.Duration {
	d, ok := p.duration(key, def)
	if ok && d <= 0 {
		p.problem(key, "must be greater than zero (got %s)", d)
	}
	return d
}

func (p *parser) between(key string, def, lo, hi time.Duration) time.Duration {
	d, ok := p.duration(key, def)
	if ok && (d < lo || d > hi) {
		p.problem(key, "must be between %s and %s (got %s)", lo, hi, d)
	}
	return d
}

func (p *parser) logLevel(key string, def slog.Level) slog.Level {
	v, ok := p.lookup(key)
	if !ok {
		return def
	}
	var l slog.Level
	switch strings.ToLower(v) {
	case "debug":
		l = slog.LevelDebug
	case "info":
		l = slog.LevelInfo
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		p.problem(key, "must be one of debug, info, warn, error (got %q)", v)
		return def
	}
	return l
}

func (p *parser) origins(key, def string) []string {
	var out []string
	for _, o := range strings.Split(p.str(key, def), ",") {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			p.problem(key, "%q is not an http(s) origin such as http://localhost:5173", o)
			continue
		}
		out = append(out, o)
	}
	if len(out) == 0 && !slices.ContainsFunc(p.problems, func(s string) bool { return strings.HasPrefix(s, key+":") }) {
		p.problem(key, "must list at least one origin")
	}
	return out
}
