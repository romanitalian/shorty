package shortener

import (
	"context"
	"regexp"
	"testing"

	"github.com/romanitalian/shorty/internal/store"
)

// base62Regex matches only Base62 characters.
var base62Regex = regexp.MustCompile(`^[0-9a-zA-Z]+$`)

// mockStore implements CodeStore for testing.
type mockStore struct {
	links        map[string]*store.Link
	getCallCount int
}

func newMockStore() *mockStore {
	return &mockStore{links: make(map[string]*store.Link)}
}

func (m *mockStore) GetLink(_ context.Context, code string) (*store.Link, error) {
	m.getCallCount++
	if link, ok := m.links[code]; ok {
		return link, nil
	}
	return nil, store.ErrLinkNotFound
}

func (m *mockStore) CreateLink(_ context.Context, link *store.Link) error {
	if _, ok := m.links[link.Code]; ok {
		return store.ErrCodeCollision
	}
	m.links[link.Code] = link
	return nil
}

func TestGenerate_CodeLength(t *testing.T) {
	s := newMockStore()
	g := New(s)

	code, err := g.Generate(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(code) != DefaultCodeLength {
		t.Errorf("expected code length %d, got %d", DefaultCodeLength, len(code))
	}
}

func TestGenerate_Base62Only(t *testing.T) {
	s := newMockStore()
	g := New(s)

	for i := 0; i < 100; i++ {
		code, err := g.Generate(context.Background())
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
		if !base62Regex.MatchString(code) {
			t.Errorf("code %q contains non-Base62 characters", code)
		}
	}
}

func TestGenerate_CryptographicallyRandom(t *testing.T) {
	s := newMockStore()
	g := New(s)

	codes := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		code, err := g.Generate(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		codes[code] = struct{}{}
	}

	// With 62^7 possible codes, 50 random codes should all be unique.
	if len(codes) != 50 {
		t.Errorf("expected 50 unique codes, got %d (codes are not sufficiently random)", len(codes))
	}
}

func TestGenerate_CollisionRetrySucceeds(t *testing.T) {
	s := newMockStore()
	g := New(s).(*generator)

	// We can't predict what codes will be generated, so instead we use
	// a store that fails N times then succeeds. We wrap the mock.
	failCount := 3
	wrapped := &collidingStore{
		inner:        s,
		failsLeft:    failCount,
		collideCodes: make(map[string]bool),
	}
	g.store = wrapped

	code, err := g.Generate(context.Background())
	if err != nil {
		t.Fatalf("expected success after retries, got error: %v", err)
	}
	if len(code) != DefaultCodeLength {
		t.Errorf("expected code length %d, got %d", DefaultCodeLength, len(code))
	}
	// The store should have been called failCount + 1 times (failCount collisions + 1 success).
	if wrapped.callCount != failCount+1 {
		t.Errorf("expected %d store calls, got %d", failCount+1, wrapped.callCount)
	}
}

func TestGenerate_MaxRetriesExceeded(t *testing.T) {
	// A store that always reports collision (every code already exists).
	alwaysCollide := &collidingStore{
		inner:        newMockStore(),
		failsLeft:    maxAttempts + 10, // more than max attempts
		collideCodes: make(map[string]bool),
	}

	g := &generator{store: alwaysCollide}

	_, err := g.Generate(context.Background())
	if err != ErrMaxRetriesExceeded {
		t.Errorf("expected ErrMaxRetriesExceeded, got: %v", err)
	}
}

// collidingStore wraps a mock store and makes the first N GetLink calls
// return a found link (simulating collision).
type collidingStore struct {
	inner        *mockStore
	failsLeft    int
	callCount    int
	collideCodes map[string]bool
}

func (c *collidingStore) GetLink(ctx context.Context, code string) (*store.Link, error) {
	c.callCount++
	if c.failsLeft > 0 {
		c.failsLeft--
		c.collideCodes[code] = true
		// Return a link to simulate collision.
		return &store.Link{Code: code}, nil
	}
	return c.inner.GetLink(ctx, code)
}

func (c *collidingStore) CreateLink(ctx context.Context, link *store.Link) error {
	return c.inner.CreateLink(ctx, link)
}

func TestGenerateCustom_ValidAliases(t *testing.T) {
	s := newMockStore()
	g := New(s)

	valid := []string{
		"abc",
		"my-link",
		"MyLink123",
		"a_b",
		"a1-2_3",
		"abcdefghijklmnopqrstuvwxyz123456", // 32 chars
	}

	for _, alias := range valid {
		if err := g.GenerateCustom(context.Background(), alias); err != nil {
			t.Errorf("expected alias %q to be valid, got error: %v", alias, err)
		}
	}
}

func TestGenerateCustom_InvalidAliases(t *testing.T) {
	s := newMockStore()
	g := New(s)

	invalid := []string{
		"",           // empty
		"ab",         // too short (2 chars, need 3+)
		"-abc",       // starts with hyphen
		"_abc",       // starts with underscore
		"ab cd",      // contains space
		"ab!cd",      // special character
		"a",          // single char
		"abcdefghijklmnopqrstuvwxyz1234567", // 33 chars, over limit
	}

	for _, alias := range invalid {
		err := g.GenerateCustom(context.Background(), alias)
		if err != ErrInvalidCustomAlias {
			t.Errorf("expected ErrInvalidCustomAlias for %q, got: %v", alias, err)
		}
	}
}

func TestGenerateCustom_ExistingCode_NoCollisionCheck(t *testing.T) {
	// After B5 fix, GenerateCustom only validates format.
	// Collision detection is deferred to the caller's CreateLink (atomic conditional write).
	s := newMockStore()
	s.links["mylink"] = &store.Link{Code: "mylink"}
	g := New(s)

	err := g.GenerateCustom(context.Background(), "mylink")
	if err != nil {
		t.Errorf("expected nil (collision deferred to CreateLink), got: %v", err)
	}
}

func TestGenerateCustom_AvailableCode(t *testing.T) {
	s := newMockStore()
	g := New(s)

	err := g.GenerateCustom(context.Background(), "available")
	if err != nil {
		t.Errorf("expected nil, got: %v", err)
	}
}
