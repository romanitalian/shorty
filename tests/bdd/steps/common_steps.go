package steps

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// InitializeCommonSteps registers steps shared across multiple feature files.
func InitializeCommonSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	// --- Service availability ---
	ctx.Step(`^the redirect service is running on "([^"]*)"$`, sc.theRedirectServiceIsRunningOn)
	ctx.Step(`^the API service is running on "([^"]*)"$`, sc.theAPIServiceIsRunningOn)
	ctx.Step(`^the service is running$`, sc.theServiceIsRunning)

	// --- Link setup ---
	ctx.Step(`^a short link "([^"]*)" pointing to "([^"]*)" is active$`, sc.aShortLinkPointingToIsActive)
	ctx.Step(`^a short link "([^"]*)" pointing to "([^"]*)" exists$`, sc.aShortLinkPointingToExists)
	ctx.Step(`^a short link "([^"]*)" pointing to "([^"]*)" exists in Redis cache$`, sc.aShortLinkExistsInRedisCache)
	ctx.Step(`^a short link "([^"]*)" pointing to "([^"]*)" exists in DynamoDB but not in Redis$`, sc.aShortLinkExistsInDynamoDBOnly)
	ctx.Step(`^a short link with code "([^"]*)" already exists$`, sc.aShortLinkWithCodeAlreadyExists)
	ctx.Step(`^no short link exists with code "([^"]*)"$`, sc.noShortLinkExistsWithCode)
	ctx.Step(`^a short link "([^"]*)" pointing to a URL of (\d+) characters is active$`, sc.aShortLinkPointingToLongURLIsActive)
	ctx.Step(`^a short link "([^"]*)" pointing to "([^"]*)" exists with:$`, sc.aShortLinkExistsWith)

	// --- Link property modification ---
	ctx.Step(`^the link "([^"]*)" is configured for permanent redirect$`, sc.theLinkIsConfiguredForPermanentRedirect)
	ctx.Step(`^the link "([^"]*)" has expires_at set to (\d+) hour in the past$`, sc.theLinkHasExpiresAtInPast)
	ctx.Step(`^the link "([^"]*)" has max_clicks set to (\d+)$`, sc.theLinkHasMaxClicksSetTo)
	ctx.Step(`^the link "([^"]*)" has click_count equal to (\d+)$`, sc.theLinkHasClickCountEqualTo)
	ctx.Step(`^the link "([^"]*)" has is_active set to false$`, sc.theLinkIsDeactivated)
	ctx.Step(`^the link has already received (\d+) click$`, sc.theLinkHasAlreadyReceivedClicks)

	// --- SQS ---
	ctx.Step(`^the SQS queue is temporarily unavailable$`, sc.theSQSQueueIsUnavailable)

	// --- Authentication ---
	ctx.Step(`^I am authenticated as a free plan user$`, sc.iAmAuthenticatedAsFreePlanUser)
	ctx.Step(`^I am authenticated as a free user$`, sc.iAmAuthenticatedAsFreePlanUser)
	ctx.Step(`^I am not authenticated$`, sc.iAmNotAuthenticated)

	// --- HTTP actions ---
	ctx.Step(`^I request GET "([^"]*)"$`, sc.iRequestGET)
	ctx.Step(`^I request GET "([^"]*)" with headers:$`, sc.iRequestGETWithHeaders)
	ctx.Step(`^I request GET "([^"]*)" without a password$`, sc.iRequestGET)
	ctx.Step(`^a visitor requests GET "([^"]*)"$`, sc.iRequestGET)
	ctx.Step(`^I POST "([^"]*)" with JSON body:$`, sc.iPOSTWithJSONBody)
	ctx.Step(`^I POST "([^"]*)" with body:$`, sc.iPOSTWithTableBody)
	ctx.Step(`^I GET "([^"]*)"$`, sc.iRequestGET)
	ctx.Step(`^I send POST to "([^"]*)" with JSON:$`, sc.iPOSTWithJSONBody)
	ctx.Step(`^I send PATCH to "([^"]*)" with JSON:$`, sc.iPATCHWithJSONBody)
	ctx.Step(`^I send DELETE to "([^"]*)"$`, sc.iSendDELETE)

	// --- Response assertions ---
	ctx.Step(`^the response status is (\d+)$`, sc.theResponseStatusIs)
	ctx.Step(`^the response status is not (\d+)$`, sc.theResponseStatusIsNot)
	ctx.Step(`^the "([^"]*)" header is "([^"]*)"$`, sc.theHeaderIs)
	ctx.Step(`^the "([^"]*)" header is present$`, sc.theHeaderIsPresent)
	ctx.Step(`^the "([^"]*)" header contains "([^"]*)"$`, sc.theHeaderContains)
	ctx.Step(`^the "([^"]*)" header contains a Unix timestamp$`, sc.theHeaderContainsUnixTimestamp)
	ctx.Step(`^the "([^"]*)" header contains a positive integer$`, sc.theHeaderContainsPositiveInteger)
	ctx.Step(`^the Location header is "([^"]*)"$`, sc.theLocationHeaderIs)
	ctx.Step(`^the response body contains "([^"]*)"$`, sc.theResponseBodyContainsString)
	ctx.Step(`^the response body contains "([^"]*)" with value "([^"]*)"$`, sc.theResponseBodyContainsField)
	ctx.Step(`^the response body contains "([^"]*)" with value (\d+)$`, sc.theResponseBodyContainsFieldInt)
	ctx.Step(`^the response body contains "([^"]*)" with value (true|false)$`, sc.theResponseBodyContainsFieldBool)
	ctx.Step(`^the response body contains "([^"]*)" with a (\d+)-(\d+) character alphanumeric value$`, sc.theResponseBodyContainsAlphanumField)
	ctx.Step(`^the response body does not contain "([^"]*)"$`, sc.theResponseBodyDoesNotContain)
	ctx.Step(`^the response body contains "([^"]*)" matching my user ID$`, sc.theResponseBodyContainsMyUserID)
	ctx.Step(`^the response body contains "([^"]*)" set to approximately (\d+) hours from now$`, sc.theResponseBodyContainsApproxHoursFromNow)
	ctx.Step(`^the "([^"]*)" field is no more than (\d+) hours from now$`, sc.theFieldIsNoMoreThanHoursFromNow)
	ctx.Step(`^the response body contains "([^"]*)" matching "([^"]*)"$`, sc.theResponseBodyContainsMatching)
	ctx.Step(`^the response content type is "([^"]*)"$`, sc.theResponseContentTypeIs)
	ctx.Step(`^the response contains a "([^"]*)" header$`, sc.theHeaderIsPresent)
}

