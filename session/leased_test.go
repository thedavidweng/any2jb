package session

import "testing"

func TestLeasedCoversExactlyTheHeldStages(t *testing.T) {
	leased := map[Stage]bool{RetainedRPC: true, RunningTool: true}
	for _, stage := range []Stage{Idle, Inspecting, NeedsSetup, Ready, BootstrapSetup, PreparingHost, CivSwitchFetching,
		WaitingForInput, Inputting, WaitingForConsole, RunningExploit, Verifying, Verified, Finished, Failed, Indeterminate} {
		if got := (Snapshot{Stage: stage}).Leased(); got != leased[stage] {
			t.Errorf("stage %s: Leased() = %t", stage, got)
		}
	}
	for stage := range leased {
		if !(Snapshot{Stage: stage}).Leased() {
			t.Errorf("stage %s is not held", stage)
		}
	}
}
