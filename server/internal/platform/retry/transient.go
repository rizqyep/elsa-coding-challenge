package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
)

// Transient reports whether err is an infrastructure failure that may succeed later (TRD §9.1):
// timeouts, broken connections, and Redis or PostgreSQL being unavailable or failing over.
func Transient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr net.Error
	var pgConnect *pgconn.ConnectError
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) || errors.As(err, &pgConnect) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, redis.ErrPoolTimeout) || errors.Is(err, redis.ErrClosed) {
		return true
	}
	var redisErr redis.Error
	if errors.As(err, &redisErr) {
		for _, p := range []string{"LOADING", "READONLY", "MASTERDOWN", "TRYAGAIN", "CLUSTERDOWN"} {
			if strings.HasPrefix(redisErr.Error(), p) {
				return true
			}
		}
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// 08: connection exception; 57P: shutdown or not accepting connections; 53300: too many connections.
		return strings.HasPrefix(pgErr.Code, "08") || strings.HasPrefix(pgErr.Code, "57P") || pgErr.Code == "53300"
	}
	return false
}
