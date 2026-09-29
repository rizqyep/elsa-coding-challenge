package testkit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// fakeServer upgrades and sends frames in order, then waits for the client to hang up.
func fakeServer(t *testing.T, frames ...string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		for _, f := range frames {
			if err := c.WriteMessage(websocket.TextMessage, []byte(f)); err != nil {
				return
			}
		}
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

const question = `{"v":1,"type":"question","data":{"question":{"questionId":"q1","index":0,"count":3,"prompt":"Synonym of rapid?",` +
	`"options":[{"id":"a","text":"slow"},{"id":"b","text":"quick"}],"openedAt":1000,"deadline":16000,"closeAt":16000},"stateVersion":%d}}`

func q(v string) string { return strings.Replace(question, "%d", v, 1) }

func dialFake(t *testing.T, url string) *Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := Dial(ctx, url, "tok", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func waitCount(t *testing.T, c *Client, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.WaitFor(ctx, n-1, func(Message) bool { return true }); err != nil {
		t.Fatalf("waiting for message %d: %v (got %d)", n, err, len(c.Messages()))
	}
}

func TestClient_ValidatesEveryMessageAgainstItsSchema(t *testing.T) {
	leak := strings.Replace(q("4"), `"closeAt":16000`, `"closeAt":16000,"correctOptionId":"b"`, 1)
	c := dialFake(t, fakeServer(t, q("3"), leak))
	waitCount(t, c, 2)
	v := c.Violations()
	if len(v) != 1 || !strings.Contains(v[0], "question") {
		t.Fatalf("violations %v; want exactly the leaked answer key flagged", v)
	}
}

func TestClient_AppliesOnlyNewerVersions(t *testing.T) {
	lb := func(v string) string {
		return `{"v":1,"type":"leaderboard","data":{"version":` + v + `,"participantCount":1,"top":[]}}`
	}
	state := `{"v":1,"type":"quiz_state","data":{"status":"question_open","questionIndex":0,"questionCount":3,"stateVersion":2}}`
	c := dialFake(t, fakeServer(t, q("3"), state, lb("7"), lb("6")))
	waitCount(t, c, 4)
	if c.StateVersion() != 3 || c.LeaderboardVersion() != 7 {
		t.Errorf("applied state %d, leaderboard %d; want 3 and 7 (older ones ignored, FR-28)", c.StateVersion(), c.LeaderboardVersion())
	}
	if c.Stale() != 2 {
		t.Errorf("stale %d, want 2", c.Stale())
	}
	if v := c.Violations(); len(v) != 0 {
		t.Errorf("violations %v; stale messages are allowed", v)
	}
}

// The same state version must always mean the same state; two different messages sharing one is a server bug.
func TestClient_FlagsConflictingMessagesWithTheSameVersion(t *testing.T) {
	closed := `{"v":1,"type":"question_closed","data":{"questionId":"q1","correctOptionId":"b","nextTransitionAt":2000,"stateVersion":3}}`
	c := dialFake(t, fakeServer(t, q("3"), q("3"), closed))
	waitCount(t, c, 3)
	v := c.Violations()
	if len(v) != 1 || !strings.Contains(v[0], "version 3") {
		t.Fatalf("violations %v; want the question_closed reusing version 3 flagged, not the identical resend", v)
	}
}

func TestClient_RecordsTheCloseCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(4000, "replaced"))
		_ = c.Close()
	}))
	defer srv.Close()
	c := dialFake(t, "ws"+strings.TrimPrefix(srv.URL, "http"))
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("client did not notice the close")
	}
	if c.CloseCode() != 4000 {
		t.Errorf("close code %d, want 4000", c.CloseCode())
	}
}

// A 503 during a burst is retried after its Retry-After, as the protocol asks (TRD §7.1).
func TestDial_RetriesAfter503(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		c, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err == nil {
			_ = c.Close()
		}
	}))
	defer srv.Close()
	start := time.Now()
	dialFake(t, "ws"+strings.TrimPrefix(srv.URL, "http"))
	if calls != 2 || time.Since(start) < 900*time.Millisecond {
		t.Errorf("%d attempts in %v; want a retry after about 1 s", calls, time.Since(start))
	}
}

// Sampled validation still checks the first message of each type, then every Nth.
func TestClient_SampledValidation(t *testing.T) {
	leak := strings.Replace(q("4"), `"closeAt":16000`, `"closeAt":16000,"correctOptionId":"b"`, 1)
	lb := func(v string) string {
		return `{"v":1,"type":"leaderboard","data":{"version":` + v + `,"participantCount":1,"top":[]}}`
	}
	frames := []string{leak, lb("1"), lb("2"), lb("3"), `{"v":1,"type":"leaderboard","data":{"version":4}}`, lb("5")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := DialWith(ctx, fakeServer(t, frames...), "tok", DialOptions{ValidateEvery: 4})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	waitCount(t, c, len(frames))
	v := c.Violations()
	// The leaked key is the first question: checked. The invalid 4th leaderboard is the 4th of its type: checked.
	if len(v) != 2 {
		t.Fatalf("violations %v; want the first question and the 4th leaderboard checked", v)
	}
	if c.LeaderboardVersion() != 5 {
		t.Errorf("leaderboard version %d, want 5", c.LeaderboardVersion())
	}
}
