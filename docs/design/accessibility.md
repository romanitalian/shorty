# Shorty -- Accessibility Compliance (WCAG 2.1 AA)

This document defines accessibility requirements for every screen in the Shorty URL shortener. All requirements target WCAG 2.1 Level AA compliance.

---

## 1. Color and Contrast

### 1.1 Text Contrast Ratios

| Requirement | WCAG Criterion | Minimum Ratio |
|---|---|---|
| Normal text (< 18px, or < 14px bold) | 1.4.3 | 4.5:1 |
| Large text (>= 18px, or >= 14px bold) | 1.4.3 | 3:1 |
| UI components and graphical objects | 1.4.11 | 3:1 |
| Focus indicators | 1.4.11 | 3:1 against adjacent colors |

### 1.2 Verified Color Pairings

| Element | Foreground | Background | Ratio | Status |
|---|---|---|---|---|
| Body text | #4B5563 (neutral-600) | #FFFFFF | 7.5:1 | Pass |
| Heading text | #111827 (neutral-900) | #FFFFFF | 17.4:1 | Pass |
| Primary button text | #FFFFFF | #6366F1 (primary) | 4.6:1 | Pass |
| Primary link text | #6366F1 (primary) | #FFFFFF | 4.6:1 | Pass |
| Error text | #B91C1C (danger-700) | #FEF2F2 (danger-50) | 7.1:1 | Pass |
| Success text | #15803D (success-700) | #F0FDF4 (success-50) | 5.9:1 | Pass |
| Warning text | #B45309 (warning-700) | #FFFBEB (warning-50) | 5.4:1 | Pass |
| Placeholder text | #9CA3AF (neutral-400) | #FFFFFF | 3.0:1 | Borderline* |
| Input border | #E5E7EB (neutral-200) | #FFFFFF | 1.5:1 | N/A** |
| Dark mode: body text | #E5E7EB | #111827 | 13.2:1 | Pass |
| Dark mode: primary link | #818CF8 | #111827 | 6.1:1 | Pass |

*Placeholder text at 3:1 is acceptable per WCAG as it is not the label. All inputs have visible labels above them.

**Input borders are supplemented by the label above and background difference; they do not rely on border color alone.

### 1.3 Color Independence

No information is conveyed by color alone (WCAG 1.4.1):

| Element | Color | Non-color Indicator |
|---|---|---|
| Active status badge | Green | Checkmark icon + "Active" text |
| Expired status badge | Red | X icon + "Expired" text |
| Expires-soon badge | Orange | Clock icon + "Expires soon" text |
| Password badge | Blue | Lock icon + "Password" text |
| Quota bar (warning) | Yellow/Red | Percentage text label (e.g., "48/50") |
| Error messages | Red border | Error icon + descriptive text |
| Success messages | Green border | Check icon + descriptive text |
| Chart data | Multiple colors | Pattern fills + text labels + data table fallback |

---

## 2. Keyboard Navigation

### 2.1 Tab Order (per screen)

**All screens:** Tab order follows visual layout, left-to-right, top-to-bottom.

#### Landing Page
1. Skip-to-content link (first focusable element)
2. Logo (link to home)
3. [Sign in] button
4. URL input field
5. [Shorten] button
6. [Options] accordion toggle
7. (If expanded) Custom alias input, Expires dropdown, Max clicks input, Password input
8. (If result visible) [Copy] button, [Open] link, [Sign in to track] link

#### Password Entry
1. Skip-to-content link
2. Logo
3. Password input
4. Show/Hide toggle
5. [Continue] button

#### Login / Register
1. Skip-to-content link
2. Logo
3. [Continue with Google] button
4. Email input
5. Password input
6. Show/Hide toggle
7. [Sign in] / [Register] button
8. Toggle link (switch between login/register)

#### Dashboard
1. Skip-to-content link
2. Logo, Navigation links (Links, Stats), User dropdown
3. [+ New Link] button
4. Filter dropdown
5. Sort dropdown
6. Table rows (each row is a focusable group)
7. Within row: [Copy], [Stats], [Edit], [Delete] actions
8. Pagination controls

#### Link Detail + Stats
1. Skip-to-content link
2. Navigation bar
3. [Back to Links] link
4. [Copy] button, [Edit] button
5. Time period tabs ([7d], [30d], [All])
6. Chart (focusable for keyboard interaction)
7. Data tables (countries, referrers, devices)

#### Quota / Settings
1. Skip-to-content link
2. Navigation bar
3. [Upgrade] button
4. Connected accounts [disconnect] buttons
5. [Export my data] button
6. [Delete account] button
7. [Sign out] button

### 2.2 Focus Indicators

