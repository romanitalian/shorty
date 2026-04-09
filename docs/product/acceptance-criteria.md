# Shorty Acceptance Criteria

All acceptance criteria are written in Given/When/Then format, ready for direct use as Gherkin BDD scenarios in `tests/bdd/features/`.

---

## Feature: Link Creation (Anonymous)

```gherkin
Feature: Anonymous link creation
  As an anonymous visitor
  I want to shorten a URL without registering
  So that I can share a shorter URL quickly

  Scenario: Create a short link with valid URL
    Given I am not authenticated
    When I POST to "/api/v1/shorten" with body:
      | original_url | https://example.com/very/long/path |
    Then I receive HTTP 201
    And the response contains a "short_url" field
    And the response contains a "code" field with 7-8 alphanumeric characters
    And the response contains "expires_at" set to 24 hours from now

  Scenario: Create a short link with empty URL
    Given I am not authenticated
    When I POST to "/api/v1/shorten" with body:
      | original_url | |
    Then I receive HTTP 400
    And the response contains error "original_url is required"

  Scenario: Create a short link with invalid URL
    Given I am not authenticated
    When I POST to "/api/v1/shorten" with body:
      | original_url | not-a-valid-url |
    Then I receive HTTP 400
    And the response contains error "original_url must be a valid HTTP or HTTPS URL"

  Scenario: Create a short link with URL exceeding max length
    Given I am not authenticated
    When I POST to "/api/v1/shorten" with a URL longer than 2048 characters
    Then I receive HTTP 400
    And the response contains error "original_url exceeds maximum length of 2048 characters"

  Scenario: Anonymous daily limit exceeded
    Given I am not authenticated
    And I have already created 5 links today from my IP
    When I POST to "/api/v1/shorten" with a valid URL
    Then I receive HTTP 429
    And the response contains a "Retry-After" header

  Scenario: Anonymous link has max 24-hour TTL
    Given I am not authenticated
    When I POST to "/api/v1/shorten" with a valid URL
    Then I receive HTTP 201
    And the link "expires_at" is no more than 24 hours from now
```

---

## Feature: Link Creation (Authenticated)

```gherkin
Feature: Authenticated link creation
  As an authenticated user
  I want to create short links with custom options
  So that I can control link behavior and track performance

  Scenario: Create a basic short link
    Given I am authenticated as a free user
    When I POST to "/api/v1/links" with body:
      | original_url | https://example.com |
    Then I receive HTTP 201
    And the response contains "code", "short_url", "original_url", "created_at"
    And "owner_id" matches my user ID

  Scenario: Create a link with time-based TTL
    Given I am authenticated as a free user
    When I POST to "/api/v1/links" with body:
      | original_url | https://example.com |
      | expires_at   | 1735689600          |
    Then I receive HTTP 201
    And the link "expires_at" equals 1735689600

  Scenario: Create a link with click-based TTL
    Given I am authenticated as a free user
    When I POST to "/api/v1/links" with body:
      | original_url | https://example.com |
      | max_clicks   | 100                 |
    Then I receive HTTP 201
    And the link "max_clicks" equals 100
    And the link "click_count" equals 0

  Scenario: Create a link with password protection
    Given I am authenticated as a free user
    When I POST to "/api/v1/links" with body:
      | original_url | https://example.com |
      | password     | s3cret              |
    Then I receive HTTP 201
    And the link has a password set
    And the password is not returned in the response

  Scenario: Create a link with custom alias
    Given I am authenticated as a free user
    And no link exists with code "my-brand"
    When I POST to "/api/v1/links" with body:
      | original_url | https://example.com |
      | custom_alias | my-brand            |
    Then I receive HTTP 201
    And the link "code" equals "my-brand"

  Scenario: Create a link with duplicate custom alias
    Given I am authenticated as a free user
    And a link already exists with code "taken"
    When I POST to "/api/v1/links" with body:
      | original_url | https://example.com |
      | custom_alias | taken               |
    Then I receive HTTP 409
    And the response contains error "alias already in use"

  Scenario: Create a link with title
    Given I am authenticated as a free user
    When I POST to "/api/v1/links" with body:
      | original_url | https://example.com |
      | title        | My Campaign Link    |
    Then I receive HTTP 201
    And the link "title" equals "My Campaign Link"

  Scenario: Free user daily quota exceeded
    Given I am authenticated as a free user
    And I have created 50 links today
    When I POST to "/api/v1/links" with a valid URL
    Then I receive HTTP 429
    And the response contains error "daily link limit reached"

  Scenario: Free user total quota exceeded
    Given I am authenticated as a free user
    And I have 500 total active links
    When I POST to "/api/v1/links" with a valid URL
    Then I receive HTTP 429
    And the response contains error "total link limit reached"

  Scenario: Unauthenticated request to authenticated endpoint
    Given I am not authenticated
    When I POST to "/api/v1/links" with a valid URL
    Then I receive HTTP 401
```

