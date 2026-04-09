@create
Feature: Link creation
  As a user
  I want to create short links
  So that I can share memorable URLs

  Background:
    Given the API service is running on "http://localhost:8080"

  # --- Anonymous (Guest) Creation ---

  @happy-path
  Scenario: Anonymous creation via POST /api/v1/shorten
    Given I am not authenticated
    When I POST "/api/v1/shorten" with JSON body:
      """
      {"original_url": "https://example.com/very/long/path"}
      """
    Then the response status is 201
    And the response body contains "code" with a 7-8 character alphanumeric value
    And the response body contains "short_url"
    And the response body contains "original_url" with value "https://example.com/very/long/path"
    And the response body contains "expires_at" set to approximately 24 hours from now

  @happy-path
  Scenario: Anonymous link enforces 24-hour maximum TTL
    Given I am not authenticated
    When I POST "/api/v1/shorten" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 201
    And the "expires_at" field is no more than 24 hours from now

  # --- Authenticated Creation ---

  @happy-path
  Scenario: Authenticated user creates a basic link
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 201
    And the response body contains "code" with a 7-8 character alphanumeric value
    And the response body contains "original_url" with value "https://example.com"
    And the response body contains "owner_id" matching my user ID
    And the response body contains "created_at"
    And the response body contains "is_active" with value true

  @happy-path
  Scenario: Create link with custom alias
    Given I am authenticated as a free plan user
    And no short link exists with code "my-brand"
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com", "custom_alias": "my-brand"}
      """
    Then the response status is 201
    And the response body contains "code" with value "my-brand"

  @happy-path
  Scenario: Create link with time-based TTL (expires_at)
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com", "expires_at": 1735689600}
      """
    Then the response status is 201
    And the response body contains "expires_at" with value 1735689600

  @happy-path
  Scenario: Create link with click-based TTL (max_clicks)
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com", "max_clicks": 100}
      """
    Then the response status is 201
    And the response body contains "max_clicks" with value 100
    And the response body contains "click_count" with value 0

  @happy-path
  Scenario: Create link with password protection
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com", "password": "s3cret"}
      """
    Then the response status is 201
    And the response body contains "has_password" with value true
    And the response body does not contain "password"
    And the response body does not contain "password_hash"

  @happy-path
  Scenario: Create link with UTM parameters
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {
        "original_url": "https://example.com/landing",
        "utm_source": "twitter",
        "utm_medium": "social",
        "utm_campaign": "spring2026"
      }
      """
    Then the response status is 201
    And the response body contains "utm_source" with value "twitter"
    And the response body contains "utm_medium" with value "social"
    And the response body contains "utm_campaign" with value "spring2026"

  @happy-path
  Scenario: Created link appears in Redis cache immediately
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com/cached"}
      """
    Then the response status is 201
    And the created link code is cached in Redis

  # --- Error Path ---

  @error-path
  Scenario: Duplicate custom alias returns 409 Conflict
    Given I am authenticated as a free plan user
    And a short link with code "taken" already exists
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com", "custom_alias": "taken"}
      """
    Then the response status is 409
    And the response body contains "code" with value "conflict"
    And the response body contains "message" with value "alias already in use"

  @error-path
  Scenario Outline: Invalid URL rejected with 400
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "<url>"}
      """
    Then the response status is 400
    And the response body contains "message" with value "<error_message>"

    Examples:
      | url              | error_message                                     |
      |                  | original_url is required                          |
      | not-a-valid-url  | original_url must be a valid HTTP or HTTPS URL    |
      | ftp://ftp.example.com | only HTTP and HTTPS URLs are allowed          |

  @error-path @security
  Scenario: URL exceeding 2048 characters rejected
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with an original_url longer than 2048 characters
    Then the response status is 400
    And the response body contains "message" with value "original_url exceeds maximum length of 2048 characters"

  @error-path @security
  Scenario: Malicious javascript: URL rejected
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "javascript:alert('xss')"}
      """
    Then the response status is 400
    And the response body contains "message" with value "only HTTP and HTTPS URLs are allowed"

  @error-path @security
  Scenario: Private IP URL (SSRF) rejected
    Given I am authenticated as a free plan user
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "http://169.254.169.254/latest/meta-data"}
      """
    Then the response status is 400
    And the response body contains "message" matching "blocked|private|not allowed"

  @error-path
  Scenario: Unauthenticated request to authenticated endpoint returns 401
    Given I am not authenticated
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 401

  # --- Quota / Rate Limiting ---

  @ratelimit
  Scenario: Guest creation respects strict quota (5/hour)
    Given I am not authenticated
    And the requesting IP has already created 5 links in the current hour
    When I POST "/api/v1/shorten" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 429
    And the "Retry-After" header is present
    And the "X-RateLimit-Remaining" header is "0"

  @ratelimit
  Scenario: Authenticated free user daily quota exceeded (50/day)
    Given I am authenticated as a free plan user
    And I have already created 50 links today
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 429
    And the response body contains "message" with value "daily link limit reached"

  @ratelimit
  Scenario: Authenticated free user total quota exceeded (500 active)
    Given I am authenticated as a free plan user
    And I have 500 total active links
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 429
    And the response body contains "message" with value "total link limit reached"
