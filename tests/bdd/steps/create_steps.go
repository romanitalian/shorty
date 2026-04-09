package steps

import (
	"github.com/cucumber/godog"
)

// InitializeCreateSteps registers step definitions specific to create_link.feature.
func InitializeCreateSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	// Response field assertions specific to link creation
	ctx.Step(`^the response body contains "([^"]*)" with value "([^"]*)"$`, sc.theResponseBodyContainsField)
	ctx.Step(`^the response body contains "([^"]*)"$`, sc.theResponseBodyContainsString)
	ctx.Step(`^the response "([^"]*)" is "([^"]*)"$`, sc.theResponseFieldIs)

	// Link accessibility
	ctx.Step(`^the created link code is cached in Redis$`, sc.createdLinkCodeIsCachedInRedis)
	ctx.Step(`^the link is accessible at the short URL$`, sc.theLinkIsAccessibleAtShortURL)

	// Quota / rate limit setup for creation
	ctx.Step(`^the requesting IP has already created (\d+) links in the current hour$`, sc.requestingIPHasCreatedLinksInCurrentHour)
	ctx.Step(`^the requesting IP has created (\d+) links in the current hour$`, sc.requestingIPHasCreatedLinksInCurrentHour)
	ctx.Step(`^I have already created (\d+) links today$`, sc.iHaveAlreadyCreatedLinksToday)
	ctx.Step(`^I have (\d+) total active links$`, sc.iHaveTotalActiveLinks)

	// Long URL creation
	ctx.Step(`^I POST "([^"]*)" with an original_url longer than (\d+) characters$`, sc.iPOSTWithLongOriginalURL)

	// TTL-specific creation
	ctx.Step(`^I created a short link "([^"]*)" with expires_at set to (\d+) hour from now pointing to "([^"]*)"$`, sc.iCreatedLinkWithExpiresAt)
	ctx.Step(`^I created a short link "([^"]*)" without expires_at or max_clicks pointing to "([^"]*)"$`, sc.iCreatedLinkWithoutTTL)

	// Response "expires_at" assertion
	ctx.Step(`^the response "([^"]*)" is no more than (\d+) hours from now$`, sc.theResponseFieldIsNoMoreThanHoursFromNow)
}

func (sc *ScenarioContext) theResponseFieldIs(field, value string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) createdLinkCodeIsCachedInRedis() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theLinkIsAccessibleAtShortURL() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) requestingIPHasCreatedLinksInCurrentHour(count int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iHaveAlreadyCreatedLinksToday(count int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iHaveTotalActiveLinks(count int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iPOSTWithLongOriginalURL(path string, maxLen int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iCreatedLinkWithExpiresAt(code string, hours int, url string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iCreatedLinkWithoutTTL(code, url string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseFieldIsNoMoreThanHoursFromNow(field string, hours int) error {
	return godog.ErrPending
}
