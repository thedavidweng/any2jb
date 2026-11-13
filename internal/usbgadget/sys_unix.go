//go:build !windows

package usbgadget

import (
	"errors"
	"os"
	"syscall"
)

func openHidgEndpoint(path string) (Endpoint, error) {
	return os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
}

func isTemporaryHIDError(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == syscall.EAGAIN || errno == syscall.EWOULDBLOCK ||
		errno == syscall.EINTR || errno == syscall.ESHUTDOWN ||
		errno == syscall.ENOTCONN || errno == syscall.EPIPE
}

func configFSGadgetWritable() bool {
	if os.Geteuid() == 0 {
		return true
	}
	const wOK, xOK = 0x2, 0x1
	return syscall.Access("/sys/kernel/config/usb_gadget", wOK|xOK) == nil
}
