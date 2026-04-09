package steps

import (
	"github.com/cucumber/godog"
)

// InitializeRateLimitSteps registers step definitions specific to rate_limit.feature.
func InitializeRateLimitSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	// IP-specific rate limit setup
	ctx.Step(`^IP "([^"]*)" has made (\d+) redirect requests in the last (\d+) seconds$`, sc.ipHasMadeRedirectRequests)
	ctx.Step(`^IP "([^"]*)" requests GET "([^"]*)"$`, sc.ipRequestsGET)

	// Sliding window time manipulation
	ctx.Step(`^the requesting IP made (\d+) redirect requests between ([^ ]+) and ([^ ]+)$`, sc.requestingIPMadeRequestsBetween)
	ctx.Step(`^the requesting IP made (\d+) redirect requests at ([^ ]+)$`, sc.requestingIPMadeRequestsAt)
	ctx.Step(`^the current time is ([^ ]+)$`, sc.theCurrentTimeIs)

	// WAF-level blocking
	ctx.Step(`^a single IP sends more than (\d+) requests per minute$`, sc.singleIPSendsMoreThanPerMinute)
	ctx.Step(`^the WAF returns HTTP (\d+) before the request reaches the Lambda function$`, sc.wafReturnsHTTPBeforeLambda)
	ctx.Step(`^the Lambda invocation count does not increase$`, sc.lambdaInvocationCountDoesNotIncrease)

	// Because step (documentation — no-op)
	ctx.Step(`^the oldest requests have fallen out of the 1-minute sliding window$`, sc.noopDocumentation)
	ctx.Step(`^the (\d+) requests from ([^ ]+) have expired from the window$`, sc.noopDocumentation2)
}

func (sc *ScenarioContext) ipHasMadeRedirectRequests(ip string, count, seconds int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) ipRequestsGET(ip, path string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) requestingIPMadeRequestsBetween(count int, from, to string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) requestingIPMadeRequestsAt(count int, at string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theCurrentTimeIs(timeStr string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) singleIPSendsMoreThanPerMinute(count int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) wafReturnsHTTPBeforeLambda(status int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) lambdaInvocationCountDoesNotIncrease() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) noopDocumentation() error {
	// "Because" clauses are documentation only — no assertion needed.
	return nil
}

func (sc *ScenarioContext) noopDocumentation2(count int, time string) error {
	return nil
}
