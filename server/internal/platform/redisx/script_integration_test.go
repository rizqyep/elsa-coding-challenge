//go:build integration

package redisx_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/redisx"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/testenv"
)

var env *testenv.Env

func TestMain(m *testing.M) { os.Exit(testenv.Run(m, &env)) }

var echo = redisx.NewScript("echo", `return ARGV[1]`)

func TestScripts_LoadAndRun(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	if err := redisx.LoadScripts(ctx, env.Redis, echo); err != nil {
		t.Fatal(err)
	}
	if got, err := echo.EvalSha(ctx, env.Redis, nil, "hi").Text(); err != nil || got != "hi" {
		t.Errorf("EvalSha after load: %q, %v", got, err)
	}
}

// After SCRIPT FLUSH (e.g. a Redis restart), Run recovers on its own but EvalSha — the only
// option inside a pipeline — fails until scripts are loaded again (TRD §4.1).
func TestScripts_AfterFlush(t *testing.T) {
	env.Reset(t)
	ctx := context.Background()
	if err := redisx.LoadScripts(ctx, env.Redis, echo); err != nil {
		t.Fatal(err)
	}
	if err := env.Redis.ScriptFlush(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if err := echo.EvalSha(ctx, env.Redis, nil, "x").Err(); err == nil || !strings.Contains(err.Error(), "NOSCRIPT") {
		t.Errorf("EvalSha after flush: got %v, want NOSCRIPT", err)
	}
	if got, err := echo.Run(ctx, env.Redis, nil, "back").Text(); err != nil || got != "back" {
		t.Errorf("Run after flush: %q, %v", got, err)
	}
	if got, err := echo.EvalSha(ctx, env.Redis, nil, "again").Text(); err != nil || got != "again" {
		t.Errorf("EvalSha once Run reloaded it: %q, %v", got, err)
	}
}
