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
  their text label; the Danger/Warning/Success theme colors only supplement it.
  Each state also has its own symbol: ✓ verified, ○ available but not
  verified, ! requires attention, and ? with a dashed frame for a check the
  user must make personally (“Check this yourself”), which the app never
  observed.
- Disabled controls stay visible and expose their disabled state; the reason a
  control is unavailable is stated in text next to it.

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

- Errors and blockers say what happened and what to do next in full sentences.
  Safety and warning text is never softened; see `docs/localization.md` for
  the translated term list that keeps risk wording consistent.
