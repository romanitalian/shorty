# Shorty -- Design System

Design tokens and component specifications for consistent implementation across all screens.

---

## 1. Colors

### Light Mode

```css
:root {
  /* Primary */
  --color-primary-50:    #EEF2FF;
  --color-primary-100:   #E0E7FF;
  --color-primary-200:   #C7D2FE;
  --color-primary-300:   #A5B4FC;
  --color-primary-400:   #818CF8;
  --color-primary:       #6366F1;   /* Indigo 500 -- CTAs, links, focus rings */
  --color-primary-600:   #4F46E5;   /* Hover state */
  --color-primary-700:   #4338CA;   /* Active/pressed state */
  --color-primary-800:   #3730A3;

  /* Semantic */
  --color-success-50:    #F0FDF4;
  --color-success:       #22C55E;   /* Green 500 -- active status, success toasts */
  --color-success-700:   #15803D;

  --color-warning-50:    #FFFBEB;
  --color-warning:       #F59E0B;   /* Amber 500 -- expires soon, quota warnings */
  --color-warning-700:   #B45309;

  --color-danger-50:     #FEF2F2;
  --color-danger:        #EF4444;   /* Red 500 -- errors, expired, delete actions */
  --color-danger-700:    #B91C1C;

  --color-info-50:       #EFF6FF;
  --color-info:          #3B82F6;   /* Blue 500 -- password-protected badge, info */
  --color-info-700:      #1D4ED8;

  /* Neutral */
  --color-neutral-0:     #FFFFFF;   /* Background */
  --color-neutral-50:    #F9FAFB;   /* Subtle background (cards, table rows) */
  --color-neutral-100:   #F3F4F6;   /* Borders, dividers */
  --color-neutral-200:   #E5E7EB;   /* Input borders */
  --color-neutral-300:   #D1D5DB;   /* Disabled text */
  --color-neutral-400:   #9CA3AF;   /* Placeholder text */
  --color-neutral-500:   #6B7280;   /* Secondary text */
  --color-neutral-600:   #4B5563;   /* Body text */
  --color-neutral-700:   #374151;   /* Headings */
  --color-neutral-800:   #1F2937;
  --color-neutral-900:   #111827;   /* Primary text */
}
```

### Dark Mode

```css
@media (prefers-color-scheme: dark) {
  :root {
    --color-primary:       #818CF8;   /* Indigo 400 -- lighter for contrast */
    --color-primary-600:   #A5B4FC;   /* Hover */

    --color-success:       #4ADE80;   /* Green 400 */
    --color-warning:       #FBBF24;   /* Amber 400 */
    --color-danger:        #F87171;   /* Red 400 */
    --color-info:          #60A5FA;   /* Blue 400 */

    --color-neutral-0:     #111827;   /* Background */
    --color-neutral-50:    #1F2937;   /* Cards */
    --color-neutral-100:   #374151;   /* Borders */
    --color-neutral-200:   #4B5563;
    --color-neutral-300:   #6B7280;
    --color-neutral-400:   #9CA3AF;   /* Placeholder */
    --color-neutral-500:   #D1D5DB;
    --color-neutral-600:   #E5E7EB;   /* Body text */
    --color-neutral-700:   #F3F4F6;
    --color-neutral-900:   #F9FAFB;   /* Primary text */
  }
}
```

### Color Contrast Compliance (WCAG 2.1 AA)

