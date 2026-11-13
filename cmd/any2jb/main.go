package main

import (
	"log"
	"os"
	"strings"

	"github.com/thedavidweng/any2jb/internal/app"
	"github.com/thedavidweng/any2jb/internal/cli"
	"github.com/thedavidweng/any2jb/internal/ui"
	"github.com/thedavidweng/any2jb/internal/usbgadget"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == usbgadget.HelperFlag {
		os.Exit(usbgadget.RunHelper(os.Args[2:], os.Stdin, os.Stdout))
	}
	if len(os.Args) == 2 && os.Args[1] == "--self-test" {
		if err := app.PackagedSelfTest(); err != nil {
			log.Fatal(err)
		}
		return
	}
	// Arguments select the command line. Older macOS launchers pass a
	// process serial number to an app opened from Finder; that stays the GUI.
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-psn_") {
		os.Exit(cli.Run(os.Args[1:]))
	}
	if err := ui.Run(); err != nil {
		log.Fatal(err)
	}
}
