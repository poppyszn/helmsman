# Frontend Design System & UI Specification

> **Mandatory UI Guide for AI Agents & Developers**: Consult and adhere strictly to this specification when creating or modifying any frontend components, views, or styles in `web/src`.
> This document defines the canonical visual design rules to prevent visual drift, inconsistency, or degraded aesthetic quality ("AI slop").

---

## 1. Design Philosophy

**Helmsman** uses the **Vercel Geist Minimalist** design language:
- **Pure Black Canvas**: The canvas background is pure black (`#000000`). Deep dark surfaces reduce eye strain for engineers working in dark IDEs and terminals all day.
- **Surface Elevation over Borders**: Containers, cards, and toolbars do not use noisy, heavy `1px` border outlines. Elevation and visual grouping are achieved strictly through flat tonal surfaces (`#000000` &rarr; `#050507` &rarr; `#0a0a0d` &rarr; `#121215` &rarr; `#141418`).
- **High-Contrast Typography**: In dark mode, light text on dark surfaces suffers from halation if the font size is too small or contrast is too low. All primary text is crisp white or zinc-100/200; secondary metadata is zinc-400.
- **Engineered Legibility**: Large, authoritative metric counters (`24px font-bold font-mono`) give immediate clarity during incident response or service monitoring.
- **Calm, High-Signal Indicators**: Status is communicated via single 8px indicators with a subtle glow, never through flashing "LIVE" banners or repetitive colorful tags.

---

## 2. Inviolable UI Rules (Anti-Patterns / Never List)

AI agents and developers must strictly follow these rules:

1. **NO Emojis in the UI**:
   - Never use emoji characters (e.g., 🟢, 🔴, ⚠️, 🚀, 📦, 💻) in components, headers, buttons, or badges.
   - Use icons exclusively from `lucide-react` with standard stroke weights (`strokeWidth={1.5}` or `strokeWidth={2}`), or official brand SVGs for third-party auth providers (e.g., GitHub mark).

2. **NO Boxy Borders Around Containers**:
   - Do not wrap cards, telemetry stats, terminal headers, or sidebar rails in `1px solid rgba(255,255,255,0.15)` boxes.
   - Use tonal separation (`bg-[#0a0a0d]`, `bg-[#121215]`) and clean border-bottom rules (`border-b border-zinc-900`) where logical dividers are needed.

3. **NO Sub-12px Microtext**:
   - Never use `text-[9px]`, `text-[10px]`, or `text-[11px]` for readable content, metrics, or interactive buttons.
   - Floor font size for secondary metadata is `12px` (`text-xs`).
   - Base body font size is `14px` or `15px`.
   - Key metric values must be `24px` (`text-2xl font-bold font-mono`).

4. **NO Flashing or Redundant "LIVE" Badges**:
   - Do not clutter cards or headers with flashing beacons, pulsing radar animations, or redundant "LIVE" tags.
   - Use a single, quiet `StatusBadge` dot with ambient glow.

5. **NO Confusing Icon Inversions (State vs Action)**:
   - When streaming logs live: display `<Pause className="size-3.5" />` with label `pause`.
   - When logs are paused: display `<Play className="size-3.5 text-emerald-400" />` with label `resume (N)`.
   - Never show the same icon for both opposing states.

6. **NO Rainbow Card Clutter**:
   - Never assign arbitrary background colors or heavy gradient cards (e.g. purple cards, bright orange cards).
   - Surfaces stay dark and neutral (`#0a0a0d`). Semantic colors (Emerald, Amber, Rose, Indigo) are reserved solely for status dots, badges, or error callouts.

---

## 3. Surface & Elevation Tokens

| Layer | Hex / Utility | Purpose |
|---|---|---|
| **Canvas** | `#000000` / `bg-black` | Root page background, deep dark canvas. |
| **Sidebar Rail** | `#050507` / `bg-[#050507]` | Left collapsible navigation rail. |
| **Elevated Cards** | `#0a0a0d` / `bg-[#0a0a0d]` | Primary containers, metric cards, table containers. |
| **Recessed Terminal** | `#060608` / `bg-[#060608]` | Log viewer viewport, terminal output background. |
| **Interactive Controls** | `#121215` / `bg-[#121215]` | Inputs, search bars, selects, command palette trigger. |
| **Hover / Active States** | `#141418` to `#1c1c22` | Hovered cards, selected rows, active nav links. |
| **Dividers** | `border-zinc-900/60` | Subtle hairline separation (tabs, table rows). |
| **Links** | `text-sky-400`, hover `text-sky-300` | Every link or control that leaves the page or opens more detail (app URLs, GitHub run links, "Show log"), so it reads as clickable on the black canvas. |

