package session

// Leased reports whether the session holds the retained RPC lease: a verified
// run that keeps its host alive for one tool at a time.
func (s Snapshot) Leased() bool {
	return s.Stage == RetainedRPC || s.Stage == RunningTool
}
