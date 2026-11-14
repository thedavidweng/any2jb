package civswitch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/thedavidweng/any2jb/internal/session"
)

const maxJavaScriptBytes = 480000

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

// execute refuses a reported payload error and an unconfirmed execution. Loader
// text never becomes part of the error the user sees.
func execute(ctx context.Context, endpoint, code string, deadline time.Duration) error {
	failed := false
	executed, err := loaderCall{deadline: deadline, limit: 1 << 20, line: func(line string) error {
		if strings.HasPrefix(line, "Error in payload:") || strings.HasPrefix(line, "EXCEPTION:") {
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
