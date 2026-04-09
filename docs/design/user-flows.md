# Shorty -- User Flows

Step-by-step user journeys covering happy paths, error states, and edge cases.

---

## Flow 1: Anonymous User Shortens a URL

**Entry point:** Landing page (`/`)
**API:** `POST /api/v1/shorten`

### Happy Path

1. User lands on homepage. URL input is auto-focused.
2. User pastes a long URL into the input field.
3. User clicks [Shorten] (or presses Enter).
4. Button shows spinner; input is disabled.
5. System validates URL format (client-side).
6. System sends `POST /api/v1/shorten` with `{ "url": "..." }`.
7. API validates URL server-side, checks rate limit (5/hour per IP), generates Base62 code.
8. API returns `201 Created` with short URL.
9. Result card appears below input: short URL in monospace, [Copy] and [Open] buttons.
10. Guest quota notice updates: "4 links remaining today."
11. User clicks [Copy]. Short URL is copied to clipboard. Button text changes to "Copied!" for 2 seconds.

### Error: Invalid URL

1. Steps 1-5 as above.
2. Client-side validation detects invalid URL (no scheme, invalid format).
3. Input border turns red; error message appears below: "Please enter a valid HTTP or HTTPS URL."
4. User corrects the URL and resubmits.
5. Continues from step 6 of happy path.

### Error: URL Blocked (Safe Browsing)

1. Steps 1-6 as above.
2. API checks URL against Safe Browsing API; URL is flagged.
3. API returns `400 Bad Request` with error: "This URL has been flagged as unsafe and cannot be shortened."
4. Error message displayed inline below input with warning icon.
5. User must enter a different URL.

### Error: Rate Limit Exceeded

1. Steps 1-6 as above.
2. API returns `429 Too Many Requests` with `Retry-After: 3600` header.
3. Toast notification: "Daily limit reached. Sign in for 50 links per day."
4. [Shorten] button remains enabled but the error persists until the rate window resets.
5. A dismissible banner appears below the input: "Create a free account for higher limits. [Sign up]"

### Error: Server Error

1. Steps 1-6 as above.
2. API returns `500 Internal Server Error`.
3. Toast notification: "Something went wrong. Please try again."
4. Input retains the URL value. [Shorten] button re-enables.
5. User can retry immediately.

---

## Flow 2: User Registers via Google SSO

**Entry point:** [Sign in] button on any page, or "Sign up" link
**API:** AWS Cognito OAuth2 flow

### Happy Path

1. User clicks [Sign in] on the navigation bar.
2. System navigates to `/login`.
3. User clicks [Continue with Google].
4. Browser redirects to Google OAuth consent screen (Cognito hosted UI).
5. User selects their Google account and grants consent.
6. Google redirects back to Shorty callback URL with authorization code.
7. Cognito exchanges code for tokens (Access + Refresh JWTs).
8. System stores tokens in httpOnly cookies.
9. **Decision: Is this a new user?**
   - **Yes:** Cognito auto-creates user record. System redirects to dashboard with welcome toast: "Welcome to Shorty! You have 50 links per day."
   - **No:** System redirects to dashboard (or the page the user was on before login).
10. Navigation bar updates: shows user email and dropdown menu.

### Error: User Cancels Google Consent

1. Steps 1-5 as above.
2. User clicks "Cancel" on Google consent screen.
3. Google redirects back to Shorty with error parameter.
4. System shows login page with info message: "Google sign-in was cancelled. You can try again or use email and password."

### Error: Cognito Service Unavailable

1. Steps 1-5 as above.
2. Cognito callback fails (network error, service outage).
3. System shows error page: "Authentication service is temporarily unavailable. Please try again in a few minutes."
4. [Retry] button and [Back to home] link.

---

## Flow 3: Authenticated User Creates a Link with Custom Alias + TTL

**Entry point:** Dashboard [+ New Link] button
**API:** `POST /api/v1/links`

### Happy Path

