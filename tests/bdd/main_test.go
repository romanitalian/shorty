package bdd

import (
	"os"
	"testing"

	"github.com/cucumber/godog"

	"github.com/romanitalian/shorty/tests/bdd/steps"
)

func TestFeatures(t *testing.T) {
	format := "pretty"
	if f := os.Getenv("GODOG_FORMAT"); f != "" {
		format = f
	}

	suite := godog.TestSuite{
		ScenarioInitializer: steps.InitializeScenario,
		Options: &godog.Options{
			Format:   format,
			Paths:    []string{"features"},
			Tags:     os.Getenv("GODOG_TAGS"),
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("non-zero status returned, failed to run feature tests")
	}
}