| Usage | Foreground | Background | Ratio | Pass |
|---|---|---|---|---|
| Body text | neutral-600 (#4B5563) | neutral-0 (#FFFFFF) | 7.5:1 | AA |
| Heading text | neutral-900 (#111827) | neutral-0 (#FFFFFF) | 17.4:1 | AAA |
| Primary on white | primary (#6366F1) | neutral-0 (#FFFFFF) | 4.6:1 | AA |
| Error text | danger-700 (#B91C1C) | danger-50 (#FEF2F2) | 7.1:1 | AA |
| Success text | success-700 (#15803D) | success-50 (#F0FDF4) | 5.9:1 | AA |
| Dark: body text | neutral-600 (#E5E7EB) | neutral-0 (#111827) | 13.2:1 | AAA |
| Dark: primary | primary (#818CF8) | neutral-0 (#111827) | 6.1:1 | AA |

---

## 2. Typography

### Font Families

```css
:root {
  --font-sans: 'Inter', system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif;
  --font-mono: 'JetBrains Mono', 'Fira Code', 'Cascadia Code', 'Consolas', monospace;
}
```

- **Inter** -- body text, headings, UI labels
- **JetBrains Mono** -- short URLs, codes, technical values

### Type Scale

| Token | Size | Line Height | Weight | Usage |
|---|---|---|---|---|
| `--text-xs` | 12px / 0.75rem | 16px / 1.33 | 400 | Captions, badges |
| `--text-sm` | 14px / 0.875rem | 20px / 1.43 | 400 | Secondary text, table cells |
| `--text-base` | 16px / 1rem | 24px / 1.5 | 400 | Body text, form labels |
| `--text-lg` | 18px / 1.125rem | 28px / 1.56 | 500 | Card titles, section headers |
| `--text-xl` | 20px / 1.25rem | 28px / 1.4 | 600 | Page subtitles |
| `--text-2xl` | 24px / 1.5rem | 32px / 1.33 | 700 | Page titles |
| `--text-3xl` | 30px / 1.875rem | 36px / 1.2 | 700 | Hero heading |
| `--text-stat` | 36px / 2.25rem | 40px / 1.11 | 700 | Stat card numbers |

### Font Weight

| Token | Value | Usage |
|---|---|---|
| `--font-normal` | 400 | Body text |
| `--font-medium` | 500 | Labels, navigation |
| `--font-semibold` | 600 | Buttons, section headers |
| `--font-bold` | 700 | Page titles, stat numbers |

---

## 3. Spacing

Base unit: **4px**. All spacing uses multiples of 4px.

| Token | Value | Usage |
|---|---|---|
| `--space-0` | 0px | |
| `--space-1` | 4px | Tight inline spacing |
| `--space-2` | 8px | Icon-to-text gap, badge padding |
| `--space-3` | 12px | Input padding, small card padding |
| `--space-4` | 16px | Default element gap, card padding (mobile) |
| `--space-5` | 20px | Section separation |
| `--space-6` | 24px | Card padding (desktop), form field gap |
| `--space-8` | 32px | Section padding |
| `--space-10` | 40px | Page section gap |
| `--space-12` | 48px | Major section separation |
| `--space-16` | 64px | Page top/bottom padding |

---

## 4. Border Radius

| Token | Value | Usage |
|---|---|---|
| `--radius-sm` | 4px | Badges, small elements |
| `--radius-md` | 8px | Buttons, inputs, cards |
| `--radius-lg` | 12px | Modals, large cards |
| `--radius-xl` | 16px | Hero cards, feature sections |
| `--radius-full` | 9999px | Avatars, pills, circular buttons |

---

## 5. Shadows

```css
:root {
  --shadow-sm:  0  1px  2px  0 rgba(0, 0, 0, 0.05);
  --shadow-md:  0  4px  6px -1px rgba(0, 0, 0, 0.10),
                0  2px  4px -2px rgba(0, 0, 0, 0.10);
  --shadow-lg:  0 10px 15px -3px rgba(0, 0, 0, 0.10),
                0  4px  6px -4px rgba(0, 0, 0, 0.10);
  --shadow-xl:  0 20px 25px -5px rgba(0, 0, 0, 0.10),
                0  8px 10px -6px rgba(0, 0, 0, 0.10);
}
```

| Token | Usage |
|---|---|
| `--shadow-sm` | Inputs (focus), subtle card elevation |
| `--shadow-md` | Cards, dropdowns |
| `--shadow-lg` | Modals, floating elements |
| `--shadow-xl` | Toast notifications |

Dark mode: reduce opacity from 0.10 to 0.25 for visibility.

---

## 6. Breakpoints (Mobile-First)

| Token | Min-width | Target |
|---|---|---|
| `--bp-sm` | 640px | Large phones (landscape) |
| `--bp-md` | 768px | Tablets |
| `--bp-lg` | 1024px | Small laptops |
| `--bp-xl` | 1280px | Desktops |

```css
/* Mobile-first: base styles are mobile */
.container { padding: var(--space-4); }

@media (min-width: 640px)  { .container { padding: var(--space-6); } }
@media (min-width: 768px)  { .container { max-width: 720px; } }
@media (min-width: 1024px) { .container { max-width: 960px; } }
@media (min-width: 1280px) { .container { max-width: 1200px; } }
```

---

## 7. Components

### 7.1 Button

| Variant | Background | Text | Border | Hover BG | Active BG |
|---|---|---|---|---|---|
| Primary | primary (#6366F1) | white | none | primary-600 (#4F46E5) | primary-700 (#4338CA) |
| Secondary | transparent | primary (#6366F1) | 1px primary-200 | primary-50 (#EEF2FF) | primary-100 (#E0E7FF) |
| Danger | danger (#EF4444) | white | none | danger-700 (#B91C1C) | #991B1B |
| Ghost | transparent | neutral-600 | none | neutral-100 (#F3F4F6) | neutral-200 (#E5E7EB) |

**Sizes:**

| Size | Height | Padding (H) | Font size | Radius |
|---|---|---|---|---|
| sm | 32px | 12px | 14px | radius-md |
| md | 40px | 16px | 14px | radius-md |
| lg | 48px | 24px | 16px | radius-md |

**States:** default, hover, active, focus (2px ring offset), disabled (opacity 0.5, cursor not-allowed), loading (spinner replaces text or shows beside it).

**Focus ring:** `box-shadow: 0 0 0 2px white, 0 0 0 4px var(--color-primary);`

### 7.2 Input

```
Height:     40px (md), 48px (lg on landing page)
Padding:    12px horizontal
Border:     1px solid var(--color-neutral-200)
Radius:     var(--radius-md) (8px)
Font:       var(--text-base) (16px) -- prevents iOS zoom on focus
Background: var(--color-neutral-0)
```

**States:**

| State | Border | Shadow | Background |
|---|---|---|---|
| Default | neutral-200 | none | white |
| Hover | neutral-300 | none | white |
| Focus | primary | 0 0 0 3px primary-100 | white |
| Error | danger | 0 0 0 3px danger-50 | danger-50 |
| Disabled | neutral-200 | none | neutral-50 |

**Label:** above input, `--text-sm`, `--font-medium`, `--color-neutral-700`. Required fields show red asterisk.

**Helper text:** below input, `--text-xs`, `--color-neutral-500`.

**Error text:** below input, `--text-xs`, `--color-danger-700`. Linked via `aria-describedby`.

### 7.3 Card

```
Background: var(--color-neutral-0)
Border:     1px solid var(--color-neutral-100)
Radius:     var(--radius-lg) (12px)
Shadow:     var(--shadow-md)
Padding:    var(--space-6) (24px)
```

Variants:
- **Default** -- standard card with optional header/footer
- **Stat card** -- centered large number + label, no border on mobile
- **Result card** -- success background (success-50), monospace URL

### 7.4 Table

```
Header:         neutral-50 background, --text-xs uppercase, --font-semibold, --color-neutral-500
Row:            neutral-0 background, --text-sm, --color-neutral-600
Row hover:      neutral-50 background
Row border:     1px solid neutral-100 (bottom)
Cell padding:   12px 16px
```

Responsive: below `--bp-md` (768px), table transforms to stacked card layout. Each row becomes a card with label: value pairs.

### 7.5 Badge

| Variant | Background | Text | Border |
|---|---|---|---|
| Active | success-50 (#F0FDF4) | success-700 (#15803D) | none |
| Expired | danger-50 (#FEF2F2) | danger-700 (#B91C1C) | none |
| Expires Soon | warning-50 (#FFFBEB) | warning-700 (#B45309) | none |
| Password | info-50 (#EFF6FF) | info-700 (#1D4ED8) | none |
| Neutral | neutral-100 (#F3F4F6) | neutral-600 (#4B5563) | none |

```
Padding:     4px 8px (2px 6px for small)
Radius:      var(--radius-full) (pill shape)
Font:        var(--text-xs), var(--font-medium)
```

All badges include a status icon alongside color to avoid conveying info by color alone.

### 7.6 Modal

```
Overlay:    rgba(0, 0, 0, 0.5)
Width:      min(90vw, 480px)
Radius:     var(--radius-lg) (12px)
Shadow:     var(--shadow-xl)
Padding:    var(--space-6)
```

**Structure:** Header (title + close button), body (content), footer (action buttons, right-aligned).

**Behavior:**
- Focus trapped inside modal while open
- Close on Escape key
- Close on overlay click (except confirmation modals)
- Returns focus to trigger element on close
- `role="dialog"`, `aria-modal="true"`, `aria-labelledby` pointing to title

### 7.7 Toast

```
Position:   top-right (desktop), top-center (mobile)
Width:      min(90vw, 360px)
Radius:     var(--radius-md)
Shadow:     var(--shadow-xl)
Padding:    12px 16px
Duration:   5 seconds (auto-dismiss), persistent for errors
```

| Variant | Left border color | Icon |
|---|---|---|
| Success | success (#22C55E) | check-circle |
| Error | danger (#EF4444) | x-circle |
| Warning | warning (#F59E0B) | alert-triangle |
| Info | info (#3B82F6) | info |

**Behavior:** `role="alert"`, `aria-live="assertive"`. Dismiss on click or close button. Stack vertically (newest on top).

### 7.8 Dropdown / Select

```
Trigger:    Same styling as Button (secondary variant)
Menu:       neutral-0 background, shadow-lg, radius-md
Item:       --text-sm, padding 8px 12px
Item hover: neutral-50 background
Selected:   primary text color + checkmark icon
```

**Keyboard:** Arrow keys navigate, Enter/Space select, Escape closes.

### 7.9 Progress Bar (Quota)

```
Track:      neutral-100 background, radius-full, height 8px
Fill:       gradient based on percentage
Label:      right-aligned, --text-sm
```

| Range | Fill color |
|---|---|
| 0-70% | success (#22C55E) |
| 70-90% | warning (#F59E0B) |
| 90-100% | danger (#EF4444) |

### 7.10 Pagination

```
Button:     ghost variant, 36px square
Active:     primary background, white text
Disabled:   opacity 0.5, no pointer events
```

Format: `<- Prev  1 [2] 3 ... 10  Next ->`

---

## 8. Icons

**Icon set:** [Lucide](https://lucide.dev/) (open source, MIT license).

| Usage | Icon name |
|---|---|
| Copy to clipboard | `copy` |
| External link | `external-link` |
| Create link | `plus` |
| Edit | `pencil` |
| Delete | `trash-2` |
| Statistics | `bar-chart-3` |
| Settings | `settings` |
| User / Profile | `user` |
| Sign out | `log-out` |
| Lock (password) | `lock` |
| Shorten / Link | `link` |
| Filter | `filter` |
| Sort | `arrow-up-down` |
| Success | `check-circle` |
| Error | `x-circle` |
| Warning | `alert-triangle` |
| Info | `info` |
| Back | `arrow-left` |
| Close | `x` |
| Menu (mobile) | `menu` |
| Google logo | Custom SVG (brand) |

**Icon size:** 16px (inline), 20px (buttons), 24px (navigation), 48px (empty states).

**Stroke width:** 2px (default), 1.5px for smaller icons.

---

## 9. Animation and Motion

```css
:root {
  --duration-fast:   150ms;   /* Hover, focus transitions */
  --duration-normal: 250ms;   /* Accordion, dropdown open/close */
  --duration-slow:   350ms;   /* Modal enter/exit, toast slide */
  --easing-default:  cubic-bezier(0.4, 0, 0.2, 1);
  --easing-in:       cubic-bezier(0.4, 0, 1, 1);
  --easing-out:      cubic-bezier(0, 0, 0.2, 1);
}

@media (prefers-reduced-motion: reduce) {
  * {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}
```

---

## 10. Z-Index Scale

| Token | Value | Usage |
|---|---|---|
| `--z-base` | 0 | Default content |
| `--z-dropdown` | 10 | Dropdowns, selects |
| `--z-sticky` | 20 | Sticky header |
| `--z-overlay` | 30 | Modal backdrop |
| `--z-modal` | 40 | Modal content |
| `--z-toast` | 50 | Toast notifications |
