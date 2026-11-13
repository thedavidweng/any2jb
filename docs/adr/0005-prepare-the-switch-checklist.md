# 0005: A confirmed checklist for what the app cannot observe

Status: accepted. Supersedes the stage count and the route-choice location in
[0004](0004-installer-style-first-stage.md) and
[0003](0003-staged-flow-and-scoped-readiness.md).

## Context

Console-side prerequisites decide whether a launch can work, and the app can
observe none of them: the console's system version, which SmileBASIC edition
is installed and how much save space it has, its USB keyboard layout, whether
the console is on wired Ethernet, whether updates are blocked, and which way
power flows through the cable. They were reference text in a disclosure, so a
first-time user could reach Launch having read none of it, and CivSwitch's four
prerequisites were a separate set of check boxes in its own panel.

## Decision

A second stage, **Prepare the Switch**, holds one checklist of the things the
app cannot detect. The wizard is five stages; the heading status label reads
"Stage N of 5".

- The stage opens with the route choice, moved here from the route stage,
  because the checklist shows the selected route's items. The route stage now
  names the chosen route as a read-only row and its Back button returns here
  to change it.
- Items are ordinary check boxes with accessible names, grouped under
  **Network and cable** and **On the Switch**. They are confirmations, never
  observations: they carry no state badge, never enter `Snapshot`, and no
  readiness check or launch gate reads a SmileBASIC item.
  [Accessibility](../accessibility.md) already forbids giving guidance a state
  badge; this keeps observed checks and user claims distinguishable.
- Continue is disabled until every item of the selected route is ticked, with
  the reason in the footer bar, as in stage 1.
- CivSwitch's four prerequisites are items in this same checklist, bound to the
  same `session.CivSwitchPrerequisites` fields, so `StartCivSwitch` still
  receives exactly what was ticked and its validation is unchanged. Its network
  prerequisite is the shared network item, so no requirement is asked twice.
  Only the console IPv4 field stays in the CivSwitch panel.
- Nothing is persisted. The ticks live in `flowUI` for as long as the app runs
  and never reach `preferences.json`: the console can change between runs, so
  every launch asks again. The stage says so in one line.
- Each item's wording comes from the pinned mac2jb README, including the 23.0.1
  reference version, SmileBASIC 4.4.9 full / 4.4.6 demo on internal storage
  with more than 20 MiB free, the US English keyboard layout and the
  unplug-wait-replug fix for cable power direction. Blocking console updates
  links to the 90dns project.

## Consequences and evidence

`internal/ui/prepare_view_test.go` covers each route's item set, the Continue gate and its
reason, that an item is never a status, the 90dns link, the CivSwitch items
reaching `StartCivSwitch` with launch still blocked when one is unticked, and
that nothing survives reopening the app or reaches the preferences file, in
English and Simplified Chinese at 720 and 560 DIP. A ticked checklist is a
user's claim, not evidence: console firmware and game compatibility, LAN
reachability and USB behavior remain **not verified**.
