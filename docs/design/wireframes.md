# Shorty -- Screen Wireframes

All wireframes follow mobile-first design. ASCII art shows desktop layout; responsive notes describe mobile adaptations.

---

## Screen 1: Landing / Guest Shortener

```
+================================================================+
|  SHORTY                                         [Sign in]      |
+================================================================+
|                                                                |
|              Shorten any link. Instantly.                       |
|              Fast, trackable, free.                             |
|                                                                |
|  +---------------------------------------------+  +---------+ |
|  | https://paste-your-long-url-here...          |  | Shorten | |
|  +---------------------------------------------+  +---------+ |
|                                                                |
|  [v Options]  <-- collapsed accordion, click to expand         |
|  +------------------------------------------------------------+|
|  |  Custom alias:  [______________]  (3-30 chars, a-z 0-9)   ||
|  |  Expires in:    [24 hours   v ]   (guest max: 24h)         ||
|  |  Max clicks:    [__________]      (optional)               ||
|  |  Password:      [______________]  (optional)               ||
|  +------------------------------------------------------------+|
|                                                                |
|  Guest limit: 5 links/day remaining (3 left)                   |
|                                                                |
|  ------- Result (shown after successful submit) -------        |
|  +------------------------------------------------------------+|
|  |  shorty.io/xK3mP9a                    [Copy]  [Open ->]   ||
|  |  Link created -- expires in 24 hours                       ||
|  |  Sign in to track clicks and get 50 links/day ->           ||
|  +------------------------------------------------------------+|
|                                                                |
|  ------- Error State (inline, replaces result) -------         |
|  +------------------------------------------------------------+|
|  |  [!] Invalid URL. Please enter a valid http/https URL.     ||
|  +------------------------------------------------------------+|
|                                                                |
+================================================================+
```

### Component Annotations

| Element | Behavior |
|---|---|
| Logo "SHORTY" | Links to `/` (landing page) |
| [Sign in] | Navigates to `/login` |
| URL input | Auto-focus on page load. Validates on blur and submit. Placeholder text shown. |
| [Shorten] button | Disabled until URL field is non-empty. Shows spinner during submission. |
| Options accordion | Collapsed by default. Toggle with keyboard (Enter/Space). Custom alias and password fields are hidden for guests unless expanded. |
| Result card | Appears below input after 201 response. Short URL in monospace font (JetBrains Mono). |
| [Copy] button | Copies short URL to clipboard. Text changes to "Copied!" for 2 seconds. |
| [Open ->] | Opens short URL in new tab (`target="_blank" rel="noopener"`). |
| Guest limit notice | Always visible for unauthenticated users. Updates after each creation. |
| Error inline | Red border on input + error message below. Clears on new input. |

### States

1. **Default** -- Empty input, shorten button disabled
2. **Typing** -- Button enabled, validation on blur
3. **Submitting** -- Button shows spinner, input disabled
4. **Success** -- Result card visible, input cleared for next URL
5. **Error** -- Inline error message, input keeps value for correction
6. **Rate limited** -- Toast notification: "Daily limit reached. Sign in for 50 links/day."

### Responsive Notes (Mobile)

- URL input and button stack vertically (full-width button below input)
- Options accordion is full-width
- Result card is full-width with larger touch targets for Copy/Open
- Logo and Sign-in button remain in single-row header

---

## Screen 2: Password Entry Page (`/p/{code}`)

```
+==========================================+
|  SHORTY                                  |
+==========================================+
|                                          |
|          [Lock Icon]                     |
|                                          |
|    This link is password protected       |
|                                          |
|    Password                              |
|    +--------------------------------+    |
|    | ********************************|    |
|    +--------------------------------+    |
|                     [Show/Hide toggle]   |
|                                          |
|    <input type="hidden"                  |
|     name="csrf_token"                    |
|     value="...">                         |
|                                          |
|    +--------------------------------+    |
|    |          Continue               |    |
|    +--------------------------------+    |
|                                          |
|    -- Error state --                     |
|    [!] Wrong password.                   |
|    4 attempts remaining before lockout.  |
|                                          |
|    -- Lockout state --                   |
|    [!] Too many attempts.                |
|    Try again in 12 minutes.              |
|                                          |
+==========================================+
```

### Component Annotations

| Element | Behavior |
|---|---|
| Lock icon | Decorative; `aria-hidden="true"` |
| Password input | `type="password"`, `autocomplete="off"`, `aria-label="Link password"` |
| Show/Hide toggle | Button toggles input type between password/text. Label updates: "Show password" / "Hide password". |
| CSRF token | Hidden input; server-rendered per session. |
| [Continue] button | Submits form via POST to `/p/{code}`. Disabled during submission. |
| Error message | `aria-live="polite"` region. Shows remaining attempts (5 max per 15 min per IP). |
| Lockout message | Shows countdown timer. No form interaction possible. |