---

## Feature: Redirect

```gherkin
Feature: URL redirect
  As a visitor
  I want short links to redirect me to the original URL
  So that I can reach the destination quickly

  Scenario: Redirect to active link
    Given a short link "abc123" exists pointing to "https://example.com" and is active
    When I request GET /abc123
    Then I am redirected to "https://example.com" with HTTP 302

  Scenario: Redirect records a click event
    Given a short link "abc123" exists pointing to "https://example.com" and is active
    When I request GET /abc123
    Then I am redirected with HTTP 302
    And a click event is recorded asynchronously with hashed IP, User-Agent, Referer, and timestamp

  Scenario: Redirect nonexistent link
    Given no link exists with code "notfound"
    When I request GET /notfound
    Then I receive HTTP 404

  Scenario: Redirect to time-expired link
    Given a short link "expired1" exists with expires_at in the past
    When I request GET /expired1
    Then I receive HTTP 410 Gone

  Scenario: Redirect to click-limited link (within limit)
    Given a short link "lim5" exists with max_clicks=5 and click_count=3
    When I request GET /lim5
    Then I am redirected with HTTP 302
    And the click_count is incremented to 4

  Scenario: Redirect to click-limited link (at limit)
    Given a short link "lim1" exists with max_clicks=1 and click_count=0
    When I request GET /lim1
    Then I am redirected with HTTP 302
    And the click_count is incremented to 1

  Scenario: Redirect to click-limited link (limit reached)
    Given a short link "limx" exists with max_clicks=1 and click_count=1
    When I request GET /limx
    Then I receive HTTP 410 Gone

  Scenario: Redirect to deactivated link
    Given a short link "deact" exists with is_active=false
    When I request GET /deact
    Then I receive HTTP 410 Gone

  Scenario: Redirect latency within SLA
    Given a short link "perf1" exists pointing to "https://example.com" and is active
    When I request GET /perf1
    Then the response is received within 100 ms (p99)

  Scenario: Cache-first redirect (cache hit)
    Given a short link "cached1" exists in Redis cache
    When I request GET /cached1
    Then I am redirected with HTTP 302
    And the link is served from cache without hitting DynamoDB

  Scenario: Cache-miss redirect (fallback to DynamoDB)
    Given a short link "nocache1" exists in DynamoDB but not in Redis cache
    When I request GET /nocache1
    Then I am redirected with HTTP 302
    And the link is fetched from DynamoDB and stored in Redis cache
```

---

## Feature: Password-Protected Links

```gherkin
Feature: Password-protected links
  As a visitor
  I want to enter a password to access a protected link
  So that only authorized users reach the destination

  Scenario: Access password-protected link shows form
    Given a short link "secret1" exists with a password set
    When I request GET /secret1
    Then I receive HTTP 200 with an HTML form
    And the form contains a CSRF token
    And the form action is POST /p/secret1

  Scenario: Submit correct password
    Given a short link "secret1" exists with password "correct-password"
    And I have a valid CSRF token for "secret1"
    When I POST to "/p/secret1" with password "correct-password" and the CSRF token
    Then I am redirected to the original URL with HTTP 302

  Scenario: Submit incorrect password
    Given a short link "secret1" exists with password "correct-password"
    And I have a valid CSRF token for "secret1"
    When I POST to "/p/secret1" with password "wrong-password" and the CSRF token
    Then I receive HTTP 401
    And I see the password form again with an error message

  Scenario: Submit without CSRF token
    Given a short link "secret1" exists with a password set
    When I POST to "/p/secret1" with password "any" and no CSRF token
    Then I receive HTTP 403

  Scenario: Password brute-force protection
    Given a short link "secret1" exists with a password set
    And I have submitted 5 incorrect passwords for "secret1" in the last 15 minutes from my IP
    When I POST to "/p/secret1" with any password
    Then I receive HTTP 429
    And the response contains a "Retry-After" header
```

---

## Feature: TTL Expiry