---

## 4. Typography Scale

Helmsman uses `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto` for general typography and `ui-monospace, SFMono-Regular, Menlo, Monaco` (`.font-mono-code` / `font-mono`) for system telemetry, IDs, timestamps, and commit hashes.

| Role | Font Size & Classes | Example Usage |
|---|---|---|
| **App Title** | `text-2xl font-bold text-white tracking-tight` (24px) | `billing-api` detail header, catalog title. |
| **Card Title** | `text-base font-semibold text-white tracking-tight` (16px) | Application card name in catalog grid. |
| **Telemetry Metric** | `text-2xl font-bold font-mono text-white tracking-tight` (24px) | `14 ms`, `running`, `7f8c12a`, `online`. |
| **Telemetry Label** | `text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider` (12px) | `HEALTH PROBE`, `CONTAINER`, `REVISION`. |
| **Body / Row Data** | `text-sm text-zinc-200` (14px) | Table cell data, modal explanation text. |
| **Metadata / Badges** | `text-xs font-mono text-zinc-400` (12px) | Timestamps, repository strings, badge pills. |
| **Terminal Output** | `text-[13px] font-mono-code leading-relaxed text-zinc-200` (13px) | Container stdout/stderr log stream lines. |

---

## 5. Motion

Motion makes a change easy to follow. It never decorates, and it never gets in the way of work. The rule of thumb: if a user would notice the animation more than the change, it is too much.

| Where | How | Utility |
|---|---|---|
| **A new page** | Fades up 6px in 220ms. Moving between the environments or tabs of one app keeps the page (the key is the app), so only the panel under the tabs fades. | `animate-page-in` on the `main` wrapper (`Layout.tsx`), `animate-fade-in` on tab panels |
| **Popovers and menus** | Open from their corner in 130ms: a 4px drop and a 2% scale. Close without animation, so a menu never blocks the next click. | `animate-pop-in origin-top-left` |
| **Dialogs and the command palette** | The backdrop fades, the panel pops in. | `animate-fade-in`, `animate-pop-in`, `backdrop:animate-fade-in` |
| **Expanding sections** | The content fades in and the chevron turns (down open, right closed). One chevron that rotates, never two icons that swap. | `animate-fade-in`, `transition-transform duration-150 -rotate-90` |
| **Loading** | A skeleton shaped like the content, not a "loading..." line, so the page does not jump when data arrives. Skeletons pulse once and quietly. | `animate-pulse rounded-xl bg-[#0a0a0d]`, `aria-busy="true"` |
| **State changes** | Colours ease in on hover, focus and status changes. The status dot eases between colours over 500ms. | `transition-colors`, `transition-[background-color,box-shadow] duration-500` |

Rules:

1. **Short.** 120 to 220ms for entrances, 150ms for hover and toggles, 500ms only for a status dot changing colour. Never longer than that.
2. **Only opacity and transform.** Do not animate width, height, top or left, which make the page re-layout and stutter.
3. **No looping or attention-seeking motion.** This follows rule 4 of section 2: no pulsing beacons, bouncing badges or spinners that stay after the work is done. A spinner is allowed only while a request is running.
4. **Respect the user's setting.** `index.css` turns every animation and transition to near zero under `prefers-reduced-motion: reduce`. Do not add motion that bypasses it, and never put a state only in an animation.
5. **Tokens live in `index.css`.** Add a new keyframe there with a `--animate-*` name in `@theme`, not inline in a component. Do not add an animation library.
6. **Do not animate things that refresh on a timer.** A list that refetches every 15 seconds must not fade or slide each time. Animate on mount, not on data.

---

## 6. Standard Component Guidelines

