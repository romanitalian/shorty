@password
Feature: Password-protected links
  As a link creator
  I want to password-protect my short links
  So that only authorized users can access the destination

  Background:
    Given the service is running

  @happy-path
  Scenario: Password-protected link shows password form
    Given a short link "secret1" exists with password "s3cret"
    When I request GET "/secret1"
    Then the response status is 200
    And the response content type is "text/html"
    And the response body contains a password input field
    And the response body contains a form with action "/p/secret1"
    And the response body contains a hidden CSRF token field

  @happy-path
  Scenario: Correct password redirects to destination
    Given a short link "secret1" exists with password "s3cret" pointing to "https://example.com"
    And I have a valid CSRF token for "secret1"
    When I POST "/p/secret1" with password "s3cret" and the CSRF token
    Then the response status is 302
    And the Location header is "https://example.com"

  @error-path
  Scenario: Wrong password returns 401 with retry form
    Given a short link "secret1" exists with password "s3cret"
    And I have a valid CSRF token for "secret1"
    When I POST "/p/secret1" with password "wrong-password" and the CSRF token
    Then the response status is 401
    And the response content type is "text/html"
    And the response body contains an error message "incorrect password"
    And the response body contains a password input field

  @security
  Scenario: CSRF token required on form submission
    Given a short link "secret1" exists with password "s3cret"
    And I have a valid CSRF token for "secret1"
    When I POST "/p/secret1" with password "s3cret" and the CSRF token
    Then the response status is 302

  @security @error-path
  Scenario: Missing CSRF token returns 403
    Given a short link "secret1" exists with password "s3cret"
    When I POST "/p/secret1" with password "s3cret" and no CSRF token
    Then the response status is 403

  @security @error-path
  Scenario: Invalid CSRF token returns 403
    Given a short link "secret1" exists with password "s3cret"
    When I POST "/p/secret1" with password "s3cret" and CSRF token "invalid-token-value"
    Then the response status is 403

  @security @error-path
  Scenario: Brute force lockout after 5 wrong attempts
    Given a short link "secret1" exists with password "s3cret"
    And I have submitted 5 incorrect passwords for "secret1" in the last 15 minutes from my IP
    When I POST "/p/secret1" with any password and a valid CSRF token
    Then the response status is 429
    And the response contains a "Retry-After" header

  @security
  Scenario: Lockout expires after 15 minutes
    Given a short link "secret1" exists with password "s3cret"
    And I was locked out of "secret1" due to brute force 16 minutes ago
    And I have a valid CSRF token for "secret1"
    When I POST "/p/secret1" with password "s3cret" and the CSRF token
    Then the response status is 302

  @security
  Scenario: Password stored as bcrypt hash not plain text
    Given I am authenticated as a free user
    When I POST "/api/v1/links" with body:
      | original_url | https://example.com |
      | password     | s3cret              |
    Then the response status is 201
    And the password is not returned in the response body
    And the stored password_hash is a valid bcrypt hash
    And the stored password_hash is not "s3cret"

  @happy-path
  Scenario: Link without password redirects directly
    Given a short link "nopass1" exists pointing to "https://example.com" without a password
    When I request GET "/nopass1"
    Then the response status is 302
    And the Location header is "https://example.com"

  @security @error-path
  Scenario Outline: Brute force attempt counting
    Given a short link "secret1" exists with password "s3cret"
    And I have submitted <attempts> incorrect passwords for "secret1" in the last 15 minutes from my IP
    And I have a valid CSRF token for "secret1"
    When I POST "/p/secret1" with password "wrong" and the CSRF token
    Then the response status is <status>

    Examples:
      | attempts | status |
      | 0        | 401    |
      | 3        | 401    |
      | 4        | 401    |
      | 5        | 429    |
      | 10       | 429    |
