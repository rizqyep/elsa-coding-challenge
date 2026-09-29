//go:build integration

package session_test

import (
	"context"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

// In the lobby, leaving takes the player out entirely, so the host's count drops.
func TestLeave_LobbyRemovesThePlayer(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	join(t, repo, "u_1", "Rina")
	join(t, repo, "u_2", "Budi")
	env.Redis.Del(ctx, redisx.SchedLeaderboardDirty)

	out, err := repo.Leave(ctx, code, "u_1")
	if err != nil || out != session.LeftRemoved {
		t.Fatalf("Leave = %v, %v; want removed", out, err)
	}
	c := string(code)
	if n := env.Redis.ZCard(ctx, redisx.LeaderboardKey(c)).Val(); n != 1 {
		t.Errorf("leaderboard has %d players, want 1", n)
	}
	if env.Redis.HExists(ctx, redisx.RosterKey(c), "u_1").Val() {
		t.Error("u_1 still on the roster")
	}
	if err := env.Redis.ZScore(ctx, redisx.OnlineKey(c), "u_1").Err(); err == nil {
		t.Error("u_1 still marked online")
	}
	if !env.Redis.SIsMember(ctx, redisx.SchedLeaderboardDirty, c).Val() {
		t.Error("room not marked dirty, so the host never sees the lower count")
	}
	if res := join(t, repo, "u_1", "Rina"); res.ParticipantCount != 2 {
		t.Errorf("rejoin after leaving: count %d, want 2", res.ParticipantCount)
	}
}

// Once the quiz has started, a leaver keeps their score and place; they only stop counting as online.
func TestLeave_AfterStartKeepsTheScore(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	join(t, repo, "u_1", "Rina")
	c := string(code)
	env.Redis.HSet(ctx, redisx.RoomKey(c), "status", "question_open")
	env.Redis.ZAdd(ctx, redisx.LeaderboardKey(c), redisZ(150, "u_1"))

	out, err := repo.Leave(ctx, code, "u_1")
	if err != nil || out != session.LeftOffline {
		t.Fatalf("Leave = %v, %v; want offline", out, err)
	}
	if s := env.Redis.ZScore(ctx, redisx.LeaderboardKey(c), "u_1").Val(); s != 150 {
		t.Errorf("score %v after leaving, want 150", s)
	}
	if !env.Redis.HExists(ctx, redisx.RosterKey(c), "u_1").Val() {
		t.Error("u_1 dropped from the roster mid-quiz")
	}
	if err := env.Redis.ZScore(ctx, redisx.OnlineKey(c), "u_1").Err(); err == nil {
		t.Error("u_1 still online, so early close would wait for them")
	}
}

// A room the worker already released is not recreated by a late leave.
func TestLeave_ReleasedRoomIsLeftAlone(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	join(t, repo, "u_1", "Rina")
	c := string(code)
	env.Redis.Del(ctx, redisx.RoomKey(c), redisx.RosterKey(c), redisx.LeaderboardKey(c), redisx.OnlineKey(c))

	out, err := repo.Leave(ctx, code, "u_1")
	if err != nil || out != session.LeftGone {
		t.Fatalf("Leave = %v, %v; want gone", out, err)
	}
	if n := env.Redis.Exists(ctx, redisx.RoomKey(c), redisx.RosterKey(c), redisx.LeaderboardKey(c), redisx.OnlineKey(c)).Val(); n != 0 {
		t.Errorf("%d released keys recreated", n)
	}
}
