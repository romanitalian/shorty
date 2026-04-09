@redirect
Feature: URL redirect
  As a visitor
  I want short links to redirect me to the original URL
  So that I can reach the destination quickly

  Background:
    Given the redirect service is running on "http://localhost:8081"
    And the API service is running on "http://localhost:8080"

  # --- Happy Path ---

  @happy-path
  Scenario: Redirect to active link returns 302
    Given a short link "abc123" pointing to "https://example.com" is active
    When I request GET "/abc123"
    Then the response status is 302
    And the "Location" header is "https://example.com"
    And the "Cache-Control" header is "private, no-cache"

  @happy-path
  Scenario: Redirect with 301 for permanently redirected link
    Given a short link "perm01" pointing to "https://example.com" is active
    And the link "perm01" is configured for permanent redirect
    When I request GET "/perm01"
    Then the response status is 301
    And the "Location" header is "https://example.com"
    And the "Cache-Control" header contains "max-age="

  @happy-path
  Scenario: Click event is recorded asynchronously after redirect
    Given a short link "evt001" pointing to "https://example.com" is active
    When I request GET "/evt001" with headers:
      | User-Agent | Mozilla/5.0 TestBrowser  |
      | Referer    | https://twitter.com/post |
    Then the response status is 302
    And within 5 seconds a click event is recorded for "evt001"
    And the click event contains a hashed IP, User-Agent, Referer, and timestamp

  @happy-path
  Scenario: Cache hit path - Redis serves redirect without DynamoDB
    Given a short link "cache1" pointing to "https://cached.example.com" exists in Redis cache
    When I request GET "/cache1"
    Then the response status is 302
    And the "Location" header is "https://cached.example.com"
    And DynamoDB was not queried for "cache1"

  @happy-path
  Scenario: Cache miss path - DynamoDB fallback warms Redis cache
    Given a short link "miss01" pointing to "https://dbonly.example.com" exists in DynamoDB but not in Redis
    When I request GET "/miss01"
    Then the response status is 302
    And the "Location" header is "https://dbonly.example.com"
    And the link "miss01" is now cached in Redis

  @happy-path
  Scenario: Redirect preserves query parameters including UTM
    Given a short link "utm001" pointing to "https://example.com/landing?ref=base" is active
    When I request GET "/utm001?utm_source=twitter&utm_medium=social&utm_campaign=launch"
    Then the response status is 302
    And the "Location" header contains "utm_source=twitter"
    And the "Location" header contains "utm_medium=social"
    And the "Location" header contains "utm_campaign=launch"

  # --- Error Path ---

  @error-path
  Scenario: Non-existent short code returns 404
    Given no short link exists with code "notfound99"
    When I request GET "/notfound99"
    Then the response status is 404
    And the response body contains "code" with value "not_found"
    And the response body contains "message"

  @error-path
  Scenario: Time-expired link returns 410 Gone
    Given a short link "exp001" pointing to "https://example.com" is active
    And the link "exp001" has expires_at set to 1 hour in the past
    When I request GET "/exp001"
    Then the response status is 410
    And the response body contains "code" with value "gone"

  @error-path
  Scenario: Click-limited link deactivates after max_clicks reached
    Given a short link "clk001" pointing to "https://example.com" is active
    And the link "clk001" has max_clicks set to 1
    And the link "clk001" has click_count equal to 1
    When I request GET "/clk001"
    Then the response status is 410
    And the response body contains "code" with value "gone"

  @error-path
  Scenario: Deactivated link returns 410 Gone
    Given a short link "deact1" pointing to "https://example.com" exists
    And the link "deact1" has is_active set to false
    When I request GET "/deact1"
    Then the response status is 410

  @error-path
  Scenario Outline: Various expired/exhausted states return 410
    Given a short link "<code>" pointing to "https://example.com" exists with:
      | expires_at  | <expires_at>  |
      | max_clicks  | <max_clicks>  |
      | click_count | <click_count> |
      | is_active   | <is_active>   |
    When I request GET "/<code>"
    Then the response status is <status>

    Examples:
      | code    | expires_at      | max_clicks | click_count | is_active | status |
      | ttl410  | 1_hour_ago      |            | 0           | true      | 410    |
      | clk410  |                 | 5          | 5           | true      | 410    |
      | off410  |                 |            | 0           | false     | 410    |
      | ok302   |                 |            | 0           | true      | 302    |
      | both410 | 1_hour_ago      | 100        | 50          | true      | 410    |

  # --- Edge Cases ---

  @edge-case
  Scenario: Click-limited link allows last click then deactivates
    Given a short link "last01" pointing to "https://example.com" is active
    And the link "last01" has max_clicks set to 1
    And the link "last01" has click_count equal to 0
    When I request GET "/last01"
    Then the response status is 302
    And the "Location" header is "https://example.com"
    And the click_count for "last01" is now 1
    When I request GET "/last01"
    Then the response status is 410

  @edge-case
  Scenario: Very long original URL redirects correctly
    Given a short link "long01" pointing to a URL of 2048 characters is active
    When I request GET "/long01"
    Then the response status is 302
    And the "Location" header matches the full 2048-character URL

  @edge-case
  Scenario: Redirect does not block when SQS is unavailable
    Given a short link "sqsfail" pointing to "https://example.com" is active
    And the SQS queue is temporarily unavailable
    When I request GET "/sqsfail"
    Then the response status is 302
    And the "Location" header is "https://example.com"

  @edge-case @security
  Scenario: Rate-limited redirect returns 429
    Given the requesting IP has made 200 redirect requests in the last 60 seconds
    When I request GET "/anycode"
    Then the response status is 429
    And the "Retry-After" header is present
