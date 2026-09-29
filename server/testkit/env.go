package testkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/metrics/metricstest"
)

// Compose is the stack started by `make up`, reached through nginx (TRD §11). The database
// connection is test-harness access: answer keys for choosing right or wrong answers, and the
// archived results to check totals against. Faults use the Docker CLI and Toxiproxy.
type Compose struct {
	BaseURL   string // e.g. http://localhost:8080
	Project   string // Compose project name, "quiz"
	Toxiproxy string // e.g. http://127.0.0.1:8474; empty without the chaos profile
	DB        *pgxpool.Pool
	http      *http.Client
}

// NewCompose connects to a running stack.
func NewCompose(ctx context.Context, baseURL, postgresDSN string) (*Compose, error) {
	pool, err := pgxpool.New(ctx, postgresDSN)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return &Compose{BaseURL: strings.TrimRight(baseURL, "/"), Project: "quiz", Toxiproxy: "http://127.0.0.1:8474", DB: pool,
		http: &http.Client{Timeout: 15 * time.Second}}, nil
}

// Close releases the database connection.
func (e *Compose) Close() { e.DB.Close() }

// WSURL is the WebSocket endpoint.
func (e *Compose) WSURL() string { return "ws" + strings.TrimPrefix(e.BaseURL, "http") + "/ws" }

// Token is a mocked identity (POST /api/v1/dev/tokens).
type Token struct {
	Token         string `json:"token"`
	ParticipantID string `json:"participantId"`
}

// DevToken issues a token for role ("host" or "participant").
func (e *Compose) DevToken(ctx context.Context, role string) (Token, error) {
	var t Token
	err := e.call(ctx, http.MethodPost, "/api/v1/dev/tokens", "", map[string]any{"role": role}, &t)
	return t, err
}

// CreateQuiz creates a quiz from a seeded question set and returns its code.
func (e *Compose) CreateQuiz(ctx context.Context, host Token, set string, window, reveal time.Duration) (string, error) {
	var q struct{ Code string }
	err := e.call(ctx, http.MethodPost, "/api/v1/quizzes", host.Token, map[string]any{"questionSetId": set,
		"questionWindowSeconds": int(window.Seconds()), "revealSeconds": int(reveal.Seconds())}, &q)
	return q.Code, err
}

// Start asks for the quiz to start (FR-3).
func (e *Compose) Start(ctx context.Context, host Token, code string) error {
	return e.call(ctx, http.MethodPost, "/api/v1/quizzes/"+code+"/start", host.Token, nil, nil)
}

// Entry is one leaderboard row.
type Entry struct {
	Rank          int    `json:"rank"`
	ParticipantID string `json:"participantId"`
	DisplayName   string `json:"displayName"`
	Score         int    `json:"score"`
}

// Leaderboard reads every page of a quiz's leaderboard over REST.
func (e *Compose) Leaderboard(ctx context.Context, tok Token, code string) ([]Entry, error) {
	var all []Entry
	for offset := 0; ; {
		var page struct {
			ParticipantCount int     `json:"participantCount"`
			Entries          []Entry `json:"entries"`
		}
		if err := e.call(ctx, http.MethodGet, fmt.Sprintf("/api/v1/quizzes/%s/leaderboard?offset=%d&limit=1000", code, offset), tok.Token, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Entries...)
		offset += len(page.Entries)
		if len(page.Entries) == 0 || offset >= page.ParticipantCount {
			return all, nil
		}
	}
}

// AnswerKeys maps each question of a set to its correct option (harness access; clients never see it early, FR-21).
func (e *Compose) AnswerKeys(ctx context.Context, set string) (map[string]string, error) {
	rows, err := e.DB.Query(ctx, `SELECT id, correct_option_id FROM questions WHERE set_id = $1`, set)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := map[string]string{}
	for rows.Next() {
		var q, o string
		if err := rows.Scan(&q, &o); err != nil {
			return nil, err
		}
		keys[q] = o
	}
	return keys, rows.Err()
}

