package cli

import (
	"os"
	"syscall"
)

var attachConsole = syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")

// The packaged Windows binary is a GUI-subsystem program, which starts with
// no console. Commands attach to the console of the shell that started them.
func cliAttachConsole() {
	if _, err := os.Stdout.Stat(); err == nil {
		return
	}
	const parentProcess = ^uintptr(0)
	if ok, _, _ := attachConsole.Call(parentProcess); ok == 0 {
		return
	}
	if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout, os.Stderr = out, out
	}
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in
	}
}

func cliTerminal(f *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil
}
