package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hsrvms/deku/release"
)

func TestRunPackagesAllSupportedBinaries(t *testing.T) {
	root := t.TempDir()
	inputDir := filepath.Join(root, "build")
	outputDir := filepath.Join(root, "dist")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	for _, target := range release.SupportedTargets() {
		data := []byte("binary for " + target.ID())
		if err := os.WriteFile(filepath.Join(inputDir, release.BinaryInputName(target)), data, 0o755); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", target.ID(), err)
		}
	}

	var stdout, stderr bytes.Buffer
	status := run([]string{"-tag", "v1.2.3", "-input-dir", inputDir, "-output-dir", outputDir}, &stdout, &stderr)
	if status != 0 {
		t.Fatalf("run() status = %d, stderr = %q", status, stderr.String())
	}
	if stdout.Len() == 0 {
		t.Fatal("run() wrote no output, want artifact summary")
	}
	for _, artifact := range []string{
		"deku_1.2.3_darwin_amd64.tar.gz",
		"deku_1.2.3_darwin_arm64.tar.gz",
		"deku_1.2.3_linux_amd64.tar.gz",
		"deku_1.2.3_linux_arm64.tar.gz",
		"deku_1.2.3_windows_amd64.zip",
		"SHA256SUMS",
	} {
		if _, err := os.Stat(filepath.Join(outputDir, artifact)); err != nil {
			t.Errorf("output %s: %v", artifact, err)
		}
	}
}

func TestRunValidateOnlyChecksStableTag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if status := run([]string{"-tag", "v1.2.3", "-validate-only"}, &stdout, &stderr); status != 0 {
		t.Fatalf("valid tag status = %d, stderr = %q", status, stderr.String())
	}
	if status := run([]string{"-tag", "v1.2.3-rc.1", "-validate-only"}, &stdout, &stderr); status == 0 {
		t.Fatal("invalid tag succeeded, want failure")
	}
	if !strings.Contains(stderr.String(), "invalid release tag") {
		t.Fatalf("stderr = %q, want tag validation error", stderr.String())
	}
}
