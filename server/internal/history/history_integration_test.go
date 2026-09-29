//go:build integration

package history_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/history"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/scoring"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

const code = quiz.Code("K7Q2MX")

func reset(t *testing.T) (*history.RedisLiveStore, *history.PostgresStore) {
	t.Helper()
	env.Reset(t)
	ctx := context.Background()
	all := append(append(append(quiz.Scripts(), session.Scripts()...), scoring.Scripts()...), history.Scripts()...)
	if err := redisx.LoadScripts(ctx, env.Redis, all...); err != nil {
		t.Fatal(err)
	}
	return history.NewRedisLiveStore(env.Redis), history.NewPostgresStore(env.Postgres)
}

// playQuestion creates the quiz (Redis + its PostgreSQL row), joins participants, records the given
// answers to dq-01, and closes the question, which queues the flush job. Returns that job.
func playQuestion(t *testing.T, answers map[string]bool) history.Job {
	t.Helper()
	ctx := context.Background()
	qr := quiz.NewRedisRepository(env.Redis)
	if _, err := qr.CreateRoom(ctx, quiz.CreateRoomInput{Code: code, QuestionSetID: "demo-quick", HostID: "host_1",
		QuestionIDs: []quiz.QuestionID{"dq-01"}, WindowMs: 15_000, RevealMs: 5_000, LobbyTimeoutMs: 60_000, TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Postgres.Exec(ctx, `INSERT INTO quizzes (code, question_set_id, host_id, status, window_ms, reveal_ms)
		VALUES ($1, 'demo-quick', 'host_1', 'running', 15000, 5000)`, string(code)); err != nil {
		t.Fatal(err)
	}
	sr := session.NewRedisRepository(env.Redis)
	ids := make([]string, 0, len(answers)+1)
	for p := range answers {
		ids = append(ids, p)
	}
	ids = append(ids, "u_silent") // joined but never answers: keeps early close off, and must still get a result
	for _, p := range ids {
		if _, err := sr.Join(ctx, session.JoinInput{Code: code, ParticipantID: quiz.ParticipantID(p), DisplayName: "P-" + p, TTL: time.Hour}); err != nil {
			t.Fatal(err)
		}
	}
	if err := qr.Start(ctx, code, "host_1"); err != nil {
		t.Fatal(err)
	}
	mustApply(t, qr, quiz.StatusQuestionOpen)
	ar := scoring.NewRedisRepository(env.Redis, scoring.RedisOptions{OnlineWindow: 30 * time.Second, TTL: time.Hour})
	for p, correct := range answers {
		option := "dq-01-a"
		if correct {
			option = "dq-01-b"
		}
		if _, err := ar.RecordAnswer(ctx, scoring.AnswerInput{Code: code, ParticipantID: quiz.ParticipantID(p), QuestionID: "dq-01", OptionID: quiz.OptionID(option), Correct: correct}); err != nil {
			t.Fatal(err)
		}
	}
	env.Redis.HSet(ctx, redisx.RoomKey(string(code)), "close_at", 0, "next_at", 0)
	mustApply(t, qr, quiz.StatusQuestionClosed)
	job, err := history.ParseJob("q|" + string(code) + "|dq-01")
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func mustApply(t *testing.T, qr *quiz.RedisRepository, want quiz.Status) {
	t.Helper()
	res, err := qr.ApplyTransition(context.Background(), code)
	if err != nil || res.Status != want {
		t.Fatalf("transition: %+v, %v; want %s", res, err, want)
	}
}

func count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := env.Postgres.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func newService(live history.LiveStore, store history.Store, onMismatch func(quiz.Code, []history.Mismatch)) *history.Service {
	return history.NewService(live, store, history.Options{TTL: time.Hour, FinalRetryDelay: time.Second, OnMismatch: onMismatch})
}

// ---- Redis live store

func TestClaim_HidesClaimedJobsUntilTheTimeout(t *testing.T) {
	live, _ := reset(t)
	ctx := context.Background()
	now := env.Redis.Time(ctx).Val().UnixMilli()
	env.Redis.ZAdd(ctx, redisx.SchedFlush, z("q|AAAAAA|x", now-3), z("q|BBBBBB|x", now-2), z("final|CCCCCC", now-1), z("final|DDDDDD", now+60_000))
	jobs, err := live.Claim(ctx, 10, 150*time.Millisecond)
	if err != nil || len(jobs) != 3 || jobs[0].ID != "q|AAAAAA|x" || jobs[2].ID != "final|CCCCCC" {
		t.Fatalf("claim: %v, %v; want the 3 due jobs, oldest first", jobs, err)
	}
	if again, _ := live.Claim(ctx, 10, 150*time.Millisecond); len(again) != 0 {
		t.Errorf("claimed jobs were claimable again immediately: %v", again)
	}
	time.Sleep(250 * time.Millisecond)
	if later, _ := live.Claim(ctx, 10, time.Minute); len(later) != 3 {
		t.Errorf("after the timeout, %d jobs were claimable, want 3 (a crashed worker's jobs come back)", len(later))
	}
}

func TestClaim_CompetingWorkersGetEachJobOnce(t *testing.T) {
	live, _ := reset(t)
	ctx := context.Background()
	now := env.Redis.Time(ctx).Val().UnixMilli()
	var want []string
	for i := range 100 {
		id := fmt.Sprintf("q|R%05d|x", i)
		want = append(want, id)
		env.Redis.ZAdd(ctx, redisx.SchedFlush, z(id, now-1))
	}
	var mu sync.Mutex
	var got []string
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 { // bounded: if claimed jobs weren't hidden this would never end
				jobs, err := live.Claim(ctx, 7, time.Minute)
				if err != nil {
					t.Error(err)
					return
				}
				if len(jobs) == 0 {
					return
				}
				mu.Lock()
				for _, j := range jobs {
					got = append(got, j.ID)
				}
				mu.Unlock()
			}
			t.Error("a worker never ran out of jobs: claimed jobs are being handed out again")
		})
	}
	wg.Wait()
	sort.Strings(got)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("claimed %d jobs with duplicates or gaps, want each of %d exactly once", len(got), len(want))
	}
}

