package version

import "testing"

func TestCurrentDefaultsToDevelopmentVersion(t *testing.T) {
	if got, want := Current(), "dev"; got != want {
		t.Fatalf("Current() = %q, want %q", got, want)
	}
}
