package civswitch

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thedavidweng/any2jb/session"
)

const (
	maxJavaScriptBytes     = 480000
	stageChunkCharacters   = 70000
	payloadStageErrorFrame = "PAYLOAD_STAGE|ERROR"
)

// Only a regular, UTF-8 .js file the user chose is accepted. Nothing is read
// from console output, a directory, or a symlinked path.
func readJavaScript(file string) (string, error) {
	if !filepath.IsAbs(file) || strings.ToLower(filepath.Ext(file)) != ".js" {
		return "", errors.New("choose an absolute path to a JavaScript .js file")
	}
	info, err := os.Lstat(file)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxJavaScriptBytes {
		return "", errors.New("choose a regular, nonempty JavaScript file smaller than 480 KB")
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("the JavaScript file must be UTF-8 text")
	}
	return string(data), nil
}

// SendJavaScript runs a file the user chose in the loader's context. That is
// arbitrary code execution on the console: consent is per action, and a
// verified loader never implies it.
func (a *Adapter) SendJavaScript(ctx context.Context, address, file string, confirmedArbitraryCode bool, report session.Report) error {
	if !confirmedArbitraryCode {
		return errors.New("sending a JavaScript file runs arbitrary code in the CivSwitch loader and needs explicit confirmation")
	}
	code, err := readJavaScript(file)
	if err != nil {
		return err
	}
	endpoint, err := a.dependentLoader(ctx, address)
	if err != nil {
		return err
	}
	report(session.Progress{SideEffect: true})
	return execute(ctx, endpoint, code, 2*time.Minute)
}

// StageJavaScript transfers a file into Control Center Payload Mode in
// acknowledged chunks; the console then starts it with A or discards it with B.
// Payload Mode does not restrict that code, so it needs the same consent.
func (a *Adapter) StageJavaScript(ctx context.Context, address, file string, confirmedArbitraryCode bool, report session.Report) error {
	if !confirmedArbitraryCode {
		return errors.New("staging a payload runs arbitrary code once the console starts it and needs explicit confirmation")
	}
	code, err := readJavaScript(file)
	if err != nil {
		return err
	}
	endpoint, err := a.dependentLoader(ctx, address, controlCenterComponent)
	if err != nil {
		return err
	}
	chunks := chunkText(code, stageChunkCharacters)
	digest := hex.EncodeToString(sha256Sum([]byte(code)))
	begin, err := stageEnvelope("s.begin(%s);await log('PAYLOAD_STAGE|BEGIN');", map[string]any{
		"name": filepath.Base(file), "bytes": len(code), "characters": len([]rune(code)),
		"chunks": len(chunks), "sha256": digest, "autostart": false})
	if err != nil {
		return err
	}
	report(session.Progress{Total: len(chunks), SideEffect: true})
	if err := execute(ctx, endpoint, begin, time.Minute); err != nil {
		return err
	}
	received := 0
	for index, chunk := range chunks {
		received += len(chunk)
		envelope, err := stageEnvelope("s.append(%s, %s);await log('PAYLOAD_STAGE|CHUNK');",
			chunk, map[string]any{"index": index + 1, "chunks": len(chunks), "received": received})
		if err != nil {
			return err
		}
		if err := execute(ctx, endpoint, envelope, time.Minute); err != nil {
			return err
		}
		report(session.Progress{Current: index + 1, Total: len(chunks), SideEffect: true})
	}
	complete, err := stageEnvelope("var result=await s.complete(%s);await log('PAYLOAD_STAGE|RESULT');",
		map[string]any{"characters": len([]rune(code)), "sha256": digest, "autostart": false})
	if err != nil {
		return err
	}
	return execute(ctx, endpoint, complete, time.Minute)
}

// Every value a staging envelope carries is JSON-encoded, so payload text is
// never interpolated into console-bound code as it stands.
func stageEnvelope(call string, values ...any) (string, error) {
	encoded := make([]any, 0, len(values))
	for _, value := range values {
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		encoded = append(encoded, string(data))
	}
	return "(async function(){try{var s=window.__civPayloadStage;" +
		"if(!s)throw new Error('Enter Control Center Payload Mode first');" +
		fmt.Sprintf(call, encoded...) +
		"}catch(e){await log('" + payloadStageErrorFrame + "|'+String(e.message||e));throw e;}})();", nil
}

func chunkText(code string, size int) []string {
	runes := []rune(code)
	var chunks []string
	for start := 0; start < len(runes); start += size {
		chunks = append(chunks, string(runes[start:min(start+size, len(runes))]))
	}
	return chunks
}

// execute refuses a reported payload error and an unconfirmed execution. Loader
// text never becomes part of the error the user sees.
func execute(ctx context.Context, endpoint, code string, deadline time.Duration) error {
	failed := false
	executed, err := loaderCall{deadline: deadline, limit: 1 << 20, line: func(line string) error {
		if strings.HasPrefix(line, "Error in payload:") || strings.HasPrefix(line, "EXCEPTION:") ||
			strings.Contains(line, payloadStageErrorFrame) {
			failed = true
		}
		return nil
	}}.send(ctx, endpoint, code)
	if err != nil {
		return err
	}
	if failed {
		return errors.New("the CivSwitch loader reported that the JavaScript failed; check the console")
	}
	if !executed {
		return errors.New("the CivSwitch loader did not confirm JavaScript execution")
	}
	return nil
}