func TestClaim_SkipsMalformedJobIDs(t *testing.T) {
	live, _ := reset(t)
	ctx := context.Background()
	now := env.Redis.Time(ctx).Val().UnixMilli()
	env.Redis.ZAdd(ctx, redisx.SchedFlush, z("garbage", now-2), z("final|AAAAAA", now-1))
	jobs, err := live.Claim(ctx, 10, time.Minute)
	if err != nil || len(jobs) != 1 || jobs[0].ID != "final|AAAAAA" {
		t.Errorf("got %v, %v; want only the valid job", jobs, err)
	}
}

func TestAckFlush_DeletesAnswersAndDecrementsOnce(t *testing.T) {
	live, _ := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true})
	ctx := context.Background()
	for range 2 { // a duplicate ack (e.g. a retried job) must not decrement again
		if err := live.AckFlush(ctx, job); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := live.PendingFlushes(ctx, code); n != 0 {
		t.Errorf("pending_flush = %d, want 0 (not negative)", n)
	}
	if env.Redis.Exists(ctx, redisx.AnswersKey(string(code), "dq-01")).Val() != 0 {
		t.Error("answers not deleted")
	}
	if _, err := env.Redis.ZScore(ctx, redisx.SchedFlush, job.ID).Result(); !errors.Is(err, redis.Nil) {
		t.Error("job not removed")
	}
}

func TestReadAnswers(t *testing.T) {
	live, _ := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true, "u_2": false})
	got, err := live.ReadAnswers(context.Background(), code, job.QuestionID)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].ParticipantID < got[j].ParticipantID })
	if !got[0].Correct || got[0].Points < 100 || got[1].Correct || got[1].Points != 0 || got[0].ReceivedAt == 0 {
		t.Errorf("answers %+v", got)
	}
}

func TestReadFinal(t *testing.T) {
	live, _ := reset(t)
	playQuestion(t, map[string]bool{"u_1": true, "u_2": false})
	final, err := live.ReadFinal(context.Background(), code)
	if err != nil || len(final.Entries) != 3 {
		t.Fatalf("got %+v, %v", final, err)
	}
	if final.Entries[0].ParticipantID != "u_1" || final.Entries[0].DisplayName != "P-u_1" || final.Entries[0].Score < 100 {
		t.Errorf("first entry %+v", final.Entries[0])
	}
	if final.Status != quiz.StatusQuestionClosed {
		t.Errorf("status %s", final.Status)
	}
}

func TestRelease_RemovesTheRoomAndItsSchedules(t *testing.T) {
	live, _ := reset(t)
	playQuestion(t, map[string]bool{"u_1": true})
	ctx := context.Background()
	env.Redis.Set(ctx, "unrelated", "keep", 0)
	job := history.Job{ID: "final|" + string(code), Kind: history.Finalize, Code: code}
	env.Redis.ZAdd(ctx, redisx.SchedFlush, z(job.ID, 0))
	if err := live.Release(ctx, job); err != nil {
		t.Fatal(err)
	}
	k := string(code)
	for _, key := range []string{redisx.RoomKey(k), redisx.QuestionIDsKey(k), redisx.RosterKey(k), redisx.OnlineKey(k), redisx.LeaderboardKey(k)} {
		if env.Redis.Exists(ctx, key).Val() != 0 {
			t.Errorf("%s still exists", key)
		}
	}
	if env.Redis.ZScore(ctx, redisx.SchedFlush, job.ID).Err() == nil || env.Redis.ZScore(ctx, redisx.SchedTransitions, k).Err() == nil {
		t.Error("room still scheduled")
	}
	if env.Redis.Get(ctx, "unrelated").Val() != "keep" {
		t.Error("release touched another key")
	}
}

