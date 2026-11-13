//go:build !darwin && !linux

package diagnostics

import "errors"

func ownerDead(int) (bool, error) {
	return false, errors.New("stale session owner verification is unavailable on this platform")
}

func recoveryLock(string) (func(), error) {
	return nil, errors.New("session recovery locking is unavailable on this platform")
}
