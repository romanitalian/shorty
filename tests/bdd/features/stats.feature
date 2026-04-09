@stats
Feature: Link statistics
  As a link owner
  I want to view click analytics for my links
  So that I can understand my audience

  Background:
    Given the service is running
    And I am authenticated as a free user

  @happy-path
  Scenario: Aggregate stats show total and unique clicks
    Given I own a link with code "stats1" that has received 150 total clicks from 80 unique visitors
    When I GET "/api/v1/links/stats1/stats"
    Then the response status is 200
    And the response contains "total_clicks" equals 150
    And the response contains "unique_clicks" equals 80

  @happy-path
  Scenario: Timeline shows clicks per day
    Given I own a link with code "tl1" with clicks on "2024-01-01", "2024-01-02", and "2024-01-03"
    When I GET "/api/v1/links/tl1/stats/timeline?period=day"
    Then the response status is 200
    And the response contains an array of date-count pairs
    And each entry has "date" and "count" fields

  @happy-path
  Scenario Outline: Timeline supports multiple periods
    Given I own a link with code "tlperiod" that has received clicks
    When I GET "/api/v1/links/tlperiod/stats/timeline?period=<period>"
    Then the response status is 200
    And the response contains an array of time-count pairs

    Examples:
      | period |
      | hour   |
      | day    |
      | week   |
      | month  |

  @happy-path
  Scenario: Geography breakdown shows top countries
    Given I own a link with code "geo1" with clicks from:
      | country | count |
      | US      | 50    |
      | DE      | 30    |
      | JP      | 20    |
    When I GET "/api/v1/links/geo1/stats/geo"
    Then the response status is 200
    And the response contains countries sorted by click count descending
    And the first entry is country "US" with count 50

  @happy-path
  Scenario: Referrer sources show top domains
    Given I own a link with code "ref1" with clicks from referrers:
      | referrer    | count |
      | twitter.com | 40    |
      | reddit.com  | 25    |
      | direct      | 15    |
    When I GET "/api/v1/links/ref1/stats/referrers"
    Then the response status is 200
    And the response contains referrer domains sorted by click count descending
    And direct traffic is represented as "direct"

  @happy-path
  Scenario: Device type breakdown
    Given I own a link with code "dev1" with clicks from devices:
      | device_type | count |
      | desktop     | 60    |
      | mobile      | 30    |
      | bot         | 10    |
    When I GET "/api/v1/links/dev1/stats"
    Then the response status is 200
    And the response contains "device_types" with entries for "desktop", "mobile", and "bot"

  @security @error-path
  Scenario: Stats only visible to link owner
    Given user "bob" owns a link with code "bobstats"
    When I GET "/api/v1/links/bobstats/stats"
    Then the response status is 403

  @security @error-path
  Scenario: Anonymous user cannot access stats
    Given I am not authenticated
    When I GET "/api/v1/links/anycode/stats"
    Then the response status is 401

  @happy-path
  Scenario: Stats for link with zero clicks returns empty data
    Given I own a link with code "empty1" that has received 0 clicks
    When I GET "/api/v1/links/empty1/stats"
    Then the response status is 200
    And the response contains "total_clicks" equals 0
    And the response contains "unique_clicks" equals 0

  @happy-path
  Scenario: Stats reflect click within seconds
    Given I own a link with code "fresh1" pointing to "https://example.com" with 0 clicks
    When a visitor requests GET "/fresh1"
    And I wait up to 5 seconds
    And I GET "/api/v1/links/fresh1/stats"
    Then the response status is 200
    And the response contains "total_clicks" equals 1

  Scenario: Free tier stats limited to 30 days history
    Given I own a link with code "hist1" with clicks spanning 60 days
    When I GET "/api/v1/links/hist1/stats"
    Then the response status is 200
    And the response only contains data from the last 30 days

  @security
  Scenario: Click event records IP hash not raw IP
    Given I own a link with code "iph1" pointing to "https://example.com"
    When a visitor with IP "192.168.1.1" requests GET "/iph1"
    And the click event is processed
    Then the stored click record contains "ip_hash" as SHA-256 of IP with salt
    And the stored click record does not contain "192.168.1.1"

  @happy-path
  Scenario: Stats endpoint pagination for large datasets
    Given I own a link with code "big1" with clicks from 50 countries
    When I GET "/api/v1/links/big1/stats/geo?limit=10"
    Then the response status is 200
    And the response contains exactly 10 country entries
    And the response contains a "next_cursor" for pagination
