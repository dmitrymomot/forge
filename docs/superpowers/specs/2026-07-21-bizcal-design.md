# core/bizcal — Business-Calendar Arithmetic

Date: 2026-07-21
Status: approved-pending-review

## Purpose

Stdlib-only, data-free business-calendar arithmetic: consumer-declared schedules (weekly windows, workday capacities, rostered shifts, always-open) plus holiday rules and date exceptions compose into an immutable, goroutine-safe `Calendar` answering instant questions (open? next open? add business duration; business time between) and day questions (working day? count; add working days; scheduled hours) — DST-correct. The substrate for SLA clocks (support app) and time-off/payroll math (HR app).

No storage, no tenancy seam: a `Calendar` is pure computation built per tenant/employee from consumer-owned rows (policy tables, shift plans). Consumers cache instances (immutable ⇒ safe) and rebuild on policy change. No embedded country/holiday tables — holiday data is always consumer-supplied.

Driven by two real consumers:
- **HR / employee portal (multi-tenant):** per-employee schedules stored as `[working weekdays] + hours-per-day` (no clock windows), date exceptions (short days, days off), expected-vs-worked hours for salary, overtime derivation, leave-day counting.
- **Customer support (multi-tenant):** SLA deadline arithmetic over business-hours, 24/7, or rostered-shift coverage; on-call duty rotas for developers (duty-hours totals, was-on-duty checks).

## Core model

Every civil date in the calendar's zone resolves to a **day plan**: a set of open intervals plus a **scheduled capacity** (duration that counts as the day's expected hours). Four schedule sources feed the resolver, layered in fixed precedence:

1. **Base** (choose one):
   - `WithWeekday(wd, windows...)` — per-weekday clock windows (repeatable per weekday). Capacity = sum of window durations.
   - `WithWorkdays(perDay, weekdays...)` — day-granular: each listed weekday is open the whole civil day with capacity `perDay` (e.g. 8h). Matches HR rows exactly; no invented start times.
   - `WithAlwaysOpen()` — every day open 00:00–24:00, capacity 24h per day.
   - (none) — base contributes nothing; shifts alone may define the calendar.
2. **Holiday rules** — `WithRule(r Rule)` (repeatable). A rule yields full days off per year: those dates get empty windows and zero capacity.
3. **Date exceptions** — `WithExceptions(...Exception)`: per-date replacement of the day plan, overriding base and rules. Constructors:
   - `DayOff(date)` — empty plan.
   - `ShortDay(date, d)` — whole-day-open with capacity `d` (workdays-model tenants).
   - `CustomDay(date, windows...)` — replacement clock windows (windows-model tenants).
4. **Shifts** — `WithShifts(...Interval)`: absolute `[start, end)` instants (helper `Shift(start, end)`), may cross midnight and span days. Shifts **add** open time on top of whatever the day resolved to (they win over holidays/exceptions: rostered coverage on a holiday is still coverage). Shift time also adds to the affected dates' capacities, split at civil midnight.

Overlapping windows/shifts are merged (union). Weekly windows must lie within one day (`start < end ≤ 24:00`); recurring cross-midnight coverage is expressed as two windows or as shifts.

### Rules vocabulary

`Rule` is a one-method interface: `Dates(year int) []Date`. Shipped implementations:

- `Fixed{Month, Day}` — e.g. Jan 1.
- `NthWeekday{Month, Weekday, N}` — N ≥ 1 from the start (4th Thursday of November); N ≤ −1 from the end (last Monday of May). N = 0 is a construction error.
- `RuleFunc func(year int) []Date` — escape hatch (Easter and friends).
- `Observed(r Rule) Rule` — shifts any produced Saturday date to Friday and Sunday date to Monday (US-style observed holidays).

All rule outputs are memoized per year inside the calendar (lazily, thread-safe), so cached long-lived calendars never recompute.

## Types

- `Date{Year int, Month time.Month, Day int}` — civil date. `NewDate(y, m, d) (Date, error)` validates; `MustDate` panics; `DateOf(t time.Time) Date` reads t in its own location; `Calendar.DateOf(t)` converts to the calendar's zone first. Methods: `AddDays`, `Weekday()` (pure civil computation, no location), `Before/After/Equal`, `String` (RFC 3339 date).
- `Window` — time-of-day span (minutes-since-midnight start/end). `NewWindow(startMin, endMin)` low-level; `ParseWindow("09:00-17:30") (Window, error)`; `ParseWindows(specs ...string) ([]Window, error)`; `MustWindows(...)` for literals.
- `Interval{Start, End time.Time}` — concrete absolute span, `End` exclusive. Used for shifts and yielded by `WindowsBetween`.
- `Exception` — opaque per-date plan override (constructors above).
- `Calendar` — immutable; `New(loc *time.Location, opts ...Option) (*Calendar, error)`. `loc` must be non-nil.

## Operations

