// Package skill embeds the user-facing AI assistant skill (SKILL.md) that is
// distributed together with the qianjue CLI binary. `qianjue skill install`
// writes this embedded copy into the Codex and Claude Code skills directories,
// so the install works identically on Windows, macOS and Linux without
// make/sh/cp.
//
// SKILL.md here teaches an AI how to *use* the CLI (auth, create, wait,
// idempotent recovery). It is deliberately separate from the contributor
// guardrail skill `zriyo-qianjue-cli`, which lives in the qianjue-parent
// repository and is never shipped with the binary.
package skill

import _ "embed"

// Name is the skill identifier and the directory name created under
// ~/.codex/skills/<Name>/ and ~/.claude/skills/<Name>/.
const Name = "qianjue-cli"

//go:embed SKILL.md
var Markdown []byte