// ArchivedTotals waits until the quiz's final results are saved (FR-27a) and returns each participant's total.
func (e *Compose) ArchivedTotals(ctx context.Context, code string, participants int) (map[string]int, error) {
	for {
		rows, err := e.DB.Query(ctx, `SELECT participant_id, total_score FROM quiz_results WHERE quiz_code = $1`, code)
		if err != nil {
			return nil, err
		}
		totals := map[string]int{}
		for rows.Next() {
			var id string
			var score int
			if err := rows.Scan(&id, &score); err != nil {
				rows.Close()
				return nil, err
			}
			totals[id] = score
		}
		rows.Close()
		if len(totals) >= participants {
			return totals, nil
		}
		select {
		case <-ctx.Done():
			return totals, fmt.Errorf("%d of %d results saved: %w", len(totals), participants, ctx.Err())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// Containers lists a service's running containers, e.g. ["quiz-ws-1", "quiz-ws-2"].
func (e *Compose) Containers(ctx context.Context, service string, all bool) ([]string, error) {
	args := []string{"ps", "--format", "{{.Names}}", "--filter", "label=com.docker.compose.project=" + e.Project,
		"--filter", "label=com.docker.compose.service=" + service}
	if all {
		args = append(args, "-a")
	}
	out, err := docker(ctx, args...)
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// Metrics sums a service's /metrics across its running instances, read from inside the Compose network.
func (e *Compose) Metrics(ctx context.Context, service string) (map[string]float64, error) {
	names, err := e.Containers(ctx, service, false)
	if err != nil {
		return nil, err
	}
	out := map[string]float64{}
	for _, n := range names {
		if err := e.metricsInto(ctx, n, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// MetricsOf reads one container's /metrics, e.g. "quiz-ws-1".
func (e *Compose) MetricsOf(ctx context.Context, container string) (map[string]float64, error) {
	out := map[string]float64{}
	return out, e.metricsInto(ctx, container, out)
}

func (e *Compose) metricsInto(ctx context.Context, container string, out map[string]float64) error {
	text, err := docker(ctx, "exec", e.Project+"-nginx-1", "wget", "-qO-", "http://"+container+":8080/metrics")
	if err != nil {
		return fmt.Errorf("metrics from %s: %w", container, err)
	}
	return metricstest.Parse(strings.NewReader(text), out)
}

// Kill kills one container, e.g. "quiz-ws-1"; it stays down until StartAll.
func (e *Compose) Kill(ctx context.Context, container string) error {
	_, err := docker(ctx, "kill", container)
	return err
}

// StopService stops every instance of a service.
func (e *Compose) StopService(ctx context.Context, service string) error {
	names, err := e.Containers(ctx, service, false)
	if err != nil || len(names) == 0 {
		return err
	}
	_, err = docker(ctx, append([]string{"stop", "-t", "0"}, names...)...)
	return err
}

// StartAll starts every stopped or killed container of the project and unpauses PostgreSQL.
func (e *Compose) StartAll(ctx context.Context) error {
	_, _ = docker(ctx, "unpause", e.Project+"-postgres-1")
	out, err := docker(ctx, "ps", "-a", "--format", "{{.Names}}", "--filter", "label=com.docker.compose.project="+e.Project,
		"--filter", "status=exited")
	if err != nil {
		return err
	}
	var names []string
	for _, n := range strings.Fields(out) {
		if !strings.HasSuffix(n, "-migrate-1") {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	_, err = docker(ctx, append([]string{"start"}, names...)...)
	return err
}

// PausePostgres freezes PostgreSQL until StartAll (or Unpause).
func (e *Compose) PausePostgres(ctx context.Context) error {
	_, err := docker(ctx, "pause", e.Project+"-postgres-1")
	return err
}

// UnpausePostgres resumes PostgreSQL.
func (e *Compose) UnpausePostgres(ctx context.Context) error {
	_, err := docker(ctx, "unpause", e.Project+"-postgres-1")
	return err
}

// RedisLatency adds latency to every Redis call (needs the chaos profile).
func (e *Compose) RedisLatency(ctx context.Context, ms int) error {
	return e.toxi(ctx, "/proxies/redis/toxics", map[string]any{"name": "latency", "type": "latency", "attributes": map[string]any{"latency": ms}})
}

// SetRedisEnabled cuts services off from Redis, or restores them (needs the chaos profile).
func (e *Compose) SetRedisEnabled(ctx context.Context, on bool) error {
	return e.toxi(ctx, "/proxies/redis", map[string]any{"enabled": on})
}

// RedisResetPeer makes Redis connections reset with a TCP RST, as if torn mid-request (needs the chaos profile).
func (e *Compose) RedisResetPeer(ctx context.Context) error {
	return e.toxi(ctx, "/proxies/redis/toxics", map[string]any{"name": "reset", "type": "reset_peer", "attributes": map[string]any{"timeout": 0}})
}

// RemoveRedisToxic removes one named toxic, e.g. "reset" or "latency".
func (e *Compose) RemoveRedisToxic(ctx context.Context, name string) error {
	return e.toxiDo(ctx, http.MethodDelete, "/proxies/redis/toxics/"+name, nil)
}

// ResetFaults removes Toxiproxy toxics and re-enables its proxies.
func (e *Compose) ResetFaults(ctx context.Context) error { return e.toxi(ctx, "/reset", nil) }

// ToxiproxyUp reports whether the chaos profile's Toxiproxy is reachable.
func (e *Compose) ToxiproxyUp(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.Toxiproxy+"/version", nil)
	if err != nil {
		return false
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// WaitHealthy waits until every project container with a healthcheck reports healthy.
func (e *Compose) WaitHealthy(ctx context.Context) error {
	for {
		out, err := docker(ctx, "ps", "-a", "--format", "{{.Names}} {{.Status}}", "--filter", "label=com.docker.compose.project="+e.Project)
		if err != nil {
			return err
		}
		var waiting []string
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if strings.Contains(line, "-migrate-") {
				continue
			}
			if !strings.Contains(line, " Up ") || (strings.Contains(line, "health") && !strings.Contains(line, "(healthy)")) {
				waiting = append(waiting, line)
			}
		}
		if len(waiting) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("not healthy: %v: %w", waiting, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (e *Compose) toxi(ctx context.Context, path string, body any) error {
	return e.toxiDo(ctx, http.MethodPost, path, body)
}

func (e *Compose) toxiDo(ctx context.Context, method, path string, body any) error {
	if e.Toxiproxy == "" {
		return fmt.Errorf("toxiproxy not configured (start with PROFILES=chaos)")
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, method, e.Toxiproxy+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return fmt.Errorf("toxiproxy (needs PROFILES=chaos): %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("toxiproxy %s: %d %s", path, resp.StatusCode, msg)
	}
	return nil
}

func (e *Compose) call(ctx context.Context, method, path, token string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, e.BaseURL+path, r)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, b)
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

func docker(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}
