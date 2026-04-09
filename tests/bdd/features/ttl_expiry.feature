@ttl
Feature: Link TTL and expiry
  As a link creator
  I want links to expire based on time or click count
  So that links don't persist indefinitely

  Background:
    Given the service is running

  @happy-path
  Scenario: Time-based TTL link is active before expiry
    Given I am authenticated as a free user
    And I created a short link "ttl1" with expires_at set to 1 hour from now pointing to "https://example.com"
    When a visitor requests GET "/ttl1"
    Then the response status is 302
    And the Location header is "https://example.com"

  @happy-path @error-path
  Scenario: Time-based TTL link returns 410 after expiry
    Given a short link "ttlexp" exists with expires_at in the past pointing to "https://example.com"
    When a visitor requests GET "/ttlexp"
    Then the response status is 410
    And the response body contains "expired"

  @happy-path
  Scenario: Click-based TTL link is active until max_clicks reached
    Given a short link "clk3" exists with max_clicks 3 and click_count 2 pointing to "https://example.com"
    When a visitor requests GET "/clk3"
    Then the response status is 302
    And the Location header is "https://example.com"
    And the click_count for "clk3" is incremented to 3

  @error-path
  Scenario: Click-based TTL link returns 410 on click N+1
    Given a short link "clkx" exists with max_clicks 5 and click_count 5
    When a visitor requests GET "/clkx"
    Then the response status is 410
    And the response body contains "expired"

  @error-path
  Scenario: Combined TTL where time expires first
    Given a short link "both1" exists with expires_at in the past and max_clicks 100 and click_count 0
    When a visitor requests GET "/both1"
    Then the response status is 410

  @error-path
  Scenario: Combined TTL where clicks exhaust first
    Given a short link "both2" exists with expires_at 1 hour from now and max_clicks 1 and click_count 1
    When a visitor requests GET "/both2"
    Then the response status is 410

  @security
  Scenario: Anonymous links enforce max 24-hour TTL
    Given I am not authenticated
    When I POST "/api/v1/shorten" with body:
      | original_url | https://example.com |
    Then the response status is 201
    And the response "expires_at" is no more than 24 hours from now

  @happy-path
  Scenario: Authenticated link with no TTL stays active indefinitely
    Given I am authenticated as a free user
    And I created a short link "notll" without expires_at or max_clicks pointing to "https://example.com"
    When a visitor requests GET "/notll"
    Then the response status is 302
    And the Location header is "https://example.com"

  Scenario: DynamoDB TTL attribute auto-deletes expired items
    Given a short link "dynttl" exists with expires_at set to 2 minutes ago
    When DynamoDB TTL cleanup has run
    Then the link "dynttl" no longer exists in the links table

  @error-path
  Scenario: Expired link shows expired message not 404
    Given a short link "gone1" exists with expires_at in the past
    When a visitor requests GET "/gone1"
    Then the response status is 410
    And the response body contains "expired"
    And the response status is not 404

  @happy-path
  Scenario: Click count is exact with atomic increment
    Given a short link "atomic1" exists with max_clicks 1 and click_count 0 pointing to "https://example.com"
    When 2 visitors request GET "/atomic1" simultaneously
    Then exactly 1 visitor receives status 302
    And exactly 1 visitor receives status 410
    And the click_count for "atomic1" equals 1

  @happy-path
  Scenario Outline: Click-based TTL boundary conditions
    Given a short link "clkbound" exists with max_clicks <max> and click_count <current> pointing to "https://example.com"
    When a visitor requests GET "/clkbound"
    Then the response status is <status>

    Examples:
      | max | current | status |
      | 1   | 0       | 302    |
      | 5   | 4       | 302    |
      | 5   | 5       | 410    |
      | 10  | 10      | 410    |
      | 10  | 9       | 302    |