### States

1. **Default** -- Empty password field, Continue button enabled
2. **Submitting** -- Button shows spinner, field disabled
3. **Error (wrong password)** -- Error message with remaining attempts
4. **Lockout (429)** -- All inputs disabled, countdown to retry
5. **Success** -- 302 redirect (user never sees a success state on this page)

### Responsive Notes (Mobile)

- Card is full-width with comfortable padding (16px)
- Password input and button are full-width
- Touch target for Show/Hide is at least 44x44px

---

## Screen 3: Login / Register

```
+==========================================+
|  SHORTY                                  |
+==========================================+
|                                          |
|          Welcome back                    |
|                                          |
|  +------------------------------------+  |
|  |  [G] Continue with Google           |  |
|  +------------------------------------+  |
|                                          |
|  --------------- or ----------------     |
|                                          |
|  Email                                   |
|  +------------------------------------+  |
|  | user@example.com                    |  |
|  +------------------------------------+  |
|                                          |
|  Password                                |
|  +------------------------------------+  |
|  | **********************              |  |
|  +------------------------------------+  |
|                           [Show/Hide]    |
|                                          |
|  +------------------------------------+  |
|  |            Sign in                  |  |
|  +------------------------------------+  |
|                                          |
|  Don't have an account? Register ->      |
|                                          |
|  -- Error state --                       |
|  +------------------------------------+  |
|  | [!] Invalid email or password.      |  |
|  +------------------------------------+  |
|                                          |
+==========================================+

--- Register variant (same layout, different labels) ---

|          Create your account             |
|  [G] Continue with Google                |
|  Email: [________________________]       |
|  Password: [________________________]    |
|  (Min 8 chars, 1 uppercase, 1 number)    |
|  [          Register          ]          |
|  Already have an account? Sign in ->     |
```

### Component Annotations

| Element | Behavior |
|---|---|
| [G] Continue with Google | Initiates Cognito Google OAuth flow. Full-width button. Google brand colors. |
| "or" divider | Visual separator between SSO and email/password |
| Email input | `type="email"`, `autocomplete="email"`, required. Validated on blur (format check). |
| Password input | `type="password"`, `autocomplete="current-password"` (login) or `autocomplete="new-password"` (register). |
| Show/Hide toggle | Same behavior as password entry page |
| [Sign in] / [Register] | Primary button. Shows spinner during API call. |
| Toggle link | "Don't have an account?" / "Already have an account?" -- toggles between login and register views. Client-side navigation. |
| Error message | Generic "Invalid email or password" (never reveals which field is wrong for security). |

### States

1. **Login default** -- Empty fields
2. **Register default** -- Empty fields with password requirements shown
3. **Submitting** -- Button spinner, fields disabled
4. **Error** -- Inline error below form
5. **SSO redirect** -- Full-page redirect to Google
6. **Success** -- Redirect to dashboard (or previous page if deep-linked)

### Responsive Notes (Mobile)

- Full-width form, centered on tablet/desktop (max-width: 400px)
- Google button and form inputs are full-width
- Comfortable spacing between elements (16px gaps)

---

## Screen 4: Dashboard (Link List)

```
+====================================================================+
|  SHORTY     Links    Stats         [user@email.com v]              |
+====================================================================+
|                                                                    |
|  [+ New Link]                    [Active v]  [Sort: Newest v]     |
|                                                                    |
|  Quota: [======------] 32/50 links today                           |
|                                                                    |
|  +----------------------------------------------------------------+|
|  | SHORT URL         ORIGINAL URL          CLICKS  STATUS  ACTIONS||
|  +----------------------------------------------------------------+|
|  | shorty.io/xK3mP9a example.com/long...   1,204   Active  [...] ||
|  | shorty.io/mNq2Wr  blog.io/post-title    42      Active  [...] ||
|  | shorty.io/pLx8Yt  drive.google.com/...  0       Expires [...] ||
|  |                                          soon                  ||
|  | shorty.io/aB3cDe  github.com/repo/...   890     Expired [...] ||
|  | shorty.io/qR5sT7  medium.com/@user/...  15      Pass.   [...] ||
|  +----------------------------------------------------------------+|
|                                                                    |
|  Status legend: [*] Active  [~] Expires soon  [x] Expired         |
|                 [#] Password protected                             |
|                                                                    |
|  <- Prev   Page 1 of 4   Next ->                                  |
|                                                                    |
|  --- Empty State (no links yet) ---                                |
|  +----------------------------------------------------------------+|
|  |        [Link icon]                                             ||
|  |        No links yet                                            ||
|  |        Create your first short link to get started.            ||
|  |        [+ Create Link]                                         ||
|  +----------------------------------------------------------------+|
|                                                                    |
+====================================================================+
```

