package release

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io"
	"testing"
)

func TestParseTagReturnsStableReleaseVersion(t *testing.T) {
	version, err := ParseTag("v1.2.3")
	if err != nil {
		t.Fatalf("ParseTag() error = %v", err)
	}
	if got, want := version.String(), "1.2.3"; got != want {
		t.Fatalf("ParseTag() = %q, want %q", got, want)
	}
	if got, want := version.Tag(), "v1.2.3"; got != want {
		t.Fatalf("Version.Tag() = %q, want %q", got, want)
	}
}

func TestParseTagRejectsNonStableVersions(t *testing.T) {
	for _, tag := range []string{"1.2.3", "v1.2", "v1.2.3-rc.1", "v01.2.3", "v1.2.3+build", "v1.2.3/other"} {
		t.Run(tag, func(t *testing.T) {
			if _, err := ParseTag(tag); err == nil {
				t.Fatalf("ParseTag(%q) succeeded, want error", tag)
			}
		})
	}
}

func TestPackageCreatesDeterministicSupportedArchives(t *testing.T) {
	binaries := make(map[string][]byte, len(SupportedTargets()))
	for _, target := range SupportedTargets() {
		binaries[target.ID()] = []byte("binary for " + target.ID())
	}

	first, err := Package("v1.2.3", binaries)
	if err != nil {
		t.Fatalf("Package() error = %v", err)
	}
	second, err := Package("v1.2.3", binaries)
	if err != nil {
		t.Fatalf("second Package() error = %v", err)
	}
	if len(first) != 5 {
		t.Fatalf("Package() returned %d artifacts, want 5", len(first))
	}
	for index := range first {
		if first[index].Name != second[index].Name {
			t.Errorf("artifact %d name = %q, second name = %q", index, first[index].Name, second[index].Name)
		}
		if !bytes.Equal(first[index].Data, second[index].Data) {
			t.Errorf("artifact %q changed between identical package inputs", first[index].Name)
		}
	}

	wantNames := []string{
		"deku_1.2.3_darwin_amd64.tar.gz",
		"deku_1.2.3_darwin_arm64.tar.gz",
		"deku_1.2.3_linux_amd64.tar.gz",
		"deku_1.2.3_linux_arm64.tar.gz",
		"deku_1.2.3_windows_amd64.zip",
	}
	for index, want := range wantNames {
		if first[index].Name != want {
			t.Errorf("artifact %d name = %q, want %q", index, first[index].Name, want)
		}
	}

	for _, artifact := range first {
		target := artifact.Target
		if target.ArchiveExtension == ".zip" {
			assertZipContains(t, artifact.Data, target.ExecutableName, binaries[target.ID()])
			continue
		}
		assertTarGZContains(t, artifact.Data, target.ExecutableName, binaries[target.ID()])
	}
}

func TestPackageRejectsMissingOrUnexpectedTargets(t *testing.T) {
	binaries := map[string][]byte{}
	for _, target := range SupportedTargets() {
		binaries[target.ID()] = []byte("binary")
	}

	delete(binaries, "linux-arm64")
	binaries["freebsd-amd64"] = []byte("binary")
	if _, err := Package("v1.2.3", binaries); err == nil {
		t.Fatal("Package() succeeded with missing and unexpected targets")
	}
}

func TestPackageRejectsEmptyBinary(t *testing.T) {
	binaries := make(map[string][]byte, len(SupportedTargets()))
	for _, target := range SupportedTargets() {
		binaries[target.ID()] = []byte("binary")
	}
	binaries["linux-amd64"] = nil

	if _, err := Package("v1.2.3", binaries); err == nil {
		t.Fatal("Package() succeeded with an empty binary")
	}
}

func TestChecksumsUsesSortedNamesAndSHA256(t *testing.T) {
	artifacts := []Artifact{
		{Name: "b.zip", Data: []byte("two")},
		{Name: "a.tar.gz", Data: []byte("one")},
	}

	got, err := Checksums(artifacts)
	if err != nil {
		t.Fatalf("Checksums() error = %v", err)
	}
	want := "7692c3ad3540bb803c020b3aee66cd8887123234ea0c6e7143c0add73ff431ed  a.tar.gz\n" +
		"3fc4ccfe745870e2c0d99f71f30ff0656c8dedd41cc1d7d3d376b0dbe685e2f3  b.zip\n"
	if string(got) != want {
		t.Fatalf("Checksums() = %q, want %q", got, want)
	}
}

func assertTarGZContains(t *testing.T, data []byte, name string, want []byte) {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip.NewReader() error = %v", err)
	}
	defer func() {
		if err := gz.Close(); err != nil {
			t.Errorf("gzip.Reader.Close() error = %v", err)
		}
	}()
	tarReader := tar.NewReader(gz)
	header, err := tarReader.Next()
	if err != nil {
		t.Fatalf("tar.Reader.Next() error = %v", err)
	}
	if header.Name != name {
		t.Fatalf("tar entry name = %q, want %q", header.Name, name)
	}
	if header.Mode&0o111 == 0 {
		t.Fatalf("tar entry mode = %o, want executable", header.Mode)
	}
	got, err := io.ReadAll(tarReader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("tar entry = %q, want %q", got, want)
	}
}

func assertZipContains(t *testing.T, data []byte, name string, want []byte) {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	if len(archive.File) != 1 || archive.File[0].Name != name {
		t.Fatalf("zip entries = %#v, want one entry named %q", archive.File, name)
	}
	if archive.File[0].Mode()&0o111 == 0 {
		t.Fatalf("zip entry mode = %o, want executable", archive.File[0].Mode())
	}
	file, err := archive.File[0].Open()
	if err != nil {
		t.Fatalf("zip.File.Open() error = %v", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("zip.File.Close() error = %v", err)
		}
	}()
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("zip entry = %q, want %q", got, want)
	}
}
