package main

// The adapters and controller own synchronized data. Window.Update schedules a
// fresh read on the UI thread, without retaining a Context or build value.
func refreshSession(closed <-chan struct{}, controller, manual <-chan struct{}, update func()) {
	for {
		select {
		case <-closed:
			return
		case <-controller:
		case <-manual:
		}
		select {
		case <-closed:
			return
		default:
			update()
		}
	}
}
