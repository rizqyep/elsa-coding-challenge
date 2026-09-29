package redisx_test

import (
	"strings"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
)

func TestKeys_Names(t *testing.T) {
	const code = "K7Q2MX"
	// A slice, not a map keyed by the result: two functions wrongly returning the same
	// name would otherwise overwrite each other's case and go unnoticed.
	cases := []struct{ got, want string }{
		{redisx.RoomKey(code), "quiz:{K7Q2MX}:room"},
		{redisx.QuestionIDsKey(code), "quiz:{K7Q2MX}:qids"},
		{redisx.RosterKey(code), "quiz:{K7Q2MX}:roster"},
		{redisx.OnlineKey(code), "quiz:{K7Q2MX}:online"},
		{redisx.LeaderboardKey(code), "quiz:{K7Q2MX}:lb"},
		{redisx.AnswersKey(code, "syn-03"), "quiz:{K7Q2MX}:ans:syn-03"},
		{redisx.RoomChannel(code), "room:{K7Q2MX}"},
		{redisx.SchedTransitions, "sched:transitions"},
		{redisx.SchedLeaderboardDirty, "sched:lbdirty"},
		{redisx.SchedFlush, "sched:flush"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

// Every key and the channel of a room must land in the same Redis Cluster slot,
// so one Lua script can touch them all (TRD §4.1).
func TestKeys_RoomKeysShareOneClusterSlot(t *testing.T) {
	for _, code := range []string{"K7Q2MX", "23456A", "ZZZZZZ"} {
		keys := []string{
			redisx.RoomKey(code), redisx.QuestionIDsKey(code), redisx.RosterKey(code),
			redisx.OnlineKey(code), redisx.LeaderboardKey(code),
			redisx.AnswersKey(code, "syn-01"), redisx.AnswersKey(code, "biz-10"),
			redisx.RoomChannel(code),
		}
		want := clusterSlot(keys[0])
		for _, k := range keys[1:] {
			if got := clusterSlot(k); got != want {
				t.Errorf("%s: slot %d, want %d (same as %s)", k, got, want, keys[0])
			}
		}
	}
}

func TestClusterSlot_MatchesRedisSpecVector(t *testing.T) {
	// From the Redis Cluster spec: CRC16("123456789") = 0x31C3.
	if got := crc16([]byte("123456789")); got != 0x31C3 {
		t.Fatalf("crc16 = %#x, want 0x31C3", got)
	}
}

// clusterSlot implements the Redis Cluster key-to-slot rule, including hash tags.
func clusterSlot(key string) uint16 {
	if i := strings.IndexByte(key, '{'); i >= 0 {
		if j := strings.IndexByte(key[i+1:], '}'); j > 0 {
			key = key[i+1 : i+1+j]
		}
	}
	return crc16([]byte(key)) % 16384
}

// crc16 is CRC-16/XMODEM, the variant Redis Cluster uses.
func crc16(b []byte) uint16 {
	var crc uint16
	for _, c := range b {
		crc ^= uint16(c) << 8
		for range 8 {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
