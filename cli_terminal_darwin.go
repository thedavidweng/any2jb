package main

import (
	"os"
	"syscall"
	"unsafe"
)

// cliAttachConsole has nothing to do: Unix programs inherit the shell's terminal.
func cliAttachConsole() {}

func cliTerminal(f *os.File) bool {
	var term syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGETA, uintptr(unsafe.Pointer(&term)))
	return errno == 0
}
