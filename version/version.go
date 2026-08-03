// Package version provides the build version reported by the Deku CLI.
package version

// Build is replaced for release builds and remains dev for local builds.
var Build = "dev"

// Current returns the version embedded in the running binary.
func Current() string {
	return Build
}
