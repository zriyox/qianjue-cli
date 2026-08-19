// Package buildinfo holds build-time metadata injected via -ldflags.
// Runtime code must never fabricate these values (cli-contract.md §3).
package buildinfo

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)
