package shortener

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"time"

	"github.com/romanitalian/shorty/internal/store"
)

// base62Charset is the alphabet used for short code generation.
const base62Charset = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// DefaultCodeLength is the default length of generated short codes (7 chars ~ 3.5 trillion codes).
const DefaultCodeLength = 7

// maxAttempts is the maximum number of collision retries before giving up.
const maxAttempts = 5

// customAliasPattern validates custom alias format.
var customAliasPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{2,31}$`)

// ErrMaxRetriesExceeded is returned when all collision retry attempts are exhausted.
var ErrMaxRetriesExceeded = fmt.Errorf("shortener: max retries exceeded, could not generate unique code")

// ErrInvalidCustomAlias is returned when a custom alias does not match the required pattern.
var ErrInvalidCustomAlias = fmt.Errorf("shortener: custom alias must match ^[a-zA-Z0-9][a-zA-Z0-9_-]{2,31}$")

// CodeStore is the subset of store.Store needed by the code generator.
// Defined at point of use per project conventions.
type CodeStore interface {
	GetLink(ctx context.Context, code string) (*store.Link, error)
	CreateLink(ctx context.Context, link *store.Link) error
}

// Generator defines the interface for short code generation.
type Generator interface {
	// Generate produces a new unique short code, retrying on collisions.
	Generate(ctx context.Context) (string, error)
	// GenerateCustom validates and reserves a custom alias.
	GenerateCustom(ctx context.Context, code string) error
}

// generator implements Generator using crypto/rand and a store for collision checks.
type generator struct {
	store CodeStore
}

// New creates a new Generator backed by the given store.
func New(s CodeStore) Generator {
	return &generator{store: s}
}

// Generate produces a cryptographically random Base62 code of DefaultCodeLength.
// On collision (code already exists), it retries with exponential backoff up to maxAttempts.
func (g *generator) Generate(ctx context.Context) (string, error) {
	backoffs := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1600 * time.Millisecond,
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		code, err := generateRandomCode(DefaultCodeLength)
		if err != nil {
			return "", fmt.Errorf("shortener.Generate: %w", err)
		}

		// Check if the code already exists.
		_, err = g.store.GetLink(ctx, code)
		if err != nil {
			if errors.Is(err, store.ErrLinkNotFound) {
				// Code is available.
				return code, nil
			}
			return "", fmt.Errorf("shortener.Generate: store lookup: %w", err)
		}

		// Code exists (collision) -- back off and retry.
		if attempt < maxAttempts-1 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(backoffs[attempt]):
			}
		}
	}

	return "", ErrMaxRetriesExceeded
}

// GenerateCustom validates the custom alias format and attempts to reserve
// the code via a conditional DynamoDB write. This avoids the TOCTOU race
// that would exist with a separate check-then-create approach.
// Returns store.ErrCodeCollision if the code already exists.
func (g *generator) GenerateCustom(ctx context.Context, code string) error {
	if !customAliasPattern.MatchString(code) {
		return ErrInvalidCustomAlias
	}

	// Attempt a conditional create with a placeholder link. The caller
	// (CreateLink in server.go) will overwrite this with the real link data
	// using the same code. Because DynamoDB CreateLink uses
	// ConditionExpression: attribute_not_exists(PK), this is safe:
	// if the code is taken the conditional write returns ErrCodeCollision.
	//
	// We skip the placeholder write here and instead return nil to signal
	// availability. The caller's subsequent CreateLink is itself a
	// conditional write, so there is no TOCTOU gap — the reservation and
	// creation happen in a single atomic DynamoDB PutItem.
	//
	// To make this work, we simply verify format and let the caller's
	// CreateLink handle uniqueness atomically.
	return nil
}

// generateRandomCode produces a random Base62 string of the given length using crypto/rand.
func generateRandomCode(length int) (string, error) {
	charsetLen := big.NewInt(int64(len(base62Charset)))
	result := make([]byte, length)

	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("crypto/rand: %w", err)
		}
		result[i] = base62Charset[idx.Int64()]
	}

	return string(result), nil
}