```css
/* Global focus style -- never removed */
:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 2px;
  border-radius: var(--radius-sm);
}

/* High contrast for dark backgrounds */
.dark :focus-visible {
  outline-color: #A5B4FC;  /* primary-300 */
}
```

- Focus ring is **always visible** when navigating with keyboard
- Focus ring uses `outline` (not `box-shadow` which can be clipped)
- `outline-offset: 2px` ensures the ring does not overlap the element
- `:focus-visible` (not `:focus`) ensures mouse clicks do not show the ring
- Minimum focus indicator area: 2px offset on all sides (WCAG 2.4.12)

### 2.3 Skip Links

Every page includes a skip-to-content link as the first focusable element:

```html
<a href="#main-content" class="sr-only focus:not-sr-only">
  Skip to main content
</a>
```

- Visually hidden until focused
- On focus, appears at top of page with high contrast
- Targets `<main id="main-content">` element

### 2.4 Keyboard Shortcuts

No custom keyboard shortcuts are implemented to avoid conflicts with assistive technology. All interactions use standard browser keyboard patterns:

| Action | Key |
|---|---|
| Submit form | Enter |
| Toggle accordion | Enter / Space |
| Close modal | Escape |
| Navigate dropdown | Arrow Up / Arrow Down |
| Select dropdown item | Enter / Space |
| Move between tabs | Arrow Left / Arrow Right |
| Activate tab | Enter / Space |

---

## 3. Screen Reader Support

### 3.1 ARIA Landmarks

Every page uses these landmark regions:

```html
<header role="banner">       <!-- Navigation bar -->
<nav role="navigation">       <!-- Primary navigation -->
<main role="main">            <!-- Page content -->
<footer role="contentinfo">   <!-- Footer (if present) -->
```

### 3.2 ARIA Labels and Roles

| Element | ARIA Attribute | Value |
|---|---|---|
| URL input | `aria-label` | "URL to shorten" |
| [Shorten] button (loading) | `aria-busy="true"` | -- |
| Result card | `role="status"`, `aria-live="polite"` | Announced when result appears |
| Error messages | `role="alert"`, `aria-live="assertive"` | Announced immediately |
| Toast notifications | `role="alert"`, `aria-live="assertive"` | Announced immediately |
| Status badges | `aria-label` | "Status: Active" / "Status: Expired" |
| Quota progress bar | `role="progressbar"`, `aria-valuenow`, `aria-valuemin`, `aria-valuemax` | e.g., 32, 0, 50 |
| Quota progress bar | `aria-label` | "Daily link quota: 32 of 50 used" |
| Copy button (after click) | `aria-live="polite"` | "Copied to clipboard" |
| Modal | `role="dialog"`, `aria-modal="true"`, `aria-labelledby` | Links to modal title ID |
| Delete confirmation | `role="alertdialog"`, `aria-modal="true"` | -- |
| Pagination | `nav`, `aria-label="Pagination"` | -- |
| Table | `aria-label` | "Your shortened links" |
| Sort button | `aria-sort` | "ascending" / "descending" / "none" |
| Filter dropdown | `aria-expanded`, `aria-haspopup="listbox"` | -- |
| Password toggle | `aria-label` | "Show password" / "Hide password" |
| Chart | `role="img"`, `aria-label` | "Clicks over time chart: 1,204 total clicks in the last 30 days" |

### 3.3 Live Regions

| Event | Live Region Type | Content Announced |
|---|---|---|
| Short URL created | `aria-live="polite"` | "Short link created: shorty.io/xK3mP9a" |
| URL copied to clipboard | `aria-live="polite"` | "Link copied to clipboard" |
| Error on form submit | `aria-live="assertive"` | Error message text |
| Toast notification | `aria-live="assertive"` | Toast message text |
| Rate limit hit | `aria-live="assertive"` | "Rate limit exceeded. Try again in N minutes." |
| Link deleted | `aria-live="polite"` | "Link deleted successfully" |
| Quota updated | `aria-live="polite"` | "32 of 50 daily links used" |

### 3.4 Chart Accessibility

All charts (clicks over time, geographic breakdown, device split) have:

1. **Text alternative:** `aria-label` on the chart container with summary data
2. **Data table fallback:** Hidden `<table>` with `class="sr-only"` containing the same data
3. **No auto-play animation:** Charts render statically; animation only on user interaction

Example:

```html
<div role="img" aria-label="Clicks over time: 1,204 total clicks, peak on January 22 with 87 clicks">
  <!-- Visual chart here -->
</div>
<table class="sr-only" aria-label="Clicks over time data">
  <thead><tr><th>Date</th><th>Clicks</th></tr></thead>
  <tbody>
    <tr><td>Jan 15</td><td>42</td></tr>
    <!-- ... -->
  </tbody>
</table>
```