```gherkin
Feature: Link TTL expiry
  As a link owner
  I want links to automatically expire based on time or click count
  So that links have a controlled lifespan

  Scenario: Link expires after time-based TTL
    Given I created a short link "ttl1" with expires_at set to 1 minute from now
    And the link is currently active
    When 2 minutes have passed
    And a visitor requests GET /ttl1
    Then the visitor receives HTTP 410 Gone

  Scenario: Link expires after reaching max clicks
    Given I created a short link "clk1" with max_clicks=3
    And 2 clicks have already been recorded
    When a visitor requests GET /clk1
    Then the visitor is redirected with HTTP 302
    And the click_count becomes 3
    When another visitor requests GET /clk1
    Then that visitor receives HTTP 410 Gone

  Scenario: Link with both time and click TTL (time expires first)
    Given I created a short link "both1" with expires_at in the past and max_clicks=100
    When a visitor requests GET /both1
    Then the visitor receives HTTP 410 Gone

  Scenario: Link with both time and click TTL (clicks exhaust first)
    Given I created a short link "both2" with expires_at in the future and max_clicks=1 and click_count=1
    When a visitor requests GET /both2
    Then the visitor receives HTTP 410 Gone

  Scenario: Anonymous link enforced 24-hour TTL
    Given I am not authenticated
    When I create a short link via POST /api/v1/shorten
    Then the link expires_at is set to exactly 24 hours from creation
    And the link cannot be accessed after 24 hours
```

---

## Feature: Rate Limiting

```gherkin
Feature: Rate limiting
  As the system
  I want to enforce rate limits at multiple tiers
  So that the service is protected from abuse and resources are fairly shared

  Scenario: Anonymous redirect rate limit
    Given I am not authenticated
    And I have made 200 redirect requests in the current minute from my IP
    When I request GET /{any-code}
    Then I receive HTTP 429
    And the response contains a "Retry-After" header

  Scenario: Anonymous redirect within limit
    Given I am not authenticated
    And I have made 100 redirect requests in the current minute from my IP
    When I request GET /valid-code
    Then I am redirected normally with HTTP 302

  Scenario: Anonymous creation rate limit
    Given I am not authenticated
    And I have created 5 links in the current hour from my IP
    When I POST to "/api/v1/shorten" with a valid URL
    Then I receive HTTP 429
    And the response contains a "Retry-After" header

  Scenario: Free user daily creation quota
    Given I am authenticated as a free user
    And I have created 50 links today
    When I POST to "/api/v1/links" with a valid URL
    Then I receive HTTP 429
    And the response body contains "daily link limit reached"

  Scenario: Free user total active link quota
    Given I am authenticated as a free user
    And I have 500 total active links
    When I POST to "/api/v1/links" with a valid URL
    Then I receive HTTP 429
    And the response body contains "total link limit reached"

  Scenario: Rate limit sliding window accuracy
    Given I am not authenticated
    And I made 200 redirect requests between 12:00:00 and 12:00:30
    When I request GET /{any-code} at 12:01:01
    Then I am redirected normally
    Because the oldest requests have fallen out of the 1-minute window
```

---

## Feature: User Dashboard

