package output

import "regexp"

// tokenPattern matches every qianjue credential prefix defined in
// docs/integration/security-and-configuration.md §1. The full token body is
// masked; the prefix is kept so operators can still tell the credential type.
var tokenPattern = regexp.MustCompile(`qj_(at|rt|pat|dc)_[A-Za-z0-9_-]+`)

// Redact masks all qianjue credential material inside s. Every string that
// reaches stdout/stderr must pass through this function (cli-contract.md §26).
func Redact(s string) string {
	return tokenPattern.ReplaceAllString(s, "qj_${1}_***")
}