### Row Hover Actions (Desktop)

```
| shorty.io/xK3mP9a  example.com/long...   1,204  Active           |
|                                    [Copy] [Stats] [Edit] [Delete] |
```

### Component Annotations

| Element | Behavior |
|---|---|
| Navigation bar | Logo, Links (active), Stats, user dropdown (settings, sign out) |
| [+ New Link] | Opens create-link modal (same fields as landing page options, minus guest restrictions) |
| Filter dropdown | Options: All, Active, Expired, Password-protected |
| Sort dropdown | Options: Newest, Oldest, Most clicks, Least clicks |
| Quota progress bar | Color transitions: green (0-70%), yellow (70-90%), red (90-100%) |
| Table rows | Clickable -- navigates to link detail. Short URLs in monospace. |
| Status badges | Color-coded: green (Active), orange (Expires soon -- within 24h), red (Expired), blue (Password) |
| [...] actions menu | Mobile: kebab menu. Desktop: hover-reveal buttons. |
| [Copy] | Copies short URL. Toast: "Copied to clipboard" |
| [Stats] | Navigates to link detail/stats view |
| [Edit] | Opens edit modal (title, is_active, expires_at) |
| [Delete] | Confirmation modal: "Delete shorty.io/xK3mP9a? This cannot be undone." [Cancel] [Delete] |
| Pagination | Cursor-based. Shows page N of M. Prev/Next buttons. |
| Empty state | Friendly illustration + CTA button |

### States

1. **Loading** -- Skeleton rows (3-5 pulsing placeholder rows)
2. **Populated** -- Table with data
3. **Empty** -- No links created yet (CTA to create first link)
4. **Error** -- "Failed to load links. [Retry]" message
5. **Deleting** -- Row dims with spinner overlay, removed on success

### Responsive Notes (Mobile)

- Table transforms to card list on screens < 768px
- Each card shows: short URL, original (truncated), clicks, status badge
- Actions available via swipe-left or kebab menu
- Filters collapse into a single [Filter v] button
- Pagination becomes infinite scroll with "Load more" button

---

## Screen 5: Link Detail + Stats

```
+====================================================================+
|  SHORTY     Links    Stats         [user@email.com v]              |
+====================================================================+
|                                                                    |
|  <- Back to Links                                                  |
|                                                                    |
|  shorty.io/xK3mP9a                   [Active]  [Copy]  [Edit]    |
|  https://example.com/very/long/original/url/that/is/truncated     |
|  Created: Jan 15, 2026  |  Expires: Feb 15, 2026  |  Max: --     |
|                                                                    |
|  +---------------+  +---------------+  +---------------+          |
|  |    1,204      |  |      847      |  |   31 days     |          |
|  |  Total Clicks |  | Unique Clicks |  |  Remaining    |          |
|  +---------------+  +---------------+  +---------------+          |
|                                                                    |
|  Clicks Over Time                          [7d] [30d] [All]       |
|  +----------------------------------------------------------------+|
|  |                                                                ||
|  |     *                                                          ||
|  |    * *        *                                                ||
|  |   *   *      * *     *                                         ||
|  |  *     *    *   *   * *                                        ||
|  | *       *  *     * *   *                                       ||
|  |          **       *     *                                      ||
|  |                          *                                     ||
|  +--+--+--+--+--+--+--+--+--+--+--+--+--+--+--> time            ||
|  | Jan 15      Jan 20      Jan 25      Jan 30                    ||
|  +----------------------------------------------------------------+|
|                                                                    |
|  +-----------------------------+  +------------------------------+ |
|  | Top Countries               |  | Top Referrers                | |
|  |-----------------------------|  |------------------------------| |
|  | US   [========--]  54%  648 |  | twitter.com     38%    456   | |
|  | DE   [===-------]  18%  216 |  | direct          30%    360   | |
|  | GB   [==--------]  11%  132 |  | google.com      22%    264   | |
|  | FR   [=---------]   7%   84 |  | reddit.com       7%     84   | |
|  | Other [---------]  10%  124 |  | other            3%     40   | |
|  +-----------------------------+  +------------------------------+ |
|                                                                    |
|  +----------------------------------------------------------------+|
|  | Device Breakdown                                               ||
|  | Desktop  [============--------]  68%                           ||
|  | Mobile   [=======-------------]  30%                           ||
|  | Bot      [=-------------------]   2%                           ||
|  +----------------------------------------------------------------+|
|                                                                    |
|  -- Chart fallback (screen reader) --                              |
|  <table> with same data for accessibility                          |
|                                                                    |
+====================================================================+
```

