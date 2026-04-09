@ratelimit
Feature: Rate limiting
  As the system
  I want to enforce rate limits per tier
  So that the service remains available and fair

  Background:
    Given the redirect service is running on "http://localhost:8081"
    And the API service is running on "http://localhost:8080"

  # --- Anonymous Redirect Rate Limits ---

  @happy-path
  Scenario: Anonymous redirect within limit succeeds
    Given I am not authenticated
    And a short link "rl001" pointing to "https://example.com" is active
    And the requesting IP has made 100 redirect requests in the last 60 seconds
    When I request GET "/rl001"
    Then the response status is 302
    And the "X-RateLimit-Limit" header is present
    And the "X-RateLimit-Remaining" header is present
    And the "X-RateLimit-Reset" header is present

  @error-path
  Scenario: Anonymous redirect exceeds 200 req/min returns 429
    Given I am not authenticated
    And the requesting IP has made 200 redirect requests in the last 60 seconds
    When I request GET "/anycode"
    Then the response status is 429
    And the "Retry-After" header is present
    And the "X-RateLimit-Remaining" header is "0"
    And the response body contains "code" with value "rate_limit_exceeded"

  # --- Anonymous Creation Rate Limits ---

  @error-path
  Scenario: Anonymous creation exceeds 5 links/hour returns 429
    Given I am not authenticated
    And the requesting IP has created 5 links in the current hour
    When I POST "/api/v1/shorten" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 429
    And the "Retry-After" header is present

  @happy-path
  Scenario: Anonymous creation within limit succeeds
    Given I am not authenticated
    And the requesting IP has created 2 links in the current hour
    When I POST "/api/v1/shorten" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 201

  # --- Rate Limit Headers ---

  @happy-path
  Scenario: Rate limit headers present on successful redirect
    Given a short link "hdr001" pointing to "https://example.com" is active
    When I request GET "/hdr001"
    Then the response status is 302
    And the "X-RateLimit-Limit" header is "200"
    And the "X-RateLimit-Remaining" header is present
    And the "X-RateLimit-Reset" header contains a Unix timestamp

  @error-path
  Scenario: 429 response includes Retry-After header with seconds
    Given the requesting IP has made 200 redirect requests in the last 60 seconds
    When I request GET "/anycode"
    Then the response status is 429
    And the "Retry-After" header contains a positive integer
    And the "X-RateLimit-Limit" header is "200"
    And the "X-RateLimit-Remaining" header is "0"

  # --- IP Isolation ---

  @happy-path
  Scenario: Different IPs have independent rate limit counters
    Given a short link "iso001" pointing to "https://example.com" is active
    And IP "10.0.0.1" has made 200 redirect requests in the last 60 seconds
    And IP "10.0.0.2" has made 0 redirect requests in the last 60 seconds
    When IP "10.0.0.1" requests GET "/iso001"
    Then the response status is 429
    When IP "10.0.0.2" requests GET "/iso001"
    Then the response status is 302

  # --- Authenticated User Limits ---

  @happy-path
  Scenario: Authenticated free user has higher creation limit (50/day)
    Given I am authenticated as a free plan user
    And I have already created 10 links today
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 201

  @error-path
  Scenario: Authenticated free user exceeds daily creation limit
    Given I am authenticated as a free plan user
    And I have already created 50 links today
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 429
    And the response body contains "message" with value "daily link limit reached"

  # --- Sliding Window Behavior ---

  @edge-case
  Scenario: Rate limit resets after sliding window expires
    Given I am not authenticated
    And the requesting IP made 200 redirect requests between 12:00:00 and 12:00:30
    And the current time is 12:01:31
    When I request GET "/reset01"
    Then the response status is not 429
    # Because the oldest requests have fallen out of the 1-minute sliding window

  @edge-case
  Scenario: Sliding window partially expires allowing some requests
    Given I am not authenticated
    And the requesting IP made 150 redirect requests at 12:00:00
    And the requesting IP made 50 redirect requests at 12:00:45
    And the current time is 12:01:01
    When I request GET "/partial01"
    Then the response status is not 429
    # Because the 150 requests from 12:00:00 have expired from the window

  # --- WAF-Level Rate Limiting ---

  @security @edge-case
  Scenario: WAF-level rate limit blocks before Lambda
    Given a single IP sends more than 1000 requests per minute
    Then the WAF returns HTTP 403 before the request reaches the Lambda function
    And the Lambda invocation count does not increase

  # --- Quota Display ---

  @happy-path
  Scenario: Rate limit headers reflect remaining quota accurately
    Given I am authenticated as a free plan user
    And I have already created 49 links today
    When I POST "/api/v1/links" with JSON body:
      """
      {"original_url": "https://example.com"}
      """
    Then the response status is 201
    And the "X-RateLimit-Remaining" header is "0"
    And the "X-RateLimit-Limit" header is "50"