func TestDeferAndExtendTTL(t *testing.T) {
	live, _ := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true})
	ctx := context.Background()
	now := env.Redis.Time(ctx).Val().UnixMilli()
	if err := live.Defer(ctx, job, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if s := env.Redis.ZScore(ctx, redisx.SchedFlush, job.ID).Val(); int64(s) < now+2_000 || int64(s) > now+5_000 {
		t.Errorf("deferred to %v, want about now+2s (%d)", s, now+2_000)
	}
	k := string(code)
	env.Redis.Expire(ctx, redisx.RoomKey(k), time.Minute)
	env.Redis.Expire(ctx, redisx.AnswersKey(k, "dq-01"), time.Minute)
	if err := live.ExtendTTL(ctx, code, "dq-01", 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{redisx.RoomKey(k), redisx.RosterKey(k), redisx.LeaderboardKey(k), redisx.AnswersKey(k, "dq-01")} {
		if ttl := env.Redis.TTL(ctx, key).Val(); ttl < time.Hour {
			t.Errorf("%s TTL %v, want refreshed to about 2h (NFR-18)", key, ttl)
		}
	}
}

// ---- PostgreSQL store

func TestSaveAnswers_IsIdempotent(t *testing.T) {
	live, store := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true, "u_2": false})
	ctx := context.Background()
	answers, _ := live.ReadAnswers(ctx, code, job.QuestionID)
	for range 2 {
		if err := store.SaveAnswers(ctx, code, job.QuestionID, answers); err != nil {
			t.Fatal(err)
		}
	}
	if n := count(t, "SELECT count(*) FROM answers WHERE quiz_code = $1", string(code)); n != 2 {
		t.Errorf("%d rows after saving twice, want 2", n)
	}
}

func TestFinalize_WritesResultsReconcilesAndClosesTheQuiz(t *testing.T) {
	live, store := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true, "u_2": false})
	ctx := context.Background()
	answers, _ := live.ReadAnswers(ctx, code, job.QuestionID)
	if err := store.SaveAnswers(ctx, code, job.QuestionID, answers); err != nil {
		t.Fatal(err)
	}
	final, _ := live.ReadFinal(ctx, code)
	in := history.FinalizeInput{Code: code, Status: quiz.StatusFinished, Results: history.RankResults(final.Entries)}
	for range 2 { // idempotent
		mismatches, err := store.Finalize(ctx, in)
		if err != nil || len(mismatches) != 0 {
			t.Fatalf("finalize: %v, %v; want no mismatches", mismatches, err)
		}
	}
	if n := count(t, "SELECT count(*) FROM quiz_results WHERE quiz_code = $1", string(code)); n != 3 {
		t.Errorf("%d result rows, want 3 (including the participant who never answered)", n)
	}
	if n := count(t, "SELECT count(*) FROM quizzes WHERE code = $1 AND status = 'finished' AND finished_at IS NOT NULL", string(code)); n != 1 {
		t.Error("quiz not marked finished")
	}
}

func TestFinalize_DetectsAScoreMismatch(t *testing.T) {
	live, store := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true})
	ctx := context.Background()
	answers, _ := live.ReadAnswers(ctx, code, job.QuestionID)
	_ = store.SaveAnswers(ctx, code, job.QuestionID, answers)
	final, _ := live.ReadFinal(ctx, code)
	results := history.RankResults(final.Entries)
	results[0].Score += 14 // live leaderboard disagrees with the stored answers
	mismatches, err := store.Finalize(ctx, history.FinalizeInput{Code: code, Status: quiz.StatusFinished, Results: results})
	if err != nil || len(mismatches) != 1 || mismatches[0].ParticipantID != "u_1" || mismatches[0].Live != mismatches[0].Recomputed+14 {
		t.Errorf("got %+v, %v; want one mismatch for u_1 off by 14", mismatches, err)
	}
}

// ---- end to end through the service

func TestFlush_SavesAndReleasesTheAnswers(t *testing.T) {
	live, store := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true, "u_2": false})
	if err := newService(live, store, nil).Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if n := count(t, "SELECT count(*) FROM answers WHERE quiz_code = $1", string(code)); n != 2 {
		t.Errorf("%d rows saved, want 2", n)
	}
	if n, _ := live.PendingFlushes(context.Background(), code); n != 0 {
		t.Errorf("pending_flush %d", n)
	}
	if env.Redis.Exists(context.Background(), redisx.AnswersKey(string(code), "dq-01")).Val() != 0 {
		t.Error("answers still in Redis after a confirmed save")
	}
}

