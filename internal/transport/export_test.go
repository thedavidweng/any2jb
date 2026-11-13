package transport

import "context"

var EncodeText = encodeText

// SetSleep replaces every keyboard wait and returns a function restoring it.
func SetSleep(f func(context.Context, int) error) (restore func()) {
	previous := sleep
	sleep = f
	return func() { sleep = previous }
}
