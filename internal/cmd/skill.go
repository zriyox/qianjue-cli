package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/skill"
)

// skillHosts are the assistant config directories probed by `skill install`.
// They are only ever written to when they already exist: the CLI must not
// invent an assistant's config layout, and creating those directories on a
// machine that does not use that assistant (or where the path is a symlink or
// a managed mount) can break the user's setup. When no candidate exists the
// AI is expected to place the file itself via `skill show` or `--dir`.
var skillHosts = []string{".codex", ".claude"}

const skillReopenNote = "重新打开 Codex / Claude Code 会话后 skill 才生效。"

// skillInstallData is the JSON payload of `qianjue skill install`.
type skillInstallData struct {
	Name      string   `json:"name"`
	Installed []string `json:"installed"` // SKILL.md paths written
	Skipped   []string `json:"skipped"`   // candidate dirs that do not exist
	Note      string   `json:"note"`
}

func newSkillCommand(app *appContext) *cobra.Command {
	skillCmd := &cobra.Command{Use: "skill", Short: "管理随 CLI 分发的 AI skill"}
	skillCmd.AddCommand(
		newSkillShowCommand(app),
		newSkillInstallCommand(app),
		newSkillPathCommand(app),
	)
	return skillCmd
}

// newSkillShowCommand prints the embedded skill to stdout. This is the primitive
// an AI assistant should prefer: it knows its own skills-directory convention
// and the user's setup far better than this CLI does.
func newSkillShowCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "把内置 AI skill 打印到 stdout（自行决定写到哪个 skills 目录）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Raw markdown, not an envelope: this output is meant to be
			// redirected straight into a SKILL.md file.
			_, err := app.stdout.Write(skill.Markdown)
			return err
		},
	}
}

// newSkillInstallCommand is the convenience path for humans. It writes only
// into assistant skills directories that already exist, and never creates an
// assistant's root config directory.
func newSkillInstallCommand(app *appContext) *cobra.Command {
	var dir string
	c := &cobra.Command{
		Use:   "install",
		Short: "安装内置 AI skill 到已存在的 skills 目录（Windows/macOS/Linux 通用）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var candidates []string
			if dir != "" {
				candidates = []string{dir}
			} else {
				home, err := app.homeDir()
				if err != nil {
					return clierr.New(clierr.KindLocalStorage, fmt.Sprintf("无法定位用户主目录: %v", err))
				}
				for _, host := range skillHosts {
					candidates = append(candidates, filepath.Join(home, host, "skills"))
				}
			}

			var installed, skipped []string
			for _, base := range candidates {
				// --dir is an explicit instruction, so it may be created;
				// probed defaults must already exist.
				if dir == "" {
					if info, err := os.Stat(base); err != nil || !info.IsDir() {
						skipped = append(skipped, base)
						continue
					}
				}
				target := filepath.Join(base, skill.Name)
				if err := os.MkdirAll(target, 0o755); err != nil {
					return clierr.New(clierr.KindLocalStorage, fmt.Sprintf("创建 skill 目录失败 %s: %v", target, err))
				}
				dest := filepath.Join(target, "SKILL.md")
				if err := os.WriteFile(dest, skill.Markdown, 0o644); err != nil {
					return clierr.New(clierr.KindLocalStorage, fmt.Sprintf("写入 skill 失败 %s: %v", dest, err))
				}
				installed = append(installed, dest)
			}

			if len(installed) == 0 {
				return clierr.New(clierr.KindUsage,
					"没有找到已存在的 skills 目录（探测过 "+fmt.Sprint(candidates)+"）。"+
						"请用 qianjue skill install --dir <skills 目录> 指定，"+
						"或用 qianjue skill show > <skills 目录>/"+skill.Name+"/SKILL.md 自行放置。")
			}

			data := skillInstallData{
				Name:      skill.Name,
				Installed: installed,
				Skipped:   skipped,
				Note:      skillReopenNote,
			}
			rows := make([][2]string, 0, len(installed)+1)
			for _, p := range installed {
				rows = append(rows, [2]string{"installed", p})
			}
			rows = append(rows, [2]string{"note", skillReopenNote})
			return app.printer.Success("skill.install", data, map[string]any{}, rows)
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "显式指定 skills 目录（不存在会创建），跳过默认探测")
	return c
}

// newSkillPathCommand reports the probed candidates and whether each exists,
// without writing anything.
func newSkillPathCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "打印候选 skills 目录及其是否存在（不写文件）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := app.homeDir()
			if err != nil {
				return clierr.New(clierr.KindLocalStorage, fmt.Sprintf("无法定位用户主目录: %v", err))
			}
			type candidate struct {
				Host   string `json:"host"`
				Dir    string `json:"dir"`
				Target string `json:"target"`
				Exists bool   `json:"exists"`
			}
			candidates := make([]candidate, 0, len(skillHosts))
			rows := make([][2]string, 0, len(skillHosts))
			for _, host := range skillHosts {
				base := filepath.Join(home, host, "skills")
				info, statErr := os.Stat(base)
				exists := statErr == nil && info.IsDir()
				candidates = append(candidates, candidate{
					Host:   host,
					Dir:    base,
					Target: filepath.Join(base, skill.Name, "SKILL.md"),
					Exists: exists,
				})
				state := "missing"
				if exists {
					state = "exists"
				}
				rows = append(rows, [2]string{host, base + " (" + state + ")"})
			}
			data := map[string]any{"name": skill.Name, "candidates": candidates}
			return app.printer.Success("skill.path", data, map[string]any{}, rows)
		},
	}
}