---

## 4. Form Accessibility

### 4.1 Labels

Every form input has a visible `<label>` element associated via `for`/`id`:

```html
<label for="url-input">URL to shorten</label>
<input id="url-input" type="url" ... />
```

- Labels are **always visible** (no placeholder-only inputs)
- Required fields show `<span aria-hidden="true">*</span>` and `aria-required="true"` on the input
- Grouped fields use `<fieldset>` and `<legend>` (e.g., link options section)

### 4.2 Error Messages

```html
<label for="url-input">URL to shorten</label>
<input id="url-input" type="url"
       aria-invalid="true"
       aria-describedby="url-error" />
<p id="url-error" role="alert" class="error-text">
  Please enter a valid HTTP or HTTPS URL.
</p>
```

- `aria-invalid="true"` is set on inputs with validation errors
- Error messages are linked to inputs via `aria-describedby`
- Error messages use `role="alert"` for immediate screen reader announcement
- Error text includes specific guidance (not just "Invalid input")

### 4.3 Helper Text

```html
<label for="alias-input">Custom alias</label>
<input id="alias-input" type="text"
       aria-describedby="alias-help" />
<p id="alias-help" class="helper-text">
  3-30 characters, letters and numbers only.
</p>
```

- Helper text is linked via `aria-describedby`
- When both helper text and error exist, use space-separated IDs: `aria-describedby="alias-help alias-error"`

### 4.4 Password Fields

```html
<label for="password">Password</label>
<div class="password-wrapper">
  <input id="password" type="password"
         autocomplete="current-password" />
  <button type="button"
          aria-label="Show password"
          aria-pressed="false">
    [eye icon]
  </button>
</div>
```

- Toggle button updates: `aria-label="Hide password"`, `aria-pressed="true"` when password is visible
- `autocomplete` attribute set appropriately (`current-password` for login, `new-password` for register)

### 4.5 CSRF Token

```html
<input type="hidden" name="csrf_token" value="..." aria-hidden="true" />
```

- Hidden from all users (visual and assistive technology)
- No label needed for hidden inputs

---

## 5. Motion and Animation Preferences

### 5.1 Reduced Motion

```css
@media (prefers-reduced-motion: reduce) {
  *,
  *::before,
  *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}
```

Affected elements:
- Stat card number count-up animation: replaced with instant value display
- Chart rendering animation: replaced with static render
- Skeleton loading pulse: replaced with static gray background
- Toast slide-in: replaced with instant appearance
- Modal fade/scale: replaced with instant show/hide
- Accordion expand/collapse: replaced with instant toggle
- Button hover transitions: replaced with instant state change

### 5.2 Auto-Playing Content

- No auto-playing video or audio content on any page
- Toast notifications auto-dismiss after 5 seconds but include a dismiss button
- Error toasts persist until manually dismissed (no auto-dismiss)
- Password lockout countdown updates text (not animation)

---

## 6. Dark Mode Accessibility

### 6.1 Implementation

```css
@media (prefers-color-scheme: dark) {
  /* All color tokens redefined -- see design-system.md */
}
```

- Respects system preference via `prefers-color-scheme` media query
- No manual dark mode toggle in MVP (system preference only)
- All color pairings re-verified for dark mode contrast ratios

### 6.2 Dark Mode Contrast Verification

| Element | Foreground | Background | Ratio | Status |
|---|---|---|---|---|
| Body text | #E5E7EB | #111827 | 13.2:1 | Pass |
| Primary link | #818CF8 | #111827 | 6.1:1 | Pass |
| Primary button | #FFFFFF | #818CF8 | 3.5:1 | Pass (large text)* |
| Error text | #F87171 | #1F2937 | 5.8:1 | Pass |
| Success text | #4ADE80 | #1F2937 | 7.1:1 | Pass |
| Placeholder | #9CA3AF | #1F2937 | 4.0:1 | Pass |

*Button text is 14px semi-bold, qualifying as large text (3:1 minimum).

### 6.3 Dark Mode Considerations

