// Command deku-release builds Deku distribution archives for the release workflow.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hsrvms/deku/release"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, output, errorOutput io.Writer) int {
	flags := flag.NewFlagSet("deku-release", flag.ContinueOnError)
	flags.SetOutput(errorOutput)
	tag := flags.String("tag", "", "stable release tag, such as v1.2.3")
	inputDir := flags.String("input-dir", "", "directory containing cross-compiled binaries")
	outputDir := flags.String("output-dir", "", "directory for release archives and SHA256SUMS")
	validateOnly := flags.Bool("validate-only", false, "validate the tag without packaging")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		if err := writeError(errorOutput, "deku-release: unexpected argument %q\n", flags.Arg(0)); err != nil {
			return 1
		}
		return 2
	}
	if *tag == "" {
		if err := writeError(errorOutput, "deku-release: -tag is required\n"); err != nil {
			return 1
		}
		return 2
	}
	if _, err := release.ParseTag(*tag); err != nil {
		if writeErr := writeError(errorOutput, "deku-release: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	if *validateOnly {
		return 0
	}
	if *inputDir == "" || *outputDir == "" {
		if err := writeError(errorOutput, "deku-release: -input-dir and -output-dir are required\n"); err != nil {
			return 1
		}
		return 2
	}

	binaries := make(map[string][]byte, len(release.SupportedTargets()))
	for _, target := range release.SupportedTargets() {
		path := filepath.Join(*inputDir, release.BinaryInputName(target))
		data, err := os.ReadFile(path)
		if err != nil {
			if writeErr := writeError(errorOutput, "deku-release: read %s: %v\n", path, err); writeErr != nil {
				return 1
			}
			return 1
		}
		binaries[target.ID()] = data
	}

	artifacts, err := release.Package(*tag, binaries)
	if err != nil {
		if writeErr := writeError(errorOutput, "deku-release: package artifacts: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	checksums, err := release.Checksums(artifacts)
	if err != nil {
		if writeErr := writeError(errorOutput, "deku-release: create checksums: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	if err := os.MkdirAll(*outputDir, 0o755); err != nil {
		if writeErr := writeError(errorOutput, "deku-release: create output directory: %v\n", err); writeErr != nil {
			return 1
		}
		return 1
	}
	for _, artifact := range artifacts {
		path := filepath.Join(*outputDir, artifact.Name)
		if err := os.WriteFile(path, artifact.Data, 0o644); err != nil {
			if writeErr := writeError(errorOutput, "deku-release: write %s: %v\n", path, err); writeErr != nil {
				return 1
			}
			return 1
		}
		if _, err := fmt.Fprintf(output, "wrote %s\n", path); err != nil {
			return 1
		}
	}
	checksumPath := filepath.Join(*outputDir, "SHA256SUMS")
	if err := os.WriteFile(checksumPath, checksums, 0o644); err != nil {
		if writeErr := writeError(errorOutput, "deku-release: write %s: %v\n", checksumPath, err); writeErr != nil {
			return 1
		}
		return 1
	}
	if _, err := fmt.Fprintf(output, "wrote %s\n", checksumPath); err != nil {
		return 1
	}
	return 0
}

func writeError(output io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(output, format, args...)
	return err
}
