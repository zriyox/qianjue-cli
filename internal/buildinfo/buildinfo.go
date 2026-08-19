// Package buildinfo holds build-time metadata injected via -ldflags.
// Runtime code must never fabricate these values (cli-contract.md §3).
//
// Release binaries get them from -ldflags (see Makefile / release workflow).
// Binaries produced by `go install module@version` have no ldflags, so the
// values below fall back to the module version and VCS stamps recorded by the
// Go toolchain itself. That is authoritative build metadata, not a fabricated
// value — nothing here invents a version when the toolchain reports none.
package buildinfo

import "runtime/debug"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

func init() {
	fillFromBuildInfo(debug.ReadBuildInfo)
}

// fillFromBuildInfo completes only the fields that -ldflags did not set, so an
// explicit release stamp always wins over the toolchain's record.
func fillFromBuildInfo(read func() (*debug.BuildInfo, bool)) {
	bi, ok := read()
	if !ok || bi == nil {
		return
	}
	if Version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		Version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if Commit == "unknown" && s.Value != "" {
				Commit = shortCommit(s.Value)
			}
		case "vcs.time":
			if BuildDate == "unknown" && s.Value != "" {
				BuildDate = s.Value
			}
		}
	}
}

func shortCommit(full string) string {
	if len(full) > 7 {
		return full[:7]
	}
	return full
}