```gherkin
Feature: User dashboard
  As an authenticated user
  I want to manage my links and view statistics
  So that I can track and control my short links

  Scenario: List all my links
    Given I am authenticated as a free user
    And I have created 25 links
    When I GET "/api/v1/links"
    Then I receive HTTP 200
    And the response contains 20 links (default page size)
    And the response contains a "next_cursor" for pagination
    And links are sorted by created_at descending

  Scenario: List links with pagination
    Given I am authenticated as a free user
    And I have created 25 links
    When I GET "/api/v1/links?limit=10"
    Then I receive HTTP 200
    And the response contains 10 links
    And the response contains a "next_cursor"
    When I GET "/api/v1/links?limit=10&cursor={next_cursor}"
    Then I receive HTTP 200
    And the response contains 10 links
    And the response contains a "next_cursor"

  Scenario: Filter active links
    Given I am authenticated as a free user
    And I have 10 active links and 5 expired links
    When I GET "/api/v1/links?status=active"
    Then I receive HTTP 200
    And the response contains exactly 10 links
    And all links have is_active=true

  Scenario: Filter expired links
    Given I am authenticated as a free user
    And I have 10 active links and 5 expired links
    When I GET "/api/v1/links?status=expired"
    Then I receive HTTP 200
    And the response contains exactly 5 links

  Scenario: View link details
    Given I am authenticated as a free user
    And I own a link with code "mylink1"
    When I GET "/api/v1/links/mylink1"
    Then I receive HTTP 200
    And the response contains "code", "original_url", "click_count", "is_active", "created_at"

  Scenario: View another user's link details
    Given I am authenticated as user "alice"
    And user "bob" owns a link with code "boblink"
    When I GET "/api/v1/links/boblink"
    Then I receive HTTP 403

  Scenario: Update link title
    Given I am authenticated as a free user
    And I own a link with code "mylink1"
    When I PATCH "/api/v1/links/mylink1" with body:
      | title | Updated Title |
    Then I receive HTTP 200
    And the link "title" equals "Updated Title"

  Scenario: Deactivate a link
    Given I am authenticated as a free user
    And I own an active link with code "mylink1"
    When I PATCH "/api/v1/links/mylink1" with body:
      | is_active | false |
    Then I receive HTTP 200
    And the link "is_active" equals false
    When a visitor requests GET /mylink1
    Then the visitor receives HTTP 410

  Scenario: Delete a link
    Given I am authenticated as a free user
    And I own a link with code "mylink1"
    When I DELETE "/api/v1/links/mylink1"
    Then I receive HTTP 204
    When a visitor requests GET /mylink1
    Then the visitor receives HTTP 404

  Scenario: Delete another user's link
    Given I am authenticated as user "alice"
    And user "bob" owns a link with code "boblink"
    When I DELETE "/api/v1/links/boblink"
    Then I receive HTTP 403

  Scenario: View quota usage
    Given I am authenticated as a free user with plan limits daily=50, total=500
    And I have created 15 links today
    And I have 200 total active links
    When I GET "/api/v1/me"
    Then I receive HTTP 200
    And the response contains:
      | daily_used  | 15  |
      | daily_limit | 50  |
      | total_used  | 200 |
      | total_limit | 500 |
      | plan        | free |
```

---

## Feature: Statistics

```gherkin
Feature: Link statistics
  As an authenticated user
  I want to view statistics for my links
  So that I can measure link performance and audience

  Scenario: View aggregate statistics
    Given I am authenticated as a free user
    And I own a link with code "stats1" that has received 150 total clicks from 80 unique visitors
    When I GET "/api/v1/links/stats1/stats"
    Then I receive HTTP 200
    And the response contains "total_clicks" equals 150
    And the response contains "unique_clicks" equals 80

  Scenario: Free user stats limited to 30 days
    Given I am authenticated as a free user
    And I own a link with code "stats2" with clicks spanning 60 days
    When I GET "/api/v1/links/stats2/stats"
    Then I receive HTTP 200
    And the response only contains data from the last 30 days

  Scenario: View statistics for another user's link
    Given I am authenticated as user "alice"
    And user "bob" owns a link with code "bobstats"
    When I GET "/api/v1/links/bobstats/stats"
    Then I receive HTTP 403

  Scenario: Anonymous user cannot access stats
    Given I am not authenticated
    When I GET "/api/v1/links/anycode/stats"
    Then I receive HTTP 401

  Scenario: View click timeline (daily)
    Given I am authenticated as a free user
    And I own a link with code "tl1" with clicks on 2024-01-01, 2024-01-02, and 2024-01-03
    When I GET "/api/v1/links/tl1/stats/timeline?period=day"
    Then I receive HTTP 200
    And the response contains an array of date-count pairs
    And each entry has "date" and "count" fields

  Scenario: View click timeline (hourly)
    Given I am authenticated as a free user
    And I own a link with code "tl2"
    When I GET "/api/v1/links/tl2/stats/timeline?period=hour"
    Then I receive HTTP 200
    And the response contains hourly click counts

  Scenario: View geographic breakdown
    Given I am authenticated as a free user
    And I own a link with code "geo1" with clicks from "US" (50), "DE" (30), "JP" (20)
    When I GET "/api/v1/links/geo1/stats/geo"
    Then I receive HTTP 200
    And the response contains countries sorted by click count descending
    And the first entry is "US" with count 50

  Scenario: View referrer sources
    Given I am authenticated as a free user
    And I own a link with code "ref1" with clicks from "twitter.com" (40), "reddit.com" (25), direct (15)
    When I GET "/api/v1/links/ref1/stats/referrers"
    Then I receive HTTP 200
    And the response contains referrer domains sorted by click count descending
    And direct traffic is represented as "direct"
```

---

## Feature: Authentication

