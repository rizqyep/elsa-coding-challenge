package main

import (
	"strings"
	"testing"
	"time"
)

const bigRoom = `
name: big-room
questionSet: demo-quick
window: 10s
reveal: 3s
rooms: 1
participantsPerRoom: 5000
joinRampUp: 20s
answers:
  within: 2s
  correctRatio: 0.7
  noAnswerRatio: 0.02
slowClients: 0
chaos:
  - at: question:2+1s
    action: kill ws-1
assert: [nfr6, nfr7, nfr8, reconciliation]
`

func TestLoadScenario(t *testing.T) {
	s, err := LoadScenario([]byte(bigRoom))
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "big-room" || s.QuestionSet != "demo-quick" || s.Window != 10*time.Second || s.Reveal != 3*time.Second ||
		s.Rooms != 1 || s.ParticipantsPerRoom != 5000 || s.JoinRampUp != 20*time.Second ||
		s.Answers.Within != 2*time.Second || s.Answers.CorrectRatio != 0.7 || s.Answers.NoAnswerRatio != 0.02 {
		t.Errorf("parsed %+v", s)
	}
	if len(s.Chaos) != 1 || s.Chaos[0].Question != 2 || s.Chaos[0].After != time.Second || s.Chaos[0].Action.Kind != "kill" || s.Chaos[0].Action.Arg != "ws-1" {
		t.Errorf("chaos %+v", s.Chaos)
	}
	if strings.Join(s.Assert, ",") != "nfr6,nfr7,nfr8,reconciliation" {
		t.Errorf("assert %v", s.Assert)
	}
}

func TestLoadScenario_Rejects(t *testing.T) {
	for name, yaml := range map[string]string{
		"unknown field":   "name: x\nquestionSet: demo-quick\nrooms: 1\nparticipantsPerRoom: 1\nwindow: 5s\nreveal: 2s\nparticipants: 5\n",
		"no participants": "name: x\nquestionSet: demo-quick\nrooms: 1\nwindow: 5s\nreveal: 2s\n",
		"ratio above 1":   "name: x\nquestionSet: demo-quick\nrooms: 1\nparticipantsPerRoom: 1\nwindow: 5s\nreveal: 2s\nanswers: {correctRatio: 1.5}\n",
		"bad chaos at":    "name: x\nquestionSet: demo-quick\nrooms: 1\nparticipantsPerRoom: 1\nwindow: 5s\nreveal: 2s\nchaos: [{at: soon, action: kill ws-1}]\n",
		"bad action":      "name: x\nquestionSet: demo-quick\nrooms: 1\nparticipantsPerRoom: 1\nwindow: 5s\nreveal: 2s\nchaos: [{at: start+1s, action: explode}]\n",
		"unknown assert":  "name: x\nquestionSet: demo-quick\nrooms: 1\nparticipantsPerRoom: 1\nwindow: 5s\nreveal: 2s\nassert: [speed]\n",
	} {
		if _, err := LoadScenario([]byte(yaml)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestParseAt(t *testing.T) {
	for in, want := range map[string]At{
		"question:2+1s":    {Question: 2, After: time.Second},
		"question:1":       {Question: 1},
		"start+5s":         {After: 5 * time.Second},
		"question:3+250ms": {Question: 3, After: 250 * time.Millisecond},
	} {
		if got, err := ParseAt(in); err != nil || got != want {
			t.Errorf("ParseAt(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "question:0+1s", "question:x", "later", "start-1s"} {
		if _, err := ParseAt(bad); err == nil {
			t.Errorf("ParseAt(%q) accepted", bad)
		}
	}
}

func TestParseAction(t *testing.T) {
	for in, want := range map[string]Action{
		"kill ws-1":        {Kind: "kill", Arg: "ws-1"},
		"stop worker":      {Kind: "stop", Arg: "worker"},
		"redis-latency 20": {Kind: "redis-latency", N: 20},
		"redis-down 5s":    {Kind: "redis-down", D: 5 * time.Second},
		"pg-down 10s":      {Kind: "pg-down", D: 10 * time.Second},
	} {
		if got, err := ParseAction(in); err != nil || got != want {
			t.Errorf("ParseAction(%q) = %+v, %v; want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"kill", "redis-latency fast", "redis-down", "explode ws-1"} {
		if _, err := ParseAction(bad); err == nil {
			t.Errorf("ParseAction(%q) accepted", bad)
		}
	}
}

func TestApplyOverrides(t *testing.T) {
	s, err := LoadScenario([]byte(bigRoom))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyOverrides([]string{"participants=10000", "window=15s", "rooms=2", "chaos=off", "slow=0.05", "ramp=0s", "seed=9"}); err != nil {
		t.Fatal(err)
	}
	if s.ParticipantsPerRoom != 10000 || s.Window != 15*time.Second || s.Rooms != 2 || len(s.Chaos) != 0 || s.SlowClients != 0.05 || s.JoinRampUp != 0 || s.Seed != 9 {
		t.Errorf("after overrides %+v", s)
	}
	for _, bad := range []string{"participants=many", "nope=1", "window", "slow=2"} {
		if err := s.ApplyOverrides([]string{bad}); err == nil {
			t.Errorf("override %q accepted", bad)
		}
	}
}
