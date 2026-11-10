package testconsole_test

import (
	"context"
	"testing"

	"github.com/thedavidweng/any2jb/internal/testconsole"
)

func TestRejectsUnsafePairingBeforeConnecting(t *testing.T) {
	for _, command := range []string{
		"START(39380,127,0,0,1,1,2,3,4)",
		"V=START(39380,127,0,0,1)",
		"V=START(39380,192,0,2,1,1,2,3,4)",
		"V=START(22,127,0,0,1,1,2,3,4)",
		"V=START(39380,127,0,0,1,281474976710656,2,3,4)",
	} {
		t.Run(command, func(t *testing.T) {
			if console, err := testconsole.Connect(context.Background(), command); err == nil {
				console.Close()
				t.Fatal("unsafe pairing accepted")
			}
		})
	}
}