1. User clicks [+ New Link] on dashboard.
2. Create-link modal opens with focus on URL input.
3. User enters the original URL.
4. User enters a custom alias: "my-promo" (3-30 alphanumeric chars).
5. User selects expiration: "7 days" from dropdown.
6. Optionally sets max clicks, password, title.
7. User clicks [Create Link].
8. Modal shows spinner.
9. System sends `POST /api/v1/links` with JWT in Authorization header.
10. API validates: URL format, alias availability, alias format, user quota.
11. API returns `201 Created` with full link object.
12. Modal transforms to success view: short URL (`shorty.io/my-promo`) with [Copy] button.
13. Dashboard table refreshes; new link appears at top.
14. Quota bar updates.

### Error: Alias Already Taken

1. Steps 1-9 as above.
2. API returns `409 Conflict` with message: "The alias 'my-promo' is already in use."
3. Modal shows error below alias field: "This alias is taken. Try a different one."
4. Alias input is highlighted in red with focus.
5. User enters a new alias and resubmits.

### Error: Invalid Alias Format

1. Steps 1-7 as above.
2. Client-side validation catches invalid characters (spaces, special chars).
3. Error shown below alias field: "Alias must be 3-30 characters, letters and numbers only."
4. User corrects and resubmits.

### Error: Quota Exceeded

1. Steps 1-9 as above.
2. API returns `429 Too Many Requests` with message indicating which limit was hit.
3. **Decision: Which quota?**
   - **Daily limit:** Modal shows error: "You've reached your daily limit of 50 links. Try again tomorrow or upgrade to Pro."
   - **Total limit:** Modal shows error: "You've reached your limit of 500 active links. Delete or let some expire, or upgrade to Pro."
4. [Upgrade to Pro] button in the error area.

---

## Flow 4: Visitor Clicks a Password-Protected Short Link

**Entry point:** Short URL (`shorty.io/xK3mP9a`)
**API:** `GET /{code}`, then `POST /p/{code}`

### Happy Path

1. Visitor clicks or navigates to `shorty.io/xK3mP9a`.
2. Redirect Lambda looks up the code; finds it is password-protected.
3. Server returns `403` with HTML password form (includes CSRF token).
4. Visitor sees the password entry page: lock icon, password input, [Continue] button.
5. Visitor enters the password shared by the link owner.
6. Visitor clicks [Continue].
7. Browser sends `POST /p/xK3mP9a` with password + CSRF token.
8. Server validates CSRF token, then verifies password (bcrypt compare).
9. Password is correct.
10. Server returns `302 Found` with `Location` header pointing to original URL.
11. Browser redirects visitor to the destination.

### Error: Wrong Password

1. Steps 1-7 as above.
2. Server verifies password; it is incorrect.
3. Server returns `403` with HTML form + error message.
4. Visitor sees: "Wrong password. 4 attempts remaining before lockout."
5. Visitor can try again.
6. **Decision: Attempts remaining?**
   - **Yes (> 0):** Return to step 5.
   - **No (0 remaining):** Continue to lockout flow.

### Error: Lockout (Rate Limited)

1. After 5 failed attempts within 15 minutes from the same IP:
2. Server returns `429 Too Many Requests`.
3. Page shows: "Too many attempts. Try again in 12 minutes."
4. Password input and button are disabled.
5. A countdown timer shows remaining lockout time.
6. After lockout period expires, the page reloads and the form is re-enabled.

### Error: Link Expired or Max Clicks Reached

1. Visitor navigates to the short URL.
2. Server looks up the code; link has expired or reached max clicks.
3. Server returns `410 Gone`.
4. Visitor sees a friendly error page: "This link has expired." with Shorty branding.
5. No password form is shown.

### Error: Link Does Not Exist

1. Visitor navigates to a short URL with an invalid code.
2. Server returns `404 Not Found`.
3. Visitor sees: "Link not found. It may have been deleted or never existed."
4. [Go to Shorty homepage] link.

---

## Flow 5: User Views Link Statistics on Dashboard

**Entry point:** Dashboard link row click, or [Stats] action
**API:** `GET /api/v1/links/{code}`, `GET /api/v1/links/{code}/stats`

### Happy Path

