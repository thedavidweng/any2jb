package parity

// Difference documents an intentional, reviewed behavior difference between
// any2jb and mac2jb/ns2jb so parity harnesses normalize only explicit items.
type Difference struct {
	ID        string
	Layer     string
	Summary   string
	Rationale string
}

// Whitelist is the complete catalog of intentional differences permitted by the
// differential parity suite. Any difference outside this list fails tests.
var Whitelist = []Difference{
	{
		ID:      "hid-redundant-neutral-reports",
		Layer:   "transport/hid",
		Summary: "mac2jb-commands repeats all-zero key-release reports between commands; any2jb Darwin.Send writes a final neutral report before closing keyboard stdin.",
		Rationale: "Boot-keyboard 8-byte HID reports represent full key state rather than edge events. " +
			"Transcript.State() collapses consecutive identical 8-byte state reports while still requiring exact report bytes and nanosecond timestamps on every state transition.",
	},
	{
		ID:      "load-confirm-human-gate",
		Layer:   "transport/hid",
		Summary: "any2jb gates editor focus, LOAD, and SmileBASIC's modal load confirmation on explicit Confirm callbacks instead of mac2jb's timer (--load-confirm-wait-ms, default 0).",
		Rationale: "Without video capture of the Switch screen, a blind timer can press Enter in the wrong UI state. " +
			"With zero human think-time in the test harness, virtual report timestamps match mac2jb's default --load-confirm-wait-ms 0.",
	},
	{
		ID:        "guided-load-confirm-step",
		Layer:     "transport/manual",
		Summary:   "any2jb represents mode preparation (slot/direct) and the extra Enter after LOAD (load-confirm) as explicit interactive steps rather than prose notes.",
		Rationale: "Guided mode steps one user action at a time; every underlying BASIC command string and bootstrap byte stream remains identical to mac2jb.",
	},
	{
		ID:        "workspace-isolation-and-lib-copy",
		Layer:     "engine/workspace",
		Summary:   "any2jb keeps staged upstream sources read-only (0400/0500) and copies engine/{lib,shared,switch2} into an owned <support>/runs/run-* directory instead of writing beside upstream run.cjs.",
		Rationale: "Prevents mutating verified upstream trees and satisfies relative ../../lib/* imports (such as cellenseres) while keeping copiedEngineSha256 and console TCP traffic identical.",
	},
	{
		ID:        "held-stage4-deduplication-and-structured-finish",
		Layer:     "engine/control",
		Summary:   "mac2jb's gui-session.cjs emits stage 4 twice in held mode (matching both pipeline's '4/4 ...' and run.cjs's '4/4 verified...') and stops run.cjs via SIGTERM; any2jb emits stages [1,2,3,4] once and finishes via stdin JSON {'type':'finish'}.",
		Rationale: "Avoids duplicate progress transitions and signal races in cross-platform process supervision while producing the exact same IPC release, cleanup RPC, and 0x8080 zero-byte native.stop() wire sequence.",
	},
	{
		ID:        "izmysh-profile-refusal",
		Layer:     "engine/profile",
		Summary:   "any2jb refuses engine launch for the legacy izmysh profile while preserving its HID and guided command formatting (NS2I, port 39395, 9-argument START).",
		Rationale: "izmysh uses a separate GPL-2.0 server/qualification protocol outside the ns2jb NativeClient adapter.",
	},
}
