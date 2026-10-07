package facedetect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCascadePath(t *testing.T) {
	tempRoot := t.TempDir()
	cascadeDir := filepath.Join(tempRoot, "ai-service", "data")
	if err := os.MkdirAll(cascadeDir, 0o755); err != nil {
		t.Fatalf("mkdir cascade dir: %v", err)
	}
	cascadePath := filepath.Join(cascadeDir, "facefinder")
	if err := os.WriteFile(cascadePath, []byte("test"), 0o600); err != nil {
		t.Fatalf("write cascade file: %v", err)
	}

	workDir := filepath.Join(tempRoot, "ai-service", "cmd", "server")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("mkdir working dir: %v", err)
	}
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}
	defer os.Chdir(oldWd)
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir working dir: %v", err)
	}

	got := resolveCascadePath("data/facefinder")
	if got != cascadePath {
		t.Fatalf("resolveCascadePath() = %q, want %q", got, cascadePath)
	}
}
