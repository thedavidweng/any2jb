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
- Disabled controls stay visible and expose their disabled state; the reason a
  control is unavailable is stated in text next to it.

## Keyboard

- Tab order is construction order, which must match visual order. Do not move
  focus programmatically except to place it in a dedicated input area the user
  explicitly opened (the keyboard forwarding capture).
- The keyboard forwarding capture is the only raw-input control; Escape stops
  forwarding and Tab leaves it, so the panel stays keyboard-navigable.
- Test tab traversal with `tt.Key(0, ui.KeyTab)` and `tt.AccessAll()`, and
  accessible names with `tt.AccessByRole`, as in `accessibility_test.go`.

## Text

- Errors and blockers say what happened and what to do next in full sentences.
  Safety and warning text is never softened; see `docs/localization.md` for
  the translated term list that keeps risk wording consistent.
