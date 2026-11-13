package civswitch_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/thedavidweng/any2jb/internal/civswitch"
)

func TestLoaderProbeRequiresNonceAndExecutionConfirmation(t *testing.T) {
	for _, response := range []string{"valid", "open port", "Executed successfully\n", "nonce only"} {
		t.Run(response, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				bytes, err := io.ReadAll(conn)
				if err != nil {
					done <- err
					return
				}
				var marker string
				err = json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimPrefix(string(bytes), "log("), ");")), &marker)
				if err != nil {
					done <- err
					return
				}
				out := response
				if response == "valid" {
					out = marker + "\nExecuted successfully\n"
				}
				if response == "nonce only" {
					out = marker + "\n"
				}
				_, err = io.WriteString(conn, out)
				done <- err
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = civswitch.ProbeLoader(ctx, listener.Addr().String())
			if (err == nil) != (response == "valid") {
				t.Fatalf("response %q: %v", response, err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