1. User clicks on a link row in the dashboard table (or clicks [Stats] action).
2. System navigates to `/links/xK3mP9a`.
3. Page shows skeleton loading state (pulsing placeholders).
4. System fetches link details: `GET /api/v1/links/xK3mP9a`.
5. System fetches stats: `GET /api/v1/links/xK3mP9a/stats`.
6. Link header populates: short URL, original URL, status badge, metadata.
7. Stat cards animate: total clicks (1,204), unique clicks (847), time remaining (31 days).
8. Clicks-over-time chart renders with default 30-day view.
9. User clicks [7d] tab to narrow the time range.
10. Chart re-fetches data for 7-day window and re-renders.
11. Geographic breakdown and referrer lists populate below the chart.
12. User hovers over a chart data point; tooltip shows: "Jan 22: 87 clicks".

### No Data State

1. Steps 1-6 as above.
2. Stats API returns empty data (zero clicks).
3. Stat cards show "0" for all values.
4. Chart area shows empty state: "No clicks recorded yet. Share your link to start tracking."
5. Geographic and referrer sections are hidden.

### Error: Access Denied

1. User attempts to view stats for a link they do not own (e.g., via direct URL).
2. API returns `403 Forbidden`.
3. Page shows: "You don't have permission to view this link's statistics."
4. [Back to dashboard] link.

### Free Tier Limitation

1. Steps 1-8 as above.
2. User clicks [All] time range tab.
3. System detects free-tier account (30-day stats limit).
4. Chart shows 30-day data only.
5. Banner appears above chart: "Stats history is limited to 30 days on the Free plan. Upgrade to Pro for 1 year of history. [Upgrade]"

---

## Flow 6: User Hits Rate Limit

**Trigger:** Any API call that returns `429 Too Many Requests`
**Applies to:** Link creation, redirect, password attempts

### Scenario A: Anonymous Link Creation Rate Limit (5/hour)

1. Anonymous user has created 5 links in the current hour.
2. User enters another URL and clicks [Shorten].
3. API returns `429` with `Retry-After: 2400` header and response body: `"You have exceeded the guest limit of 5 links per hour."`
4. UI shows:
   - Inline error below input: "Guest limit reached (5 links per hour)."
   - Dismissible banner: "Create a free account for 50 links per day. [Sign up free]"
   - [Shorten] button remains enabled (user might try after window resets).
5. **Recovery path A:** User waits until the rate window resets (shown as approximate time).
6. **Recovery path B:** User clicks [Sign up free], creates account, and gets higher quota immediately.

### Scenario B: Authenticated User Daily Quota (50/day)

1. Authenticated user has created 50 links today.
2. User clicks [+ New Link], fills in URL, clicks [Create Link].
3. API returns `429` with message: "Daily link creation limit reached (50/day)."
4. UI shows:
   - Error in modal: "You've used all 50 links for today. Your quota resets at midnight UTC."
   - Quota bar on dashboard is full (red, 50/50).
   - [Upgrade to Pro] button in the error message.
5. **Recovery path A:** User waits until midnight UTC (quota bar shows reset countdown).
6. **Recovery path B:** User clicks [Upgrade to Pro] to get 500 links/day (post-MVP).

### Scenario C: Redirect Rate Limit (200/min/IP)

1. A visitor (or automated client) exceeds 200 redirect requests per minute from one IP.
2. Next redirect request returns `429` with `Retry-After: 30` header.
3. Visitor sees a simple HTML error page:
   - "Too many requests. Please wait a moment and try again."
   - No Shorty branding or navigation (minimal page for performance).
   - Auto-retry after Retry-After period via `<meta http-equiv="refresh">`.
4. **Recovery:** Wait for the rate window to reset (typically under 60 seconds).

### Scenario D: Password Attempt Rate Limit (5/15min/IP/link)

1. Visitor has entered 5 incorrect passwords within 15 minutes for a specific link.
2. Visitor submits a 6th password attempt.
3. API returns `429`.
4. Password form is disabled. Message: "Too many attempts. Try again in N minutes."
5. Countdown timer shows remaining lockout time.
6. **Recovery:** Wait for the 15-minute window to expire. Timer counts down on-screen.

### Rate Limit Headers (all responses)

Every API response includes these headers so the client can proactively show quota state:

| Header | Description | Example |
|---|---|---|
| `X-RateLimit-Limit` | Maximum requests in window | `50` |
| `X-RateLimit-Remaining` | Requests remaining | `12` |
| `X-RateLimit-Reset` | Unix timestamp when window resets | `1706400000` |
| `Retry-After` | Seconds to wait (only on 429) | `3600` |
