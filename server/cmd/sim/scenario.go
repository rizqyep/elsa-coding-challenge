package main

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Scenario is a simulation file in loadtest/scenarios (TRD §11.4).
type Scenario struct {
	Name                string
	QuestionSet         string
	Window, Reveal      time.Duration
	Rooms               int
	ParticipantsPerRoom int
	JoinRampUp          time.Duration
	RoomStagger         time.Duration // gap between room starts, for many-rooms
	Answers             struct {
		Within                      time.Duration
		CorrectRatio, NoAnswerRatio float64
	}
	SlowClients float64
	Chaos       []ChaosStep
	Assert      []string
	Seed        uint64
}

// ChaosStep is a timed fault: at question N (or the start) plus an offset.
type ChaosStep struct {
	At
	Action Action
	Text   string // as written, for the report
}

// At anchors a step; Question 0 means the quiz start.
type At struct {
	Question int
	After    time.Duration
}

// Action is a fault the stack supports (TRD §11.5).
type Action struct {
	Kind string        // kill, stop, redis-latency, redis-down, pg-down
	Arg  string        // container suffix for kill ("ws-1"), service for stop ("worker")
	N    int           // milliseconds for redis-latency
	D    time.Duration // how long for redis-down and pg-down
}

// Asserts the simulator knows, each checked against its NFR target (see targets in report.go).
var knownAsserts = []string{"nfr6", "nfr7", "nfr8", "nfr9", "transition-lag", "reconciliation", "no-errors", "rejoin"}

type scenarioFile struct {
	Name                string        `yaml:"name"`
	QuestionSet         string        `yaml:"questionSet"`
	Window              time.Duration `yaml:"window"`
	Reveal              time.Duration `yaml:"reveal"`
	Rooms               int           `yaml:"rooms"`
	ParticipantsPerRoom int           `yaml:"participantsPerRoom"`
	JoinRampUp          time.Duration `yaml:"joinRampUp"`
	RoomStagger         time.Duration `yaml:"roomStagger"`
	Answers             struct {
		Within        time.Duration `yaml:"within"`
		CorrectRatio  float64       `yaml:"correctRatio"`
		NoAnswerRatio float64       `yaml:"noAnswerRatio"`
	} `yaml:"answers"`
	SlowClients float64 `yaml:"slowClients"`
	Chaos       []struct {
		At     string `yaml:"at"`
		Action string `yaml:"action"`
	} `yaml:"chaos"`
	Assert []string `yaml:"assert"`
	Seed   uint64   `yaml:"seed"`
}

// LoadScenario parses and validates a scenario; unknown fields are errors, so a typo can't silently do nothing.
func LoadScenario(b []byte) (*Scenario, error) {
	var f scenarioFile
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("scenario: %w", err)
	}
	s := &Scenario{Name: f.Name, QuestionSet: f.QuestionSet, Window: f.Window, Reveal: f.Reveal, Rooms: f.Rooms,
		ParticipantsPerRoom: f.ParticipantsPerRoom, JoinRampUp: f.JoinRampUp, RoomStagger: f.RoomStagger,
		SlowClients: f.SlowClients, Assert: f.Assert, Seed: f.Seed}
	s.Answers.Within, s.Answers.CorrectRatio, s.Answers.NoAnswerRatio = f.Answers.Within, f.Answers.CorrectRatio, f.Answers.NoAnswerRatio
	for _, c := range f.Chaos {
		at, err := ParseAt(c.At)
		if err != nil {
			return nil, err
		}
		a, err := ParseAction(c.Action)
		if err != nil {
			return nil, err
		}
		s.Chaos = append(s.Chaos, ChaosStep{At: at, Action: a, Text: c.At + ": " + c.Action})
	}
	return s, s.validate()
}