### A. Buttons (`components/ui/button.tsx`)
- **Default size**: `h-9 px-4 text-xs font-medium` (comfortable 36px touch target).
- **Small size (`size="sm"`)**: `h-8 px-3 text-xs`.
- **Large size (`size="lg"`)**: `h-10 px-5 text-sm font-medium`.
- **Variants**:
  - `default`: Pure white background (`bg-white text-black font-semibold hover:bg-zinc-200 shadow-sm`). Used for primary actions (e.g. Restart container, Create agent).
  - `outline`: Dark flat surface (`bg-[#141418] text-zinc-200 hover:bg-[#1c1c22] hover:text-white`).
  - `secondary`: Subdued background (`bg-[#1a1a20] text-zinc-200 hover:bg-[#24242c]`).
  - `ghost`: Transparent (`text-zinc-400 hover:text-white hover:bg-zinc-900/60`).
  - `destructive`: Red tinted warning (`bg-rose-500/15 text-rose-300 hover:bg-rose-500/25 font-semibold`).

### B. Inputs (`components/ui/input.tsx`)
- Sizing: `h-9 px-3 text-xs font-mono-code rounded-md`.
- Colors: `bg-[#121215] text-zinc-100 placeholder:text-zinc-500 focus:outline-none focus:ring-1 focus:ring-zinc-400`.
- Search bars in headers or lists include an icon: `<Search className="size-3.5 text-zinc-500" strokeWidth={1.5} />` placed at `left-3 top-2.5` with input padding `pl-8`.

### C. Status Badges (`components/StatusBadge.tsx`)
- Dot size: `8px` (`h-2 w-2 rounded-full shrink-0`) with subtle ambient glow:
  - `healthy`: `bg-emerald-400 shadow-[0_0_8px_rgba(52,211,153,0.4)]`
  - `degraded`: `bg-amber-400 shadow-[0_0_8px_rgba(251,191,36,0.4)]`
  - `down`: `bg-rose-500 shadow-[0_0_8px_rgba(244,63,94,0.4)]`
  - `unknown`: `bg-zinc-600`
- Label: `text-xs font-mono font-medium`.

### D. Badges & Chips (`components/ui/badge.tsx`)
- Sizing: `px-2 py-0.5 text-xs font-mono tracking-tight select-none rounded-md`.
- Contrast tokens:
  - `default`: `bg-zinc-800/80 text-zinc-300`
  - `prod`: `bg-rose-500/15 text-rose-300 font-semibold`
  - `ok`: `bg-emerald-500/15 text-emerald-300 font-medium`
  - `bad`: `bg-rose-500/15 text-rose-300 font-medium`
  - `warn`: `bg-amber-500/15 text-amber-300 font-medium`
  - `indigo`: `bg-indigo-500/15 text-indigo-300 font-medium`

### E. Telemetry Scorecards (`pages/AppDetail.tsx`)
- Grid layout: `grid grid-cols-2 md:grid-cols-4 gap-4`.
- Card surface: `rounded-xl bg-[#0a0a0d] p-4.5 space-y-1.5 transition-colors hover:bg-[#101014]`.
- Top label: `text-xs font-mono font-medium text-zinc-400 uppercase tracking-wider`.
- Metric row: `text-2xl font-bold font-mono text-white tracking-tight` with secondary units/status in `text-xs font-mono`.

### F. Terminal / Log Viewer (`components/LogViewer.tsx`)
- Outer container: `rounded-xl bg-[#060608] overflow-hidden`.
- Header bar: `px-4 py-2 text-xs font-mono text-zinc-400 bg-[#0c0c0f] border-b border-zinc-900/80 flex items-center justify-between`.
- Terminal body: `h-[420px] overflow-auto p-4 font-mono-code text-[13px] text-zinc-200 leading-relaxed select-text`.
- Line numbers: `text-zinc-600 text-xs w-8 shrink-0 text-right font-mono`.
- Timestamp: `text-zinc-500 text-xs shrink-0 font-mono`.
- Log text: `text-zinc-200` with ANSI code coloring via `AnsiText`.

### G. Tables (`components/ui/table.tsx`)
- Container: `overflow-x-auto rounded-xl bg-[#0a0a0d]`.
- Table header (`Th`): `px-4 py-3 text-xs font-semibold uppercase tracking-wider text-zinc-400 bg-[#060608]`.
- Table cell (`Td`): `px-4 py-3.5 align-middle text-sm text-zinc-200 border-t border-zinc-900/60`.
- Table row (`Tr`): `hover:bg-[#121216] transition-colors`.

---

## 7. Verification & Quality Gates

Before declaring any frontend work complete, agents must verify:

```bash
# 1. Strict TypeScript check (0 errors permitted)
cd web && npm run typecheck

# 2. Strict ESLint check (0 errors, 0 warnings permitted)
cd web && npm run lint
```
