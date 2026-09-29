//go:build integration

package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/session"
)

func TestJoin_PublishesAKickNamingTheNewConnection(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	sub := env.Redis.Subscribe(ctx, redisx.RoomChannel(string(code)))
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Join(ctx, session.JoinInput{Code: code, ParticipantID: "u_1", DisplayName: "Rina", ConnID: "conn-new", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-sub.Channel():
		var ev struct{ T, P, K string }
		if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.T != "kick" || ev.P != "u_1" || ev.K != "conn-new" {
			t.Errorf("published %s, want a kick for u_1 keeping conn-new", msg.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("join published no kick")
	}
}

func TestJoin_WithoutAConnectionIDPublishesNothing(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	sub := env.Redis.Subscribe(ctx, redisx.RoomChannel(string(code)))
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	join(t, repo, "u_1", "Rina")
	select {
	case msg := <-sub.Channel():
		t.Errorf("published %s; a join with no connection ID has nothing to replace", msg.Payload)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestJoin_FinishedOrRejectedPublishesNoKick(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	env.Redis.HSet(ctx, redisx.RoomKey(string(code)), "status", "finished")
	sub := env.Redis.Subscribe(ctx, redisx.RoomChannel(string(code)))
	defer func() { _ = sub.Close() }()
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Join(ctx, session.JoinInput{Code: code, ParticipantID: "u_1", DisplayName: "Rina", ConnID: "c", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	select {
	case msg := <-sub.Channel():
		t.Errorf("published %s for a finished quiz", msg.Payload)
	case <-time.After(200 * time.Millisecond):
	}
}

func redisNowMs(t *testing.T) int64 {
	t.Helper()
	now, err := env.Redis.Time(context.Background()).Result()
	if err != nil {
		t.Fatal(err)
	}
	return now.UnixMilli()
}

func TestRefreshPresence_StampsRedisTime(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	join(t, repo, "u_1", "Rina")
	join(t, repo, "u_2", "Budi")
	online := redisx.OnlineKey(string(code))
	env.Redis.ZAdd(ctx, online, redisZ(0, "u_1"), redisZ(0, "u_2"))

	before := redisNowMs(t)
	if err := repo.RefreshPresence(ctx, code, []quiz.ParticipantID{"u_1", "u_2"}); err != nil {
		t.Fatal(err)
	}
	after := redisNowMs(t)
	for _, id := range []string{"u_1", "u_2"} {
		got := int64(env.Redis.ZScore(ctx, online, id).Val())
		if got < before || got > after {
			t.Errorf("%s stamped %d, want Redis TIME within [%d, %d]", id, got, before, after)
		}
	}
}

func TestRefreshPresence_LargeRoomInChunks(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	ids := make([]quiz.ParticipantID, 1_234)
	for i := range ids {
		ids[i] = quiz.ParticipantID(fmt.Sprintf("u_%04d", i))
	}
	if err := repo.RefreshPresence(ctx, code, ids); err != nil {
		t.Fatal(err)
	}
	if n := env.Redis.ZCard(ctx, redisx.OnlineKey(string(code))).Val(); n != int64(len(ids)) {
		t.Errorf("%d online, want %d", n, len(ids))
	}
}

func TestRefreshPresence_ReleasedRoomIsNotRecreated(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	env.Redis.Del(ctx, redisx.RoomKey(string(code)), redisx.OnlineKey(string(code)))
	if err := repo.RefreshPresence(ctx, code, []quiz.ParticipantID{"u_1"}); err != nil {
		t.Fatal(err)
	}
	if env.Redis.Exists(ctx, redisx.OnlineKey(string(code))).Val() != 0 {
		t.Error("refresh recreated the online set of a released room; it would never expire")
	}
}

func TestRefreshPresence_KeepsAnExpiry(t *testing.T) {
	repo := setup(t)
	ctx := context.Background()
	online := redisx.OnlineKey(string(code))
	env.Redis.Del(ctx, online) // the room exists but its online set doesn't yet
	if err := repo.RefreshPresence(ctx, code, []quiz.ParticipantID{"u_1"}); err != nil {
		t.Fatal(err)
	}
	if ttl := env.Redis.PTTL(ctx, online).Val(); ttl <= 0 {
		t.Errorf("online set TTL %v; it must expire with the room", ttl)
	}
}

func redisZ(score float64, member string) redis.Z { return redis.Z{Score: score, Member: member} }
