// Package skill embeds the user-facing AI assistant skill that ships with the
// qianjue CLI binary. `qianjue skill install` writes the embedded tree into the
// Codex and Claude Code skills directories, so the install works identically on
// Windows, macOS and Linux without make/sh/cp.
//
// The skill is layered on purpose: SKILL.md stays short enough to be read every
// time it is loaded, while the bulky per-capability parameter tables live under
// references/ and are opened only when a task actually needs them.
//
// It is deliberately separate from the contributor guardrail skill
// `zriyo-qianjue-cli`, which lives in the qianjue-parent repository and is never
// shipped with the binary.
package skill

import (
	"embed"
	"io/fs"
)

// Name is the skill identifier and the directory name created under
// ~/.codex/skills/<Name>/ and ~/.claude/skills/<Name>/.
const Name = "qianjue-cli"

//go:embed SKILL.md references
var files embed.FS

// Markdown is the entry document. `skill show` prints exactly this.
var Markdown = mustRead("SKILL.md")

// Files walks every embedded file, yielding paths relative to the skill root
// ("SKILL.md", "references/video-tasks.md", …) so the installer can recreate the
// tree without knowing what is in it.
func Files() (map[string][]byte, error) {
	out := map[string][]byte{}
	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, readErr := files.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		out[path] = b
		return nil
	})
	return out, err
}

func mustRead(name string) []byte {
	b, err := files.ReadFile(name)
	if err != nil {
		panic("embedded skill file missing: " + name)
	}
	return b
}
