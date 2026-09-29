package realtime

import (
	"hash/fnv"
	"sync"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// Member is what a connection joined: a participant, or the host watching (empty ParticipantID).
type Member struct {
	Code          quiz.Code
	SetID         quiz.QuestionSetID
	ParticipantID quiz.ParticipantID
	DisplayName   string
}

// registry holds this gateway's rooms, sharded by quiz code so rooms don't contend (TRD §7.4).
type registry struct{ shards []regShard }

type regShard struct {
	mu    sync.RWMutex
	rooms map[quiz.Code]*localRoom
}

type localRoom struct {
	setID        quiz.QuestionSetID
	conns        map[*Conn]quiz.ParticipantID
	participants map[quiz.ParticipantID]*Conn
	stateVer     int64
	lbVer        int64
}

// Version counters for dropping stale events: finished shares the state counter.
type counter int

const (
	stateCounter counter = iota
	lbCounter
)

func newRegistry(shards int) *registry {
	r := &registry{shards: make([]regShard, max(shards, 1))}
	for i := range r.shards {
		r.shards[i].rooms = map[quiz.Code]*localRoom{}
	}
	return r
}

func (r *registry) shard(code quiz.Code) *regShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(code))
	return &r.shards[int(h.Sum32())%len(r.shards)]
}

// add registers c and returns the participant's previous local connection, if any.
func (r *registry) add(c *Conn, m Member) (replaced *Conn) {
	s := r.shard(m.Code)
	s.mu.Lock()
	defer s.mu.Unlock()
	room := s.rooms[m.Code]
	if room == nil {
		room = &localRoom{setID: m.SetID, conns: map[*Conn]quiz.ParticipantID{}, participants: map[quiz.ParticipantID]*Conn{}}
		s.rooms[m.Code] = room
	}
	room.conns[c] = m.ParticipantID
	if m.ParticipantID != "" {
		if old := room.participants[m.ParticipantID]; old != c {
			replaced = old
		}
		room.participants[m.ParticipantID] = c
	}
	return replaced
}

// remove unregisters c and reports whether its room is now empty.
func (r *registry) remove(c *Conn, m Member) (removed, last bool) {
	s := r.shard(m.Code)
	s.mu.Lock()
	defer s.mu.Unlock()
	room := s.rooms[m.Code]
	if room == nil {
		return false, false
	}
	if _, ok := room.conns[c]; !ok {
		return false, false
	}
	delete(room.conns, c)
	if m.ParticipantID != "" && room.participants[m.ParticipantID] == c {
		delete(room.participants, m.ParticipantID)
	}
	if len(room.conns) == 0 {
		delete(s.rooms, m.Code)
		return true, true
	}
	return true, false
}

// conns copies the room's connections, so callers never touch sockets under the lock.
func (r *registry) conns(code quiz.Code) []*Conn {
	s := r.shard(code)
	s.mu.RLock()
	defer s.mu.RUnlock()
	room := s.rooms[code]
	if room == nil {
		return nil
	}
	out := make([]*Conn, 0, len(room.conns))
	for c := range room.conns {
		out = append(out, c)
	}
	return out
}

func (r *registry) participants(code quiz.Code) map[quiz.ParticipantID]*Conn {
	s := r.shard(code)
	s.mu.RLock()
	defer s.mu.RUnlock()
	room := s.rooms[code]
	if room == nil {
		return nil
	}
	out := make(map[quiz.ParticipantID]*Conn, len(room.participants))
	for id, c := range room.participants {
		out[id] = c
	}
	return out
}

func (r *registry) participant(code quiz.Code, id quiz.ParticipantID) *Conn {
	s := r.shard(code)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if room := s.rooms[code]; room != nil {
		return room.participants[id]
	}
	return nil
}

func (r *registry) setOf(code quiz.Code) (quiz.QuestionSetID, bool) {
	s := r.shard(code)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if room := s.rooms[code]; room != nil {
		return room.setID, true
	}
	return "", false
}

// advance records version v on the counter and reports whether it is newer than the last one seen.
func (r *registry) advance(code quiz.Code, which counter, v int64) bool {
	s := r.shard(code)
	s.mu.Lock()
	defer s.mu.Unlock()
	room := s.rooms[code]
	if room == nil {
		return false
	}
	last := &room.stateVer
	if which == lbCounter {
		last = &room.lbVer
	}
	if v <= *last {
		return false
	}
	*last = v
	return true
}

func (r *registry) has(code quiz.Code) bool {
	_, ok := r.setOf(code)
	return ok
}

func (r *registry) codes() []quiz.Code {
	var out []quiz.Code
	for i := range r.shards {
		s := &r.shards[i]
		s.mu.RLock()
		for code := range s.rooms {
			out = append(out, code)
		}
		s.mu.RUnlock()
	}
	return out
}
