package redisx

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// Script is a Lua script called by hash; load it at startup with LoadScripts (TRD §4.1, §4.4).
type Script struct {
	Name   string
	script *redis.Script
}

// NewScript wraps Lua source under a name used in errors and logs.
func NewScript(name, src string) *Script {
	return &Script{Name: name, script: redis.NewScript(src)}
}

// Run executes the script.
func (s *Script) Run(ctx context.Context, c redis.Scripter, keys []string, args ...any) *redis.Cmd {
	return s.script.Run(ctx, c, keys, args...)
}

// EvalSha executes the script by hash only; use inside pipelines, after LoadScripts.
func (s *Script) EvalSha(ctx context.Context, c redis.Scripter, keys []string, args ...any) *redis.Cmd {
	return s.script.EvalSha(ctx, c, keys, args...)
}

// LoadScripts loads every script into Redis's script cache.
func LoadScripts(ctx context.Context, c redis.Scripter, scripts ...*Script) error {
	for _, s := range scripts {
		if err := s.script.Load(ctx, c).Err(); err != nil {
			return fmt.Errorf("load script %s: %w", s.Name, err)
		}
	}
	return nil
}
