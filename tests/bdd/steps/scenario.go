package steps

import (
	"context"
	"io"
	"net/http"

	"github.com/cucumber/godog"
)

// ScenarioContext holds shared state for a single BDD scenario.
type ScenarioContext struct {
	// Service endpoints
	RedirectBaseURL string
	APIBaseURL      string

	// HTTP client (does not follow redirects automatically)
	Client *http.Client

	// Last HTTP response state
	LastStatusCode int
	LastHeaders    http.Header
	LastBody       []byte

	// Authentication
	AuthToken string
	UserID    string

	// CSRF token for password-protected link flows
	CSRFToken string

	// Tracking for cleanup
	CreatedLinkCodes []string
}

// NewScenarioContext creates a fresh context for each scenario.
func NewScenarioContext() *ScenarioContext {
	return &ScenarioContext{
		RedirectBaseURL: "http://localhost:8081",
		APIBaseURL:      "http://localhost:8080",
		Client: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse // don't follow redirects
			},
		},
	}
}

// doRequest executes an HTTP request and stores the response in the context.
func (sc *ScenarioContext) doRequest(req *http.Request) error {
	if sc.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+sc.AuthToken)
	}

	resp, err := sc.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	sc.LastStatusCode = resp.StatusCode
	sc.LastHeaders = resp.Header
	sc.LastBody, err = io.ReadAll(resp.Body)
	return err
}

// InitializeScenario wires all step definitions into a godog ScenarioContext.
func InitializeScenario(ctx *godog.ScenarioContext) {
	sc := NewScenarioContext()

	// Before/After hooks
	ctx.Before(func(ctx2 context.Context, sc2 *godog.Scenario) (context.Context, error) {
		return ctx2, nil
	})
	ctx.After(func(ctx2 context.Context, sc2 *godog.Scenario, err error) (context.Context, error) {
		// Cleanup created test data here in future
		return ctx2, nil
	})

	// Wire all step groups
	InitializeCommonSteps(ctx, sc)
	InitializeRedirectSteps(ctx, sc)
	InitializeCreateSteps(ctx, sc)
	InitializeRateLimitSteps(ctx, sc)
	InitializePasswordSteps(ctx, sc)
	InitializeStatsSteps(ctx, sc)
}
