// Package release owns Deku version validation and deterministic distribution packaging.
package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

const DevelopmentVersion = "dev"

var stableTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

// Version is a stable Deku Release Version without the leading v.
type Version string

func (v Version) String() string {
	return string(v)
}

func (v Version) Tag() string {
	if v == "" {
		return ""
	}
	return "v" + v.String()
}

// ParseTag validates a stable release tag and returns its Release Version.
func ParseTag(tag string) (Version, error) {
	matches := stableTagPattern.FindStringSubmatch(tag)
	if matches == nil {
		return "", fmt.Errorf("invalid release tag %q: want vX.Y.Z", tag)
	}
	return Version(strings.TrimPrefix(tag, "v")), nil
}

// Target describes one supported operating-system and architecture pair.
type Target struct {
	OS               string
	Arch             string
	ArchiveExtension string
	ExecutableName   string
}

func (t Target) ID() string {
	return t.OS + "-" + t.Arch
}

func (t Target) archiveName(version Version) string {
	return fmt.Sprintf("deku_%s_%s_%s%s", version, t.OS, t.Arch, t.ArchiveExtension)
}

var supportedTargets = []Target{
	{OS: "darwin", Arch: "amd64", ArchiveExtension: ".tar.gz", ExecutableName: "deku"},
	{OS: "darwin", Arch: "arm64", ArchiveExtension: ".tar.gz", ExecutableName: "deku"},
	{OS: "linux", Arch: "amd64", ArchiveExtension: ".tar.gz", ExecutableName: "deku"},
	{OS: "linux", Arch: "arm64", ArchiveExtension: ".tar.gz", ExecutableName: "deku"},
	{OS: "windows", Arch: "amd64", ArchiveExtension: ".zip", ExecutableName: "deku.exe"},
}

// SupportedTargets returns the initial Supported Platform matrix.
func SupportedTargets() []Target {
	return append([]Target(nil), supportedTargets...)
}

// BinaryInputName returns the expected raw binary filename for a target.
func BinaryInputName(target Target) string {
	return "deku-" + target.ID()
}

// Artifact is one packaged Release Artifact.
type Artifact struct {
	Name   string
	Target Target
	Data   []byte
}

// Package creates one deterministic archive for every Supported Platform.
// binaries is keyed by Target.ID().
func Package(tag string, binaries map[string][]byte) ([]Artifact, error) {
	version, err := ParseTag(tag)
	if err != nil {
		return nil, err
	}
	if len(binaries) != len(supportedTargets) {
		return nil, fmt.Errorf("release requires %d supported binaries, got %d", len(supportedTargets), len(binaries))
	}

	artifacts := make([]Artifact, 0, len(supportedTargets))
	knownTargets := make(map[string]Target, len(supportedTargets))
	for _, target := range supportedTargets {
		knownTargets[target.ID()] = target
	}
	for targetID, binary := range binaries {
		target, ok := knownTargets[targetID]
		if !ok {
			return nil, fmt.Errorf("unsupported release target %q", targetID)
		}
		if len(binary) == 0 {
			return nil, fmt.Errorf("release binary for %q is empty", targetID)
		}
		data, err := archive(target, binary)
		if err != nil {
			return nil, fmt.Errorf("package %s: %w", targetID, err)
		}
		artifacts = append(artifacts, Artifact{
			Name:   target.archiveName(version),
			Target: target,
			Data:   data,
		})
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Name < artifacts[j].Name
	})
	return artifacts, nil
}

// Checksums returns a stable SHA-256 checksum file for the supplied artifacts.
func Checksums(artifacts []Artifact) ([]byte, error) {
	if len(artifacts) == 0 {
		return nil, errors.New("at least one artifact is required")
	}
	ordered := append([]Artifact(nil), artifacts...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Name < ordered[j].Name
	})

	var result bytes.Buffer
	seen := make(map[string]struct{}, len(ordered))
	for _, artifact := range ordered {
		if artifact.Name == "" {
			return nil, errors.New("artifact name is required")
		}
		if _, exists := seen[artifact.Name]; exists {
			return nil, fmt.Errorf("duplicate artifact name %q", artifact.Name)
		}
		seen[artifact.Name] = struct{}{}
		digest := sha256.Sum256(artifact.Data)
		_, _ = fmt.Fprintf(&result, "%s  %s\n", hex.EncodeToString(digest[:]), artifact.Name)
	}
	return result.Bytes(), nil
}

func archive(target Target, binary []byte) ([]byte, error) {
	if target.ArchiveExtension == ".zip" {
		return zipArchive(target.ExecutableName, binary)
	}
	if target.ArchiveExtension == ".tar.gz" {
		return tarGzipArchive(target.ExecutableName, binary)
	}
	return nil, fmt.Errorf("unsupported archive extension %q", target.ArchiveExtension)
}

func tarGzipArchive(name string, binary []byte) ([]byte, error) {
	var result bytes.Buffer
	gzipWriter := gzip.NewWriter(&result)
	epoch := time.Unix(0, 0).UTC()
	gzipWriter.ModTime = epoch
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{
		Name:    name,
		Mode:    0o755,
		Size:    int64(len(binary)),
		ModTime: epoch,
	}); err != nil {
		return nil, fmt.Errorf("write tar header: %w", err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		return nil, fmt.Errorf("write tar entry: %w", err)
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("close tar archive: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close gzip archive: %w", err)
	}
	return result.Bytes(), nil
}

func zipArchive(name string, binary []byte) ([]byte, error) {
	var result bytes.Buffer
	zipWriter := zip.NewWriter(&result)
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Store,
	}
	header.SetMode(0o755)
	header.Modified = time.Unix(0, 0).UTC()
	entry, err := zipWriter.CreateHeader(header)
	if err != nil {
		return nil, fmt.Errorf("create zip entry: %w", err)
	}
	if _, err := entry.Write(binary); err != nil {
		return nil, fmt.Errorf("write zip entry: %w", err)
	}
	if err := zipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close zip archive: %w", err)
	}
	return result.Bytes(), nil
}