```gherkin
Feature: Authentication
  As a visitor
  I want to register and log in
  So that I can access authenticated features

  Scenario: Register with email and password
    Given I am not registered
    When I register with email "user@example.com" and password "Str0ngP@ss!"
    Then I receive HTTP 201
    And I receive Access and Refresh JWT tokens in httpOnly cookies

  Scenario: Register with weak password
    Given I am not registered
    When I register with email "user@example.com" and password "123"
    Then I receive HTTP 400
    And the response contains error about password requirements

  Scenario: Register with duplicate email
    Given a user already exists with email "existing@example.com"
    When I register with email "existing@example.com" and password "Str0ngP@ss!"
    Then I receive HTTP 409

  Scenario: Login with valid credentials
    Given I am registered with email "user@example.com" and password "Str0ngP@ss!"
    When I login with email "user@example.com" and password "Str0ngP@ss!"
    Then I receive HTTP 200
    And I receive Access and Refresh JWT tokens in httpOnly cookies

  Scenario: Login with invalid credentials
    Given I am registered with email "user@example.com"
    When I login with email "user@example.com" and password "wrong-password"
    Then I receive HTTP 401

  Scenario: Login with Google SSO
    Given I have a valid Google account
    When I initiate the Google OAuth flow
    And Google authenticates me successfully
    Then I am redirected to the callback URL
    And I receive Access and Refresh JWT tokens in httpOnly cookies
    And my account is created if it does not exist

  Scenario: Refresh expired access token
    Given I have a valid refresh token
    And my access token has expired
    When I request a token refresh
    Then I receive HTTP 200
    And I receive a new Access token

  Scenario: Refresh with invalid refresh token
    Given I have an invalid or expired refresh token
    When I request a token refresh
    Then I receive HTTP 401

  Scenario: Access protected endpoint without authentication
    Given I am not authenticated
    When I GET "/api/v1/links"
    Then I receive HTTP 401

  Scenario: Access protected endpoint with expired token
    Given I have an expired access token
    When I GET "/api/v1/links"
    Then I receive HTTP 401
```

---

## Feature: Click Event Processing

```gherkin
Feature: Click event processing
  As the system
  I want to process click events asynchronously
  So that redirect performance is not degraded by analytics writes

  Scenario: Click event is published to SQS on redirect
    Given a short link "evt1" exists and is active
    When a visitor requests GET /evt1
    Then the visitor is redirected with HTTP 302
    And a click event message is published to the SQS FIFO queue
    And the message contains: link code, hashed IP, User-Agent, Referer, timestamp

  Scenario: Click event does not block redirect
    Given a short link "evt2" exists and is active
    And the SQS queue is temporarily unavailable
    When a visitor requests GET /evt2
    Then the visitor is still redirected with HTTP 302
    And the click event failure is logged but does not affect the response

  Scenario: Worker processes click events in batch
    Given 10 click events are in the SQS FIFO queue
    When the worker Lambda is triggered
    Then all 10 events are written to the "clicks" DynamoDB table
    And each click record has a 90-day TTL

  Scenario: IP address is hashed before storage
    Given a visitor with IP "192.168.1.1" clicks a link
    When the click event is processed
    Then the stored "ip_hash" is SHA-256("192.168.1.1" + secret_salt)
    And the raw IP address is not stored anywhere
```

---

## Feature: URL Validation

```gherkin
Feature: URL validation
  As the system
  I want to validate submitted URLs
  So that malicious and invalid links are blocked

  Scenario: Valid HTTP URL
    Given I submit original_url "http://example.com"
    When the URL is validated
    Then validation passes

  Scenario: Valid HTTPS URL
    Given I submit original_url "https://example.com/path?q=1"
    When the URL is validated
    Then validation passes

  Scenario: Invalid protocol
    Given I submit original_url "ftp://example.com"
    When the URL is validated
    Then I receive HTTP 400
    And the response contains error "only HTTP and HTTPS URLs are allowed"

  Scenario: No protocol
    Given I submit original_url "example.com"
    When the URL is validated
    Then I receive HTTP 400

  Scenario: URL flagged by Safe Browsing
    Given I submit original_url "https://malicious-site.example.com"
    And the URL is flagged by Google Safe Browsing API
    When the URL is validated
    Then I receive HTTP 400
    And the response contains error "URL is blocked for safety reasons"

  Scenario: URL exceeds maximum length
    Given I submit original_url with more than 2048 characters
    When the URL is validated
    Then I receive HTTP 400
    And the response contains error "URL exceeds maximum length"
```
