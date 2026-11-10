package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestCheckRejectsUnexplainedSkip(t *testing.T) {
	var output bytes.Buffer
	err := run(context.Background(), []string{"-reference-root", t.TempDir(), "-skip-payload-reason", " "}, &output)
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("unexplained skip accepted: %v\n%s", err, output.String())
	}
}

func TestCheckRequiresExplicitSource(t *testing.T) {
	var output bytes.Buffer
	if err := run(context.Background(), nil, &output); err == nil {
		t.Fatal("check accepted missing pinned source root")
	}
}