// FR-34 with a real outage: PostgreSQL unreachable → answers stay in Redis; on recovery the retry saves them.
func TestFlush_PostgresOutageKeepsAnswersInRedis(t *testing.T) {
	live, _ := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true, "u_2": true})
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, env.PostgresViaProxyDSN+"&connect_timeout=2")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	svc := newService(live, history.NewPostgresStore(pool), nil)

	proxy := env.Proxy(t, testenv.PostgresProxy)
	if err := proxy.Disable(); err != nil {
		t.Fatal(err)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := svc.Process(cctx, job); err == nil {
		t.Fatal("expected the save to fail while PostgreSQL is down")
	}
	if n := env.Redis.HLen(ctx, redisx.AnswersKey(string(code), "dq-01")).Val(); n != 2 {
		t.Errorf("%d answers left in Redis after a failed save, want all 2", n)
	}
	if n, _ := live.PendingFlushes(ctx, code); n != 1 {
		t.Errorf("pending_flush %d after a failed save, want 1", n)
	}

	if err := proxy.Enable(); err != nil {
		t.Fatal(err)
	}
	if err := svc.Process(ctx, job); err != nil {
		t.Fatalf("retry after recovery: %v", err)
	}
	if n := count(t, "SELECT count(*) FROM answers WHERE quiz_code = $1", string(code)); n != 2 {
		t.Errorf("%d rows after recovery, want 2", n)
	}
}

// A crash after the commit but before the acknowledgement: the retry must not duplicate rows.
func TestFlush_CrashBetweenCommitAndAckIsSafeToRetry(t *testing.T) {
	live, store := reset(t)
	job := playQuestion(t, map[string]bool{"u_1": true, "u_2": false})
	ctx := context.Background()
	crashing := &failAckOnce{LiveStore: live}
	svc := newService(crashing, store, nil)
	if err := svc.Process(ctx, job); err == nil {
		t.Fatal("expected the simulated crash")
	}
	if env.Redis.HLen(ctx, redisx.AnswersKey(string(code), "dq-01")).Val() != 2 {
		t.Error("answers were deleted although the acknowledgement failed")
	}
	if err := svc.Process(ctx, job); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if n := count(t, "SELECT count(*) FROM answers WHERE quiz_code = $1", string(code)); n != 2 {
		t.Errorf("%d rows after the retry, want 2 (no duplicates)", n)
	}
}

type failAckOnce struct {
	history.LiveStore
	failed bool
}

func (f *failAckOnce) AckFlush(ctx context.Context, job history.Job) error {
	if !f.failed {
		f.failed = true
		return errors.New("simulated crash before acknowledging")
	}
	return f.LiveStore.AckFlush(ctx, job)
}

func TestFinal_EndToEnd(t *testing.T) {
	live, store := reset(t)
	flushJob := playQuestion(t, map[string]bool{"u_1": true, "u_2": false})
	ctx := context.Background()
	mismatches := 0
	svc := newService(live, store, func(quiz.Code, []history.Mismatch) { mismatches++ })
	finalJob := history.Job{ID: "final|" + string(code), Kind: history.Finalize, Code: code}

	if err := svc.Process(ctx, finalJob); err != nil { // the question's flush is still pending → deferred
		t.Fatal(err)
	}
	if n := count(t, "SELECT count(*) FROM quiz_results WHERE quiz_code = $1", string(code)); n != 0 {
		t.Fatalf("finalized before the answers were flushed (%d results)", n)
	}
	if err := svc.Process(ctx, flushJob); err != nil {
		t.Fatal(err)
	}
	env.Redis.HSet(ctx, redisx.RoomKey(string(code)), "status", "finished")
	if err := svc.Process(ctx, finalJob); err != nil {
		t.Fatal(err)
	}
	if n := count(t, "SELECT count(*) FROM quiz_results WHERE quiz_code = $1", string(code)); n != 3 {
		t.Errorf("%d results, want 3", n)
	}
	if mismatches != 0 {
		t.Errorf("reconciliation reported %d mismatches, want 0 (FR-35)", mismatches)
	}
	if env.Redis.Exists(ctx, redisx.RoomKey(string(code))).Val() != 0 {
		t.Error("room not released from Redis after the final results were saved")
	}
	var status string
	_ = env.Postgres.QueryRow(ctx, "SELECT status FROM quizzes WHERE code = $1", string(code)).Scan(&status)
	if status != "finished" {
		t.Errorf("quiz status %q, want finished", status)
	}
}

func z(member string, score int64) redis.Z { return redis.Z{Score: float64(score), Member: member} }
