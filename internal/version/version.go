// Package version reports which build of schedy is running, so "what's
// deployed?" is answerable from the binary or a probe instead of guesswork.
package version

import "runtime/debug"

// Version is stamped by release builds via
//
//	-ldflags "-X github.com/ksamirdev/schedy/internal/version.Version=v1.2.3"
//
// and left empty otherwise.
var Version = ""

// String returns the stamped release version, falling back to the module
// version Go embeds in `go install` builds, then to "dev".
func String() string {
	if Version != "" {
		return Version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
