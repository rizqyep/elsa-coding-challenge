package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Role is what a token allows its holder to do.
type Role string

// Roles (openapi.yaml Role).
const (
	RoleParticipant Role = "participant"
	RoleHost        Role = "host"
)

// MaxParticipantIDLength matches DevTokenRequest.participantId in openapi.yaml.
const MaxParticipantIDLength = 64

// ErrInvalidToken covers every rejected token: malformed, wrong key or algorithm, expired, or bad claims.
var ErrInvalidToken = errors.New("invalid token")

// Claims identify the caller.
type Claims struct {
	ParticipantID string
	Role          Role
	ExpiresAt     time.Time
}

// Tokens issues and verifies HS256 tokens for the mocked identity provider (TRD §2.2, D15).
type Tokens struct {
	Key []byte
	TTL time.Duration
	Now func() time.Time
}

type tokenClaims struct {
	Role Role `json:"role"`
	jwt.RegisteredClaims
}

// Issue signs a token for id with role.
func (t *Tokens) Issue(id string, role Role) (string, Claims, error) {
	if id == "" || len(id) > MaxParticipantIDLength {
		return "", Claims{}, fmt.Errorf("issue: participant id must be 1-%d bytes", MaxParticipantIDLength)
	}
	if !role.valid() {
		return "", Claims{}, fmt.Errorf("issue: unknown role %q", role)
	}
	exp := t.now().Add(t.TTL).Truncate(time.Second).UTC()
	c := tokenClaims{Role: role, RegisteredClaims: jwt.RegisteredClaims{Subject: id, ExpiresAt: jwt.NewNumericDate(exp)}}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(t.Key)
	if err != nil {
		return "", Claims{}, fmt.Errorf("issue: %w", err)
	}
	return s, Claims{ParticipantID: id, Role: role, ExpiresAt: exp}, nil
}

// Verify checks the signature, algorithm, expiry, and claims.
func (t *Tokens) Verify(token string) (Claims, error) {
	var c tokenClaims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return t.Key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	if c.Subject == "" || len(c.Subject) > MaxParticipantIDLength || !c.Role.valid() {
		return Claims{}, fmt.Errorf("%w: bad claims", ErrInvalidToken)
	}
	return Claims{ParticipantID: c.Subject, Role: c.Role, ExpiresAt: c.ExpiresAt.UTC()}, nil
}

func (t *Tokens) now() time.Time {
	if t.Now == nil {
		return time.Now()
	}
	return t.Now()
}

func (r Role) valid() bool { return r == RoleParticipant || r == RoleHost }

// NewParticipantID returns a random ID for callers that don't bring their own.
func NewParticipantID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("participant id: %w", err)
	}
	return "u_" + hex.EncodeToString(b[:]), nil
}
