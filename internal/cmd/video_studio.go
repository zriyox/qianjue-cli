package cmd

import (
	"encoding/json"

	"github.com/spf13/cobra"

	"github.com/zriyox/qianjue-cli/internal/api"
	"github.com/zriyox/qianjue-cli/internal/idem"
)

func newVideoStudioCommand(app *appContext) *cobra.Command {
	c := &cobra.Command{
		Use:   "video-studio",
		Short: "视频剪辑（口播成片 / 智能混剪）",
		Long: "两条链路都要先查模板再提交 —— templateId 必填、后台可配，别硬编码：\n" +
			"  qianjue video-studio koubo templates\n" +
			"  qianjue video-studio koubo submit --request req.json\n" +
			"产出的是视频任务，用 qianjue task wait video <taskId> 等成片。",
	}
	c.AddCommand(newKouboCommand(app), newSmartMixCommand(app))
	return c
}

func newKouboCommand(app *appContext) *cobra.Command {
	k := &cobra.Command{Use: "koubo", Short: "口播成片"}

	templates := &cobra.Command{
		Use:   "templates",
		Short: "口播模板列表（提交前必查，templateId 必填）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.stylingClientOrAuthed()
			if err != nil {
				return err
			}
			raw, err := client.ListKouboTemplates(cmd.Context())
			if err != nil {
				return err
			}
			return app.printer.Success("video-studio.koubo.templates", raw, app.meta, templateRows(raw))
		},
	}

	var requestPath string
	submit := &cobra.Command{
		Use:   "submit",
		Short: "提交口播成片任务",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, client, err := app.readRequestAndClient(requestPath)
			if err != nil {
				return err
			}
			raw, err := client.SubmitKouboTask(cmd.Context(), body)
			if err != nil {
				return err
			}
			return app.printer.Success("video-studio.koubo.submit", raw, app.meta, videoTaskSubmitRows(raw))
		},
	}
	submit.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（必填）")
	_ = submit.MarkFlagRequired("request")

	k.AddCommand(templates, submit)
	return k
}

func newSmartMixCommand(app *appContext) *cobra.Command {
	s := &cobra.Command{Use: "smart-mix", Short: "智能混剪"}

	templates := &cobra.Command{
		Use:   "templates",
		Short: "混剪模板列表",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.stylingClientOrAuthed()
			if err != nil {
				return err
			}
			raw, err := client.ListSmartMixTemplates(cmd.Context())
			if err != nil {
				return err
			}
			return app.printer.Success("video-studio.smart-mix.templates", raw, app.meta, templateRows(raw))
		},
	}

	voices := &cobra.Command{
		Use:   "voices",
		Short: "混剪可用音色（voiceCode 从这里取）",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := app.stylingClientOrAuthed()
			if err != nil {
				return err
			}
			raw, err := client.ListSmartMixVoices(cmd.Context())
			if err != nil {
				return err
			}
			return app.printer.Success("video-studio.smart-mix.voices", raw, app.meta, templateRows(raw))
		},
	}

	var requestPath string
	submit := &cobra.Command{
		Use:   "submit",
		Short: "提交智能混剪任务",
		Long:  "草稿生成费与云渲染导出费分别冻结、各阶段独立结算。",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			body, client, err := app.readRequestAndClient(requestPath)
			if err != nil {
				return err
			}
			raw, err := client.SubmitSmartMixTask(cmd.Context(), body)
			if err != nil {
				return err
			}
			return app.printer.Success("video-studio.smart-mix.submit", raw, app.meta, videoTaskSubmitRows(raw))
		},
	}
	submit.Flags().StringVar(&requestPath, "request", "", "请求 JSON 文件路径；- 表示 stdin（必填）")
	_ = submit.MarkFlagRequired("request")

	s.AddCommand(templates, voices, submit)
	return s
}

// readRequestAndClient reads the request document and returns an authed client,
// the pair every video-studio submit needs.
func (a *appContext) readRequestAndClient(requestPath string) ([]byte, *api.Client, error) {
	resolved, err := a.resolveConfig()
	if err != nil {
		return nil, nil, err
	}
	if err := resolved.RequireAPIBaseURL(); err != nil {
		return nil, nil, err
	}
	raw, err := readRequestDocument(a, requestPath)
	if err != nil {
		return nil, nil, err
	}
	if err := idem.CheckRequestObject(raw); err != nil {
		return nil, nil, err
	}
	client, err := a.newAuthedClient(resolved)
	if err != nil {
		return nil, nil, err
	}
	return raw, client, nil
}

// stylingClientOrAuthed returns an authed client for plain read commands.
func (a *appContext) stylingClientOrAuthed() (*api.Client, error) {
	resolved, err := a.resolveConfig()
	if err != nil {
		return nil, err
	}
	if err := resolved.RequireAPIBaseURL(); err != nil {
		return nil, err
	}
	return a.newAuthedClient(resolved)
}

// templateRows renders id/name pairs from a list response.
func templateRows(raw json.RawMessage) [][2]string {
	var items []map[string]any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	rows := make([][2]string, 0, len(items))
	for _, it := range items {
		id := firstString(it, "templateId", "id", "code", "voiceCode")
		name := firstString(it, "name", "templateName", "displayName", "title")
		if id == "" && name == "" {
			continue
		}
		rows = append(rows, [2]string{id, name})
	}
	if len(rows) == 0 {
		rows = append(rows, [2]string{"结果", "暂无可用项"})
	}
	return rows
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// videoTaskSubmitRows surfaces the task id so the caller knows what to wait on.
func videoTaskSubmitRows(raw json.RawMessage) [][2]string {
	var probe struct {
		TaskID  json.Number `json:"taskId"`
		ID      json.Number `json:"id"`
		Status  string      `json:"status"`
		BatchID json.Number `json:"batchId"`
	}
	_ = json.Unmarshal(raw, &probe)
	id := probe.TaskID.String()
	if id == "" || id == "0" {
		id = probe.ID.String()
	}
	rows := [][2]string{{"Task ID", id}}
	if probe.Status != "" {
		rows = append(rows, [2]string{"Status", probe.Status})
	}
	rows = append(rows, [2]string{"下一步", "qianjue task wait video " + id})
	return rows
}
