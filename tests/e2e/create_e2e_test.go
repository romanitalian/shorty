//go:build e2e

package e2e

import "testing"

func TestCreateE2E_AuthenticatedBasicLink(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Authenticate, POST /api/v1/links with original_url
	// Assert 201, code field present, original_url matches
}

func TestCreateE2E_CustomAlias(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: POST /api/v1/links with custom_alias
	// Assert 201, code matches alias
}

func TestCreateE2E_DuplicateAlias(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link with alias, then create another with same alias
	// Assert 409 Conflict
}

func TestCreateE2E_AnonymousShorten(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: POST /api/v1/shorten without auth
	// Assert 201, expires_at within 24 hours
}

func TestCreateE2E_PasswordProtected(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link with password, assert has_password=true, password not in response
}

func TestCreateE2E_WithMaxClicks(t *testing.T) {
	t.Skip("E2E: not implemented yet -- requires deployed environment")
	// TODO: Create link with max_clicks, assert max_clicks and click_count=0
}
