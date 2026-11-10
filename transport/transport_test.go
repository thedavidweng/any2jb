package transport_test

import (
	"context"
	"testing"

	"github.com/thedavidweng/any2jb/session"
	"github.com/thedavidweng/any2jb/transport"
)

func TestPlatformStubsNeverPretendInputWasSent(t *testing.T) {
	for _, input := range []transport.InputTransport{transport.NewDarwin(), transport.NewLinux(), transport.NewWindows()} {
		err := input.Send(context.Background(), session.Run{}, session.Bootstrap, session.PreparedHost{}, func(session.Progress) { t.Fatal("stub reported progress") })
		if err == nil {
			t.Fatal("unimplemented transport claimed delivery")
		}
		if err := input.Release(context.Background(), session.Run{}); err != nil {
			t.Fatal(err)
		}
	}
}
