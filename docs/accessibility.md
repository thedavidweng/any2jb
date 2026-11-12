# Accessibility rules

Every view follows these rules; `accessibility_test.go` enforces them on the
main session flow and, screen by screen, on Advanced Tools, SaveKit,
Diagnostics, Driver and CivSwitch. New views extend that table.

## Names and roles

- Every interactive element needs an accessible name. Buttons and checkboxes
  use their visible text; `TextInput`, `TextArea` and `Select` always set
  `.Label(...)`; icon-only controls set `.Label(...)` instead of visible text.
- Session and readiness state is a status live region:
  `.Role(ui.RoleStatus)` with a stable `c.Key(...)`, so a screen reader reads
  state changes as they happen.
- State is never carried by color alone. The readiness states
  (`requires attention`, `available but not verified`, `verified`) always show
  their text label. Routine verified and available states use neutral ink;
  danger color only supplements genuine blockers and failures.
  Each state also has its own symbol: ✓ verified, ○ available but not
  verified, ! requires attention, and ? with a dashed frame for a check the
  user must make personally (“Check this yourself”), which the app never
  observed. State labels are unboxed text, not tinted pills. Observed checks
  use compact rows separated by neutral hairlines; the manual note alone keeps
  its dashed frame. Recovery instructions remain bold and visible.
- Disabled controls stay visible and expose their disabled state; the reason a
  control is unavailable is stated in text next to it. Permanently unsupported
  or distribution-gated tools are named together under their shared reason
  instead of repeating disabled actions; temporary session gates keep the
  controls visible.

## Keyboard

- Tab order is construction order, matching visual order within each stage.
  A stable status heading announces “Stage N of 4” and the stage name. Move
  focus to that heading on a stage transition so the user enters the new scope.
  Other programmatic focus is limited to the explicitly opened keyboard capture.
- Diagnostics and Settings are fixed corner controls outside the scrolling stage
  body. They remain keyboard-reachable from every stage. Stage navigation never
  uses color alone; numbered names, status text and disabled actions carry meaning.
- Each stage's Back and primary actions sit in a bar pinned to the window's
  bottom edge, after the stage body in tab order, so they never scroll away.
- The keyboard forwarding capture is the only raw-input control; Escape stops
  forwarding and Tab leaves it, so the panel stays keyboard-navigable.
- Test tab traversal with `tt.Key(0, ui.KeyTab)` and `tt.AccessAll()`, and
  accessible names with `tt.AccessByRole`, as in `accessibility_test.go`.

## Motion

- Only a stage's arrival animates: a 200 ms ease-out fade with a 6 DIP rise.
  Under Reduce Motion MyGo keeps the fade and drops the movement. Buttons,
  checks, typing and other frequent or keyboard-driven changes never animate.

## Text

- Task sections share a flat neutral surface and 1-DIP separators. The system
  accent remains on primary actions and keyboard focus. Route selection uses
  a neutral fill and hairline edge, with the selected route named above it.
- The system sans and relative heading sizes follow text scaling. Compact
  section spacing does not reduce native control padding or hit targets.
  Theme borders and muted ink retain Increase Contrast adjustments.

- A readiness row reads subject, state, then one next action prefixed with
  “Next:”. Guidance, preferences and reference notes are never shown as rows
  with a state badge; only observed checks are.
- Expert options, reference prerequisites, connecting to an open session and
  the exact verification evidence live in disclosures. A disclosure hides
  detail, never a risk statement or a blocking reason.
- Errors and blockers say what happened and what to do next in full sentences.
  Safety and warning text is never softened; see `docs/localization.md` for
  the translated term list that keeps risk wording consistent.
