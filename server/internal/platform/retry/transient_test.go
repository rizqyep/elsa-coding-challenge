package retry_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/platform/retry"
)

type redisErr string

func (e redisErr) Error() string { return string(e) }
func (e redisErr) RedisError()   {}

func TestTransient(t *testing.T) {
	transient := map[string]error{
		"deadline":            context.DeadlineExceeded,
		"wrapped deadline":    fmt.Errorf("answer: %w", context.DeadlineExceeded),
		"connection refused":  &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		"connection reset":    fmt.Errorf("read: %w", syscall.ECONNRESET),
		"broken pipe":         syscall.EPIPE,
		"eof":                 io.EOF,
		"unexpected eof":      fmt.Errorf("x: %w", io.ErrUnexpectedEOF),
		"redis pool timeout":  redis.ErrPoolTimeout,
		"redis closed":        redis.ErrClosed,
		"redis loading":       redisErr("LOADING Redis is loading the dataset in memory"),
		"redis failover":      redisErr("READONLY You can't write against a read only replica."),
		"redis master down":   redisErr("MASTERDOWN Link with MASTER is down"),
		"redis try again":     redisErr("TRYAGAIN Multiple keys request during rehashing of slot"),
		"postgres connect":    &pgconn.ConnectError{},
		"postgres shutdown":   &pgconn.PgError{Code: "57P01"},
		"postgres cannot run": &pgconn.PgError{Code: "57P03"},
	}
	for name, err := range transient {
		if !retry.Transient(err) {
			t.Errorf("%s: not transient", name)
		}
	}
	permanent := map[string]error{
		"nil":                  nil,
		"plain":                errors.New("unexpected script reply"),
		"canceled by caller":   context.Canceled,
		"redis script error":   redisErr("ERR user_script:1: attempt to call a nil value"),
		"postgres unique":      &pgconn.PgError{Code: "23505"},
		"postgres syntax":      &pgconn.PgError{Code: "42601"},
		"redis nil (no value)": redis.Nil,
	}
	for name, err := range permanent {
		if retry.Transient(err) {
			t.Errorf("%s: transient", name)
		}
	}
}
