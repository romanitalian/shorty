package steps

import (
	"github.com/cucumber/godog"
)

// InitializeStatsSteps registers step definitions specific to stats.feature.
func InitializeStatsSteps(ctx *godog.ScenarioContext, sc *ScenarioContext) {
	// Stats data setup
	ctx.Step(`^I own a link with code "([^"]*)" that has received (\d+) total clicks from (\d+) unique visitors$`, sc.iOwnLinkWithClicksAndUniqueVisitors)
	ctx.Step(`^I own a link with code "([^"]*)" with clicks on "([^"]*)", "([^"]*)", and "([^"]*)"$`, sc.iOwnLinkWithClicksOnDates)
	ctx.Step(`^I own a link with code "([^"]*)" that has received clicks$`, sc.iOwnLinkWithClicks)
	ctx.Step(`^I own a link with code "([^"]*)" that has received (\d+) clicks$`, sc.iOwnLinkWithNClicks)
	ctx.Step(`^I own a link with code "([^"]*)" pointing to "([^"]*)" with (\d+) clicks$`, sc.iOwnLinkPointingToWithClicks)
	ctx.Step(`^I own a link with code "([^"]*)" with clicks spanning (\d+) days$`, sc.iOwnLinkWithClicksSpanningDays)
	ctx.Step(`^I own a link with code "([^"]*)" with clicks from (\d+) countries$`, sc.iOwnLinkWithClicksFromCountries)
	ctx.Step(`^user "([^"]*)" owns a link with code "([^"]*)"$`, sc.userOwnsLinkWithCode)

	// Geo data setup
	ctx.Step(`^I own a link with code "([^"]*)" with clicks from:$`, sc.iOwnLinkWithClicksFromTable)

	// Referrer data setup
	ctx.Step(`^I own a link with code "([^"]*)" with clicks from referrers:$`, sc.iOwnLinkWithClicksFromReferrers)

	// Device data setup
	ctx.Step(`^I own a link with code "([^"]*)" with clicks from devices:$`, sc.iOwnLinkWithClicksFromDevices)

	// Stats response assertions
	ctx.Step(`^the response contains "([^"]*)" equals (\d+)$`, sc.theResponseContainsFieldEqualsInt)
	ctx.Step(`^the response contains an array of date-count pairs$`, sc.responseContainsDateCountPairs)
	ctx.Step(`^each entry has "([^"]*)" and "([^"]*)" fields$`, sc.eachEntryHasFields)
	ctx.Step(`^the response contains an array of time-count pairs$`, sc.responseContainsTimeCountPairs)
	ctx.Step(`^the response contains countries sorted by click count descending$`, sc.responseContainsCountriesSorted)
	ctx.Step(`^the first entry is country "([^"]*)" with count (\d+)$`, sc.firstEntryIsCountryWithCount)
	ctx.Step(`^the response contains referrer domains sorted by click count descending$`, sc.responseContainsReferrersSorted)
	ctx.Step(`^direct traffic is represented as "([^"]*)"$`, sc.directTrafficRepresentedAs)
	ctx.Step(`^the response contains "([^"]*)" with entries for "([^"]*)", "([^"]*)", and "([^"]*)"$`, sc.responseContainsDeviceTypes)
	ctx.Step(`^the response only contains data from the last (\d+) days$`, sc.responseOnlyContainsDataFromLastDays)
	ctx.Step(`^the response contains exactly (\d+) country entries$`, sc.responseContainsExactlyCountryEntries)
	ctx.Step(`^the response contains a "([^"]*)" for pagination$`, sc.responseContainsPaginationField)

	// Click event processing
	ctx.Step(`^I wait up to (\d+) seconds$`, sc.iWaitUpToSeconds)
	ctx.Step(`^the click event is processed$`, sc.clickEventIsProcessed)
	ctx.Step(`^the stored click record contains "([^"]*)" as SHA-256 of IP with salt$`, sc.storedClickRecordContainsIPHash)
	ctx.Step(`^the stored click record does not contain "([^"]*)"$`, sc.storedClickRecordDoesNotContain)
	ctx.Step(`^a visitor with IP "([^"]*)" requests GET "([^"]*)"$`, sc.aVisitorWithIPRequestsGET)
}

func (sc *ScenarioContext) iOwnLinkWithClicksAndUniqueVisitors(code string, total, unique int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkWithClicksOnDates(code, d1, d2, d3 string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkWithClicks(code string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkWithNClicks(code string, count int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkPointingToWithClicks(code, url string, clicks int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkWithClicksSpanningDays(code string, days int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkWithClicksFromCountries(code string, countries int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) userOwnsLinkWithCode(user, code string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkWithClicksFromTable(code string, table *godog.Table) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkWithClicksFromReferrers(code string, table *godog.Table) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iOwnLinkWithClicksFromDevices(code string, table *godog.Table) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) theResponseContainsFieldEqualsInt(field string, value int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) responseContainsDateCountPairs() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) eachEntryHasFields(field1, field2 string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) responseContainsTimeCountPairs() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) responseContainsCountriesSorted() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) firstEntryIsCountryWithCount(country string, count int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) responseContainsReferrersSorted() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) directTrafficRepresentedAs(name string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) responseContainsDeviceTypes(field, type1, type2, type3 string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) responseOnlyContainsDataFromLastDays(days int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) responseContainsExactlyCountryEntries(count int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) responseContainsPaginationField(field string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) iWaitUpToSeconds(seconds int) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) clickEventIsProcessed() error {
	return godog.ErrPending
}

func (sc *ScenarioContext) storedClickRecordContainsIPHash(field string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) storedClickRecordDoesNotContain(value string) error {
	return godog.ErrPending
}

func (sc *ScenarioContext) aVisitorWithIPRequestsGET(ip, path string) error {
	return godog.ErrPending
}
