package transport_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/thedavidweng/any2jb/internal/manifest"
	"github.com/thedavidweng/any2jb/internal/session"
	"github.com/thedavidweng/any2jb/internal/transport"
)

func TestUSBSaveMatchesEveryProfileEditionWithoutAcceptingOverwrite(t *testing.T) {
	m, err := manifest.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range m.Profiles {
		for _, edition := range profile.SessionProfile().Editions {
			t.Run(profile.ID+"/"+edition, func(t *testing.T) {
				t.Parallel()
				path := filepath.Join(t.TempDir(), "reports")
				confirmations := 0
				options := transport.DarwinOptions{Controller: "usb-drd0", Timing: transport.DefaultTiming(),
					Confirm: func(_ context.Context, _ session.Run, step transport.ConsoleStep) error {
						if step != transport.SaveCommand {
							t.Fatalf("unexpected console step %q", step)
						}
						confirmations++
						return nil
					}}
				options.Timing.GapMS = 1
				d := transport.NewHelperDarwin(options, helperFactory(t, path))
				run := session.Run{ID: "save", Profile: profile.SessionProfile(), Edition: edition}
				if err := d.Send(context.Background(), run, session.Save, session.PreparedHost{}, func(session.Progress) {}); err != nil {
					t.Fatal(err)
				}
				want := "SAVE \"NS2\"\n"
				if profile.ID == "izmysh" {
					want = "SAVE \"NS2I\"\n"
				}
				if edition == "demo" {
					want = "PRGEDIT 0:C$=\"\":FOR I=0 TO PRGSIZE(0)-1:C$=C$+PRGGET$() :NEXT:SAVEV \"TXT:NS2\",C$\n"
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := decodeConsoleReports(t, data); got != want || confirmations != 1 {
					t.Fatalf("save reports %q, want %q with one submission and no overwrite acceptance; confirmations=%d", got, want, confirmations)
				}
			})
		}
	}
}
