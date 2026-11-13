package civswitch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func javaScriptFile(t *testing.T, name, content string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestJavaScriptSendNeedsConsentLiveLoaderAndExecutionConfirmation(t *testing.T) {
	code := "log(\"hi $(touch pwned) '`\");\n"
	t.Run("unconfirmed", func(t *testing.T) {
		f := newPersistenceFixture(t, nil)
		file := javaScriptFile(t, "payload.js", code)
		if err := f.adapter.SendJavaScript(context.Background(), "192.0.2.2", file, false, f.report); err == nil {
			t.Fatal("ran arbitrary JavaScript on the console without explicit confirmation")
		}
		if len(f.loader.sent()) != 0 {
			t.Fatalf("connected before confirmation: %v", f.loader.sent())
		}
		if f.sideEffectReported() {
			t.Fatal("reported a console side effect for a refused action")
		}
	})
	t.Run("not javascript", func(t *testing.T) {
		f := newPersistenceFixture(t, nil)
		for _, file := range []string{javaScriptFile(t, "payload.txt", code), javaScriptFile(t, "empty.js", ""), "payload.js"} {
			if err := f.adapter.SendJavaScript(context.Background(), "192.0.2.2", file, true, f.report); err == nil {
				t.Fatalf("accepted %q", file)
			}
		}
		if f.loader.codeSent() != 0 {
			t.Fatal("sent code the user did not choose")
		}
	})
	t.Run("confirmed", func(t *testing.T) {
		f := newPersistenceFixture(t, nil)
		if err := f.adapter.SendJavaScript(context.Background(), "192.0.2.2", javaScriptFile(t, "payload.js", code), true, f.report); err != nil {
			t.Fatal(err)
		}
		sent := f.sent(t)
		if len(sent) != 1 || sent[0] != code {
			t.Fatalf("the loader did not receive exactly the chosen file: %q", sent)
		}
		if !f.sideEffectReported() {
			t.Fatal("arbitrary code execution was not reported as a console side effect")
		}
	})
	t.Run("loader refuses the payload", func(t *testing.T) {
		hostile := "Error in payload: $(touch pwned)\n" + executionConfirmation + "\n"
		f := newPersistenceFixture(t, func(string) string { return hostile })
		err := f.adapter.SendJavaScript(context.Background(), "192.0.2.2", javaScriptFile(t, "payload.js", code), true, f.report)
		if err == nil {
			t.Fatal("a refused payload was reported as executed")
		}
		if strings.Contains(err.Error(), "touch") {
			t.Fatalf("loader output reached the reported message: %v", err)
		}
	})
	t.Run("execution unconfirmed", func(t *testing.T) {
		f := newPersistenceFixture(t, func(string) string { return "nothing happened\n" })
		if err := f.adapter.SendJavaScript(context.Background(), "192.0.2.2", javaScriptFile(t, "payload.js", code), true, f.report); err == nil {
			t.Fatal("an unconfirmed send was reported as executed")
		}
	})
	t.Run("loader not answering", func(t *testing.T) {
		f := newPersistenceFixture(t, nil)
		f.loader.listener.Close()
		if err := f.adapter.SendJavaScript(context.Background(), "192.0.2.2", javaScriptFile(t, "payload.js", code), true, f.report); err == nil {
			t.Fatal("sent code without validating the loader connection")
		}
	})
}

func TestStagedPayloadNeedsConsentPayloadModeAndAcknowledgedChunks(t *testing.T) {
	code := strings.Repeat("a\"'\n", 30000)
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(code)))
	t.Run("unconfirmed", func(t *testing.T) {
		f := newPersistenceFixture(t, nil)
		if err := f.adapter.StageJavaScript(context.Background(), "192.0.2.2", javaScriptFile(t, "payload.js", code), false, f.report); err == nil {
			t.Fatal("staged a payload without explicit confirmation")
		}
		if len(f.loader.sent()) != 0 {
			t.Fatal("connected before confirmation")
		}
	})
	t.Run("payload mode closed", func(t *testing.T) {
		f := newPersistenceFixture(t, func(string) string {
			return "PAYLOAD_STAGE|ERROR|Enter Control Center Payload Mode first\n" + executionConfirmation + "\n"
		})
		if err := f.adapter.StageJavaScript(context.Background(), "192.0.2.2", javaScriptFile(t, "payload.js", code), true, f.report); err == nil {
			t.Fatal("a rejected staging attempt was reported as staged")
		}
	})
	t.Run("acknowledged chunks", func(t *testing.T) {
		f := newPersistenceFixture(t, nil)
		if err := f.adapter.StageJavaScript(context.Background(), "192.0.2.2", javaScriptFile(t, "payload.js", code), true, f.report); err != nil {
			t.Fatal(err)
		}
		sent := f.sent(t)
		if len(sent) < 4 {
			t.Fatalf("staging did not use begin, chunk and complete envelopes: %d", len(sent))
		}
		if !strings.Contains(sent[0], digest) || !strings.Contains(sent[0], "__civPayloadStage") {
			t.Fatalf("begin envelope: %q", sent[0][:min(200, len(sent[0]))])
		}
		var staged string
		for _, envelope := range sent[1 : len(sent)-1] {
			if strings.Contains(envelope, "\n"+code[:4]) {
				t.Fatal("payload text was interpolated into the envelope unescaped")
			}
			start := strings.Index(envelope, `s.append(`) + len(`s.append(`)
			end := strings.LastIndex(envelope, `, {`)
			var chunk string
			if err := jsonUnmarshalString(envelope[start:end], &chunk); err != nil {
				t.Fatalf("chunk envelope is not JSON-encoded: %v", err)
			}
			staged += chunk
		}
		if staged != code {
			t.Fatalf("staged payload differs from the chosen file: %d of %d bytes", len(staged), len(code))
		}
		if !strings.Contains(sent[len(sent)-1], "s.complete(") || !strings.Contains(sent[len(sent)-1], digest) {
			t.Fatal("staging did not finish with a verified complete envelope")
		}
	})
}