func (s *Scenario) validate() error {
	var errs []error
	if s.Name == "" || s.QuestionSet == "" {
		errs = append(errs, errors.New("name and questionSet are required"))
	}
	if s.Rooms < 1 || s.ParticipantsPerRoom < 1 {
		errs = append(errs, errors.New("rooms and participantsPerRoom must be at least 1"))
	}
	if s.Window < 5*time.Second || s.Window > 120*time.Second || s.Reveal < 2*time.Second || s.Reveal > 30*time.Second {
		errs = append(errs, errors.New("window must be 5–120 s and reveal 2–30 s (the API's limits)"))
	}
	for name, r := range map[string]float64{"correctRatio": s.Answers.CorrectRatio, "noAnswerRatio": s.Answers.NoAnswerRatio, "slowClients": s.SlowClients} {
		if r < 0 || r > 1 {
			errs = append(errs, fmt.Errorf("%s must be between 0 and 1", name))
		}
	}
	for _, a := range s.Assert {
		if !slices.Contains(knownAsserts, a) {
			errs = append(errs, fmt.Errorf("unknown assert %q (known: %v)", a, knownAsserts))
		}
	}
	return errors.Join(errs...)
}

// ParseAt reads "question:N[+offset]" or "start+offset".
func ParseAt(s string) (At, error) {
	var at At
	anchor, off, hasOff := strings.Cut(s, "+")
	if hasOff {
		d, err := time.ParseDuration(off)
		if err != nil || d < 0 {
			return At{}, fmt.Errorf("chaos at %q: bad offset", s)
		}
		at.After = d
	}
	switch {
	case anchor == "start" && hasOff:
	case strings.HasPrefix(anchor, "question:"):
		n, err := strconv.Atoi(strings.TrimPrefix(anchor, "question:"))
		if err != nil || n < 1 {
			return At{}, fmt.Errorf("chaos at %q: question must be 1 or more", s)
		}
		at.Question = n
	default:
		return At{}, fmt.Errorf("chaos at %q: want question:N+offset or start+offset", s)
	}
	return at, nil
}

// ParseAction reads "kill ws-1", "stop worker", "redis-latency 20", "redis-down 5s", or "pg-down 10s".
func ParseAction(s string) (Action, error) {
	kind, arg, _ := strings.Cut(strings.TrimSpace(s), " ")
	a := Action{Kind: kind}
	var err error
	switch kind {
	case "kill", "stop":
		a.Arg = arg
		if arg == "" {
			err = errors.New("needs a target")
		}
	case "redis-latency":
		a.N, err = strconv.Atoi(arg)
	case "redis-down", "pg-down":
		a.D, err = time.ParseDuration(arg)
	default:
		err = errors.New("unknown action")
	}
	if err != nil {
		return Action{}, fmt.Errorf("chaos action %q: %w", s, err)
	}
	return a, nil
}

// ApplyOverrides applies key=value pairs from the command line (TRD §11.4).
func (s *Scenario) ApplyOverrides(kvs []string) error {
	for _, kv := range kvs {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return fmt.Errorf("override %q: want key=value", kv)
		}
		var err error
		switch strings.ToLower(k) {
		case "participants":
			s.ParticipantsPerRoom, err = strconv.Atoi(v)
		case "rooms":
			s.Rooms, err = strconv.Atoi(v)
		case "window":
			s.Window, err = time.ParseDuration(v)
		case "reveal":
			s.Reveal, err = time.ParseDuration(v)
		case "ramp":
			s.JoinRampUp, err = time.ParseDuration(v)
		case "within":
			s.Answers.Within, err = time.ParseDuration(v)
		case "slow":
			s.SlowClients, err = strconv.ParseFloat(v, 64)
		case "seed":
			s.Seed, err = strconv.ParseUint(v, 10, 64)
		case "chaos":
			if v != "off" {
				err = errors.New("only chaos=off")
			}
			s.Chaos = nil
		default:
			err = errors.New("unknown key")
		}
		if err != nil {
			return fmt.Errorf("override %q: %w", kv, err)
		}
	}
	return s.validate()
}