Instant ops (all interpret/return instants in absolute time; results normalized to the calendar's zone):

- `IsOpen(t time.Time) bool`
- `NextOpen(t time.Time) (time.Time, error)` — earliest instant ≥ t that is open (t itself if open). `ErrHorizonExceeded` if none within the scan horizon.
- `Add(t time.Time, d time.Duration) (time.Time, error)` — the instant after `d` of business time elapses from t. `d < 0` walks backward. `Add(t, 0)` = `NextOpen(t)`.
- `Between(a, b time.Time) time.Duration` — business time elapsed in `[a, b)`; signed: `Between(a, b) == -Between(b, a)`.
- `WindowsBetween(from, to time.Time) iter.Seq[Interval]` — the merged concrete open intervals overlapping `[from, to)`, clipped to it, in order. The flexibility primitive for payroll rules (night differentials, per-window rounding); `Add`/`Between` are built on the same resolver.

Day ops (dates in the calendar's zone; ranges half-open `[from, to)`):

- `IsWorkingDay(d Date) bool` — day has non-zero capacity or any open time.
- `WorkingDays(from, to Date) int`
- `AddWorkingDays(d Date, n int) (Date, error)` — n-th working day strictly after (n > 0) / before (n < 0) `d`; n = 0 returns `d` unchanged. `ErrHorizonExceeded` on exhaustion.
- `DayDuration(d Date) time.Duration` — the day's scheduled capacity.
- `ScheduledBetween(from, to Date) time.Duration` — sum of capacities (the salary-base "expected hours").

Salary formulas (consumer-side, documented in doc.go): expected = `ScheduledBetween`; payable in-schedule = `Between(clockIn, clockOut)`; undertime = `DayDuration(day) − worked`; overtime = raw span − in-schedule span (windows model) or `worked − DayDuration(day)` (workdays model).

### Workdays-model semantics

Under `WithWorkdays`, a working day is open the entire civil day but its capacity is `perDay`. `Between` therefore returns wall time clipped to working days (a 10h presence on an 8h day yields 10h — intended, overtime falls out via `worked − DayDuration`). `ScheduledBetween`/`DayDuration` use capacity. SLA-style `Add` on a workdays calendar consumes whole-day wall time; tenants wanting clock-accurate SLA use windows or shifts.

## DST semantics

Windows are wall-clock in the calendar's zone; all arithmetic happens on concrete instants after zone resolution (`time.Date` normalization):

- **Spring forward** (02:00→03:00 skip): a boundary in the gap normalizes forward; the day's real open time shrinks accordingly. `Between`/`Add` measure absolute elapsed time, so an SLA spanning the transition is never over- or under-counted.
- **Fall back** (repeated hour): boundaries resolve to the first occurrence; the repeated hour inside an open window counts twice in absolute time (it really elapsed).
- Workdays-model capacity is a fixed duration, unaffected by DST (an 8h expectation stays 8h); windows-model `DayDuration` reflects the real instant span (DST day may be 7h or 9h) — documented.

## Errors

Sentinels in `errors.go`, `errors.Is`-matchable: `ErrInvalidWindow`, `ErrInvalidDate`, `ErrInvalidWeekday`, `ErrInvalidRule`, `ErrInvalidShift` (end ≤ start), `ErrInvalidCapacity` (negative), `ErrNilLocation`, `ErrNeverOpen` (statically empty calendar: no base, no shifts), `ErrHorizonExceeded`. Options accumulate errors; `New` joins and returns them (standard forge option-error pattern).

Scan horizon: `NextOpen`/`Add`/`AddWorkingDays` search at most `WithHorizon(d)` (default 5 years) of calendar time from the anchor before returning `ErrHorizonExceeded` — guards against rule-funcs that close every day.

## Performance

- Day resolution memoized per year (windows expanded, rules applied, exceptions overlaid) behind a `sync.RWMutex`-guarded map (or `sync.Map` if benchmarks prefer); computed lazily on first touch of a year.
- Hot ops (`IsOpen`, `Between` within a resolved year) target zero allocations.
- `bench_test.go`: construction, `IsOpen`, `NextOpen`, `Add` (short and multi-week), `Between` (day and month span), `ScheduledBetween` (year span), `WindowsBetween` iteration; post-bench optimization pass with before/after numbers in the PR.

## Testing

- Black-box (`bizcal_test`) throughout; white-box only if an internal primitive needs direct pinning.
- DST fixtures: Europe/Kyiv and America/New_York transition days for `Add`/`Between`/`NextOpen`/`DayDuration`.
- Property/fuzz: `Between(a,b) == -Between(b,a)`; `Between(t, Add(t,d)) == d` (when `Add` succeeds and d ≥ 0); `Add` monotonicity; `WorkingDays` consistency with `IsWorkingDay` loop; oracle: minute-granularity brute-force `Between`/`NextOpen` over small ranges vs the fast implementation.
- Concurrency: `-race` test hammering lazy year-cache population from parallel goroutines.
- Layering: shifts over holidays, exceptions over rules, merge of overlapping windows/shifts, cross-midnight shifts splitting capacity across dates.
- Horizon: never-open calendar construction error; rule-func closing everything hits `ErrHorizonExceeded`.

## Anatomy & scope

`core/bizcal`: `doc.go` (runnable example: HR workdays calendar + salary math; SLA add), `options.go`, `errors.go`, `date.go`, `window.go`, `rule.go`, `calendar.go` (+ resolver), `bizcal_test.go` et al., `bench_test.go`. Stdlib-only. Target ~250–850 LOC of implementation.

Anti-scope: pay rates/overtime classes/pay-period aggregation (future `hr/` territory), embedded country holiday tables, storage/loader seams, fractional leave-day policy (consumers derive from `Between`/`DayDuration`), recurring cross-midnight weekly windows (use two windows or shifts), work-hour accounting of actual timesheets (consumer joins their clock data with bizcal outputs).

After shipping: delete the `core/bizcal` entry from `docs/packages.md` (roadmap lists only unbuilt packages) and update the SLA entry's dep tag.