### Component Annotations

| Element | Behavior |
|---|---|
| <- Back to Links | Returns to dashboard list, preserving filter/page state |
| Link header | Short URL (monospace, clickable), original URL (truncated with tooltip for full), metadata |
| [Active] badge | Status badge, same color coding as dashboard |
| [Copy] | Copies short URL to clipboard |
| [Edit] | Opens inline edit: title field, active toggle, expiry date picker. [Save] [Cancel] |
| Stat cards (3) | Total clicks, unique clicks, time remaining (or "No expiry"). Animate counting up on load. |
| Time period tabs | [7d] [30d] [All]. Defaults to 30d. Reloads chart data via API. |
| Clicks chart | Line chart with data points. Tooltip on hover shows date + count. |
| Top Countries | Bar chart with country flag, name, bar, percentage, count. Max 5 + "Other". |
| Top Referrers | List with domain, percentage, count. "direct" for no-referrer traffic. |
| Device Breakdown | Horizontal stacked bars with labels |
| Chart fallback | Hidden `<table>` elements with same data; visible to screen readers via `sr-only` class |

### States

1. **Loading** -- Skeleton placeholders for stat cards and charts
2. **Populated** -- Full data display
3. **No data** -- "No clicks recorded yet. Share your link to start tracking."
4. **Error** -- "Failed to load statistics. [Retry]"
5. **Free tier limit** -- Stats older than 30 days: "Upgrade for full history" banner

### Responsive Notes (Mobile)

- Stat cards stack to 1 per row (full-width)
- Chart becomes horizontally scrollable
- Country and referrer lists are full-width, stacked vertically
- Time period tabs become a dropdown select
- Edit controls use full-screen modal instead of inline edit

---

## Screen 6: Quota / Settings

```
+==========================================+
|  SHORTY     Links    Stats               |
|                          [user@email v]  |
+==========================================+
|                                          |
|  Profile                                 |
|  user@example.com                        |
|  Free Plan                               |
|                                          |
|  +--------------------------------------+|
|  |  Usage                               ||
|  |                                       ||
|  |  Links today                          ||
|  |  [============--------]  32 / 50      ||
|  |                                       ||
|  |  Total active links                   ||
|  |  [========------------]  201 / 500    ||
|  |                                       ||
|  |  Stats retention        30 days       ||
|  |                                       ||
|  +--------------------------------------+|
|                                          |
|  +--------------------------------------+|
|  |  Upgrade to Pro                       ||
|  |                                       ||
|  |  * 500 links/day                      ||
|  |  * 10,000 total links                 ||
|  |  * 1 year stats retention             ||
|  |  * Custom aliases                     ||
|  |  * API access                         ||
|  |                                       ||
|  |  [       Upgrade ->       ]           ||
|  +--------------------------------------+|
|                                          |
|  +--------------------------------------+|
|  |  Account                              ||
|  |                                       ||
|  |  Connected accounts:                  ||
|  |  [G] Google  user@gmail.com  [x]      ||
|  |                                       ||
|  |  [Export my data]                     ||
|  |  [Delete account]                     ||
|  +--------------------------------------+|
|                                          |
|  [Sign out]                              |
|                                          |
+==========================================+
```

### Component Annotations

| Element | Behavior |
|---|---|
| Profile header | Email, plan name. Non-editable display. |
| Usage section | Progress bars with current/limit numbers. Color: green (0-70%), yellow (70-90%), red (90-100%). |
| Links today bar | Resets at midnight UTC. Shows real-time value from GET /api/v1/me. |
| Total active bar | Count of all non-expired, non-deleted links. |
| Stats retention | Text display of plan-specific retention period. |
| Upgrade card | Feature comparison list. CTA button. Visually prominent (border or background). |
| [Upgrade ->] | Navigates to billing page (post-MVP; currently shows "Coming soon" toast). |
| Connected accounts | Shows OAuth providers linked to account. [x] to disconnect (with confirmation). |
| [Export my data] | Triggers async export (CSV). Toast: "Export started. You'll receive an email when ready." |
| [Delete account] | Danger zone. Confirmation modal with re-type email. Permanent deletion. |
| [Sign out] | Clears tokens, redirects to landing page. |

### States

1. **Loading** -- Skeleton for usage bars
2. **Populated** -- Full data
3. **Quota warning** -- Yellow/red bars with "You're running low on links today" banner
4. **Quota exhausted** -- Red bar at 100%, "Limit reached" message, link to upgrade

### Responsive Notes (Mobile)

- All sections stack vertically, full-width
- Progress bars remain full-width
- Upgrade card has larger padding and button for easy tapping
- Sign out button at bottom, full-width