// --- Service availability ---

func (sc *ScenarioContext) theRedirectServiceIsRunningOn(url string) error {
	sc.RedirectBaseURL = url
	return godog.ErrPending
}

func (sc *ScenarioContext) theAPIServiceIsRunningOn(url string) error {
	sc.APIBaseURL = url
	return godog.ErrPending
}

func (sc *ScenarioContext) theServiceIsRunning() error {
	return godog.ErrPending
}

// --- Link setup ---

func (sc *ScenarioContext) aShortLinkPointingToIsActive(code, url string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) aShortLinkPointingToExists(code, url string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) aShortLinkExistsInRedisCache(code, url string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) aShortLinkExistsInDynamoDBOnly(code, url string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) aShortLinkWithCodeAlreadyExists(code string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) noShortLinkExistsWithCode(code string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) aShortLinkPointingToLongURLIsActive(code string, length int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) aShortLinkExistsWith(code, url string, table *godog.Table) error {
	return godog.ErrPending
}

// --- Link property modification ---

func (sc *ScenarioContext) theLinkIsConfiguredForPermanentRedirect(code string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theLinkHasExpiresAtInPast(code string, hours int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theLinkHasMaxClicksSetTo(code string, max int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theLinkHasClickCountEqualTo(code string, count int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theLinkIsDeactivated(code string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theLinkHasAlreadyReceivedClicks(count int) error {
	return godog.ErrPending
}

// --- SQS ---

func (sc *ScenarioContext) theSQSQueueIsUnavailable() error {
	return godog.ErrPending
}

// --- Authentication ---

func (sc *ScenarioContext) iAmAuthenticatedAsFreePlanUser() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iAmNotAuthenticated() error {
	sc.AuthToken = ""
	sc.UserID = ""
	return godog.ErrPending
}

// --- HTTP actions ---

func (sc *ScenarioContext) iRequestGET(path string) error {
	baseURL := sc.RedirectBaseURL
	if strings.HasPrefix(path, "/api/") {
		baseURL = sc.APIBaseURL
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	return sc.doRequest(req)
}

func (sc *ScenarioContext) iRequestGETWithHeaders(path string, table *godog.Table) error {
	baseURL := sc.RedirectBaseURL
	if strings.HasPrefix(path, "/api/") {
		baseURL = sc.APIBaseURL
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	for _, row := range table.Rows {
		if len(row.Cells) >= 2 {
			req.Header.Set(row.Cells[0].Value, row.Cells[1].Value)
		}
	}
	return sc.doRequest(req)
}

func (sc *ScenarioContext) iPOSTWithJSONBody(path string, body *godog.DocString) error {
	baseURL := sc.APIBaseURL
	if strings.HasPrefix(path, "/p/") {
		baseURL = sc.RedirectBaseURL
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewBufferString(body.Content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return sc.doRequest(req)
}

func (sc *ScenarioContext) iPOSTWithTableBody(path string, table *godog.Table) error {
	// Convert table to JSON-ish body — stub for now
	return godog.ErrPending
}

func (sc *ScenarioContext) iPATCHWithJSONBody(path string, body *godog.DocString) error {
	baseURL := sc.APIBaseURL
	req, err := http.NewRequest(http.MethodPatch, baseURL+path, bytes.NewBufferString(body.Content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return sc.doRequest(req)
}

func (sc *ScenarioContext) iSendDELETE(path string) error {
	req, err := http.NewRequest(http.MethodDelete, sc.APIBaseURL+path, nil)
	if err != nil {
		return err
	}
	return sc.doRequest(req)
}

// --- Response assertions ---

func (sc *ScenarioContext) theResponseStatusIs(expected int) error {
	if sc.LastStatusCode != expected {
		return fmt.Errorf("expected status %d, got %d; body: %s", expected, sc.LastStatusCode, string(sc.LastBody))
	}
	return nil
}

func (sc *ScenarioContext) theResponseStatusIsNot(notExpected int) error {
	if sc.LastStatusCode == notExpected {
		return fmt.Errorf("expected status NOT to be %d but it was", notExpected)
	}
	return nil
}

func (sc *ScenarioContext) theHeaderIs(header, expected string) error {
	got := sc.LastHeaders.Get(header)
	if got != expected {
		return fmt.Errorf("expected header %q to be %q, got %q", header, expected, got)
	}
	return nil
}

func (sc *ScenarioContext) theHeaderIsPresent(header string) error {
	if sc.LastHeaders.Get(header) == "" {
		return fmt.Errorf("expected header %q to be present, but it was not", header)
	}
	return nil
}

func (sc *ScenarioContext) theHeaderContains(header, substr string) error {
	got := sc.LastHeaders.Get(header)
	if !strings.Contains(got, substr) {
		return fmt.Errorf("expected header %q to contain %q, got %q", header, substr, got)
	}
	return nil
}

func (sc *ScenarioContext) theHeaderContainsUnixTimestamp(header string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theHeaderContainsPositiveInteger(header string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theLocationHeaderIs(expected string) error {
	return sc.theHeaderIs("Location", expected)
}

func (sc *ScenarioContext) theResponseBodyContainsString(substr string) error {
	if !strings.Contains(string(sc.LastBody), substr) {
		return fmt.Errorf("expected body to contain %q, got: %s", substr, string(sc.LastBody))
	}
	return nil
}

func (sc *ScenarioContext) theResponseBodyContainsField(field, value string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseBodyContainsFieldInt(field string, value int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseBodyContainsFieldBool(field, value string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseBodyContainsAlphanumField(field string, minLen, maxLen int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseBodyDoesNotContain(field string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseBodyContainsMyUserID(field string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseBodyContainsApproxHoursFromNow(field string, hours int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theFieldIsNoMoreThanHoursFromNow(field string, hours int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseBodyContainsMatching(field, pattern string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseContentTypeIs(expected string) error {
	ct := sc.LastHeaders.Get("Content-Type")
	if !strings.Contains(ct, expected) {
		return fmt.Errorf("expected Content-Type to contain %q, got %q", expected, ct)
	}
	return nil
}
