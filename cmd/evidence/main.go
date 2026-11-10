// Command evidence validates hardware acceptance logs, applies them to the
// parity ledger's evidence column and prints release evidence reports. It
// performs no hardware operations itself; see docs/hardware-acceptance.md.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/thedavidweng/any2jb/internal/hwevidence"
)

const defaultEvidenceDir = "docs/hardware-evidence"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: evidence <validate|apply|report> [flags] <log.json>...\n" +
			"validate: check evidence logs against the acceptance protocol\n" +
			"apply:    advance parity ledger rows verified by one log (run from the repository root)\n" +
			"report:   print release evidence bullets (SPEC release checklist format)")
	}
	command, rest := args[0], args[1:]
	switch command {
	case "validate":
		paths := rest
		if len(paths) == 0 {
			names, err := filepath.Glob(filepath.Join(defaultEvidenceDir, "*.json"))
			if err != nil {
				return err
			}
			paths = names
		}
		for _, path := range paths {
			if strings.HasPrefix(filepath.Base(path), "TEMPLATE") {
				continue
			}
			if _, err := hwevidence.Load(path); err != nil {
				return err
			}
			fmt.Fprintf(out, "PASS %s\n", path)
		}
		return nil
	case "apply":
		flags := flag.NewFlagSet("apply", flag.ContinueOnError)
		flags.SetOutput(out)
		ledger := flags.String("ledger", "docs/parity-ledger.md", "parity ledger to update")
		if err := flags.Parse(rest); err != nil {
			return err
		}
		if flags.NArg() != 1 {
			return fmt.Errorf("apply needs exactly one evidence log file")
		}
		log, err := hwevidence.Load(flags.Arg(0))
		if err != nil {
			return err
		}
		advanced, err := hwevidence.ApplyLedger(*ledger, log)
		if err != nil {
			return err
		}
		if len(advanced) == 0 {
			fmt.Fprintf(out, "no rows advanced by %s (already hardware-verified or no passing steps)\n", log.Name)
			return nil
		}
		for _, id := range advanced {
			fmt.Fprintf(out, "hardware-verified %s\n", id)
		}
		return nil
	case "report":
		logs, err := loadArgs(rest, nil)
		if err != nil {
			return err
		}
		if len(logs) == 0 {
			fmt.Fprintln(out, "No hardware evidence logs; hardware behavior is not verified.")
			return nil
		}
		hwevidence.Report(out, logs)
		return nil
	}
	return fmt.Errorf("unknown command %q", command)
}

// loadArgs loads the named logs, or every log in the default evidence
// directory when none are named.
func loadArgs(args []string, _ any) ([]*hwevidence.Log, error) {
	if len(args) == 0 {
		return hwevidence.LoadDir(defaultEvidenceDir)
	}
	logs := make([]*hwevidence.Log, 0, len(args))
	for _, path := range args {
		log, err := hwevidence.Load(path)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, nil
}