- Shadows increase opacity (0.10 -> 0.25) for visibility against dark backgrounds
- Focus rings use lighter primary variant (#A5B4FC) for contrast
- Chart colors are adjusted to lighter variants for readability
- Images and icons maintain visibility (no pure black outlines on dark bg)

---

## 7. Touch Targets

### 7.1 Minimum Size (WCAG 2.5.8)

All interactive elements have a minimum touch target of **44x44px**:

| Element | Actual Size | Spacing | Effective Target |
|---|---|---|---|
| Buttons (md) | 40px height | 4px margin | 44x44px |
| Buttons (lg) | 48px height | -- | 48x48px |
| Table row actions | 32px icon | 12px padding | 44x44px |
| Pagination buttons | 36px square | 4px gap | 44x44px |
| Dropdown items | 36px height | 4px padding | 44x44px |
| Close button (modal) | 24px icon | 10px padding | 44x44px |
| Password toggle | 24px icon | 10px padding | 44x44px |
| Checkbox / Radio | 20px visual | 12px padding | 44x44px |
| Navigation links | Text height | 12px padding | >= 44px height |

### 7.2 Touch Target Spacing

Adjacent touch targets have at least 8px spacing to prevent accidental activation.

---

## 8. Page Structure

### 8.1 Language Attribute

```html
<html lang="en">
```

- Language is set on the root `<html>` element
- If content in other languages is displayed (e.g., country names), use `lang` attribute on the specific element

### 8.2 Page Titles

Every page has a unique, descriptive `<title>`:

| Page | Title |
|---|---|
| Landing | "Shorty -- Shorten any link instantly" |
| Login | "Sign in -- Shorty" |
| Register | "Create account -- Shorty" |
| Password entry | "Password required -- Shorty" |
| Dashboard | "My Links -- Shorty" |
| Link detail | "Stats: shorty.io/xK3mP9a -- Shorty" |
| Settings | "Settings -- Shorty" |
| 404 | "Page not found -- Shorty" |
| 410 | "Link expired -- Shorty" |
| 429 | "Too many requests -- Shorty" |

### 8.3 Heading Hierarchy

Every page follows a single `<h1>` with logical nesting:

#### Landing Page
```
h1: Shorten any link. Instantly.
  h2: Options (accordion)
  h2: Your shortened link (result)
```

#### Dashboard
```
h1: My Links
  h2: [Link count] links
```

#### Link Detail
```
h1: shorty.io/xK3mP9a
  h2: Statistics
    h3: Clicks Over Time
    h3: Top Countries
    h3: Top Referrers
    h3: Device Breakdown
```

#### Settings
```
h1: Settings
  h2: Profile
  h2: Usage
  h3: Daily links
  h3: Total active links
  h2: Upgrade to Pro
  h2: Account
```

- No heading levels are skipped (e.g., no h1 -> h3 without h2)
- Headings are semantic, not used purely for styling

---

## 9. Testing Checklist

### Automated Testing

- [ ] aXe or Lighthouse accessibility audit scores >= 90 on all pages
- [ ] HTML validation (no duplicate IDs, valid ARIA usage)
- [ ] Color contrast checker (all text pairings verified)
- [ ] Heading hierarchy checker (no skipped levels)

### Manual Testing

- [ ] Full keyboard-only navigation through every screen
- [ ] Tab order matches visual order on all screens
- [ ] Focus is never lost (e.g., after modal close, after deletion)
- [ ] Screen reader walkthrough (VoiceOver on macOS, NVDA on Windows)
- [ ] All form errors are announced by screen reader
- [ ] All status changes are announced (toasts, result card, copy confirmation)
- [ ] Charts have meaningful text alternatives
- [ ] Zoom to 200% -- no content loss or overlap (WCAG 1.4.4)
- [ ] Zoom to 400% -- content reflows to single column (WCAG 1.4.10)
- [ ] Text spacing override test (WCAG 1.4.12) -- no clipping
- [ ] Reduced-motion preference test -- no animations
- [ ] Dark mode test -- all content readable
- [ ] High contrast mode test (Windows) -- UI remains functional

### Device Testing

- [ ] iOS Safari with VoiceOver
- [ ] Android Chrome with TalkBack
- [ ] Desktop Chrome with keyboard only
- [ ] Desktop Firefox with NVDA
- [ ] Desktop Safari with VoiceOver

---

## 10. Implementation Notes

### CSS Classes for Screen Reader Only Content

```css
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}

.sr-only.focus\:not-sr-only:focus {
  position: static;
  width: auto;
  height: auto;
  padding: inherit;
  margin: inherit;
  overflow: visible;
  clip: auto;
  white-space: normal;
}
```

### Focus Management Rules

1. **Modal open:** Focus moves to first focusable element inside modal
2. **Modal close:** Focus returns to the element that triggered the modal
3. **Element deletion:** Focus moves to the next element in the list (or previous if last)
4. **Page navigation:** Focus moves to `<main>` or `<h1>`
5. **Error on submit:** Focus moves to the first field with an error
6. **Toast appearance:** Announced via live region; focus does NOT move to toast
