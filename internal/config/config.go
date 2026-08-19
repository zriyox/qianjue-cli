// Package config implements profile configuration, precedence resolution and
// permission enforcement per cli-contract.md §6/§7. Tokens are never stored in
// config.toml.
package config

import (
	"errors"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"

	"github.com/zriyox/qianjue-cli/internal/clierr"
	"github.com/zriyox/qianjue-cli/internal/output"
)

// Profile is one named API target (cli-contract.md §6.2).
type Profile struct {
	APIBaseURL      string `toml:"api_base_url"`
	Output          string `toml:"output,omitempty"`
	HTTPTimeout     string `toml:"http_timeout,omitempty"`
	TaskWaitTimeout string `toml:"task_wait_timeout,omitempty"`
}

// File is the on-disk config.toml shape.
type File struct {
	CurrentProfile string             `toml:"current_profile,omitempty"`
	Profiles       map[string]Profile `toml:"profiles,omitempty"`
}

// Built-in defaults (cli-contract.md §5/§19).
const (
	DefaultHTTPTimeout     = 30 * time.Second
	DefaultTaskWaitTimeout = 10 * time.Minute
	DefaultProfileName     = "default"

	// DefaultAPIBaseURL is the production endpoint used when neither a flag,
	// QIANJUE_API_BASE_URL, nor the profile sets one. End users therefore need
	// zero configuration; developers targeting dev/test must opt in explicitly
	// (and `config show` marks when this built-in default is in effect, so
	// hitting production by accident is visible rather than silent).
	DefaultAPIBaseURL = "https://api.aiqianjue.com/api/v1"
)

// Load reads config.toml. A missing file yields an empty config; a present
// file must pass permission checks and parse cleanly.
func Load(getenv Getenv) (*File, error) {
	path, err := ConfigPath(getenv)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &File{Profiles: map[string]Profile{}}, nil
	}
	if err != nil {
		return nil, clierr.LocalStorage("读取配置失败: %v", err)
	}
	if err := CheckPerms(path, false); err != nil {
		return nil, err
	}
	var f File
	if err := toml.Unmarshal(raw, &f); err != nil {
		return nil, clierr.LocalStorage("配置文件 %s 解析失败: %v", path, err)
	}
	if f.Profiles == nil {
		f.Profiles = map[string]Profile{}
	}
	return &f, nil
}

// Save writes config.toml atomically with 0700 dir / 0600 file permissions.
func Save(getenv Getenv, f *File) error {
	path, err := ConfigPath(getenv)
	if err != nil {
		return err
	}
	data, err := toml.Marshal(f)
	if err != nil {
		return clierr.LocalStorage("序列化配置失败: %v", err)
	}
	return AtomicWrite(path, data)
}

// AtomicWrite writes data to path via same-directory temp file + fsync +
// rename, creating parent directories with 0700 and the file with 0600.
func AtomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return clierr.LocalStorage("创建目录 %s 失败: %v", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return clierr.LocalStorage("创建临时文件失败: %v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后为 no-op
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return clierr.LocalStorage("设置临时文件权限失败: %v", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return clierr.LocalStorage("写入临时文件失败: %v", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return clierr.LocalStorage("fsync 临时文件失败: %v", err)
	}
	if err := tmp.Close(); err != nil {
		return clierr.LocalStorage("关闭临时文件失败: %v", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return clierr.LocalStorage("原子替换 %s 失败: %v", path, err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// CheckPerms rejects group/other-accessible config or request-log paths on
// Unix (cli-contract.md §6.1). Windows ACLs are out of scope for v1.
func CheckPerms(path string, isDir bool) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return clierr.LocalStorage("检查权限失败: %v", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		want := "0600"
		if isDir {
			want = "0700"
		}
		return clierr.LocalStorage("%s 权限过宽（%04o），请执行: chmod %s %s",
			path, info.Mode().Perm(), want, path)
	}
	return nil
}

// localHosts may use plain HTTP (cli-contract.md §6).
var localHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}

// ValidateBaseURL enforces HTTPS for non-local API base URLs.
func ValidateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return clierr.Usage("无效的 API 根地址: %q", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if h, _, err := net.SplitHostPort(u.Host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if localHosts[host] {
			return nil
		}
		return clierr.Usage("非本机地址必须使用 HTTPS: %q", raw)
	default:
		return clierr.Usage("API 根地址必须是 http(s) URL: %q", raw)
	}
}

// Overrides carries the command-line values that participate in precedence.
type Overrides struct {
	Profile     string
	APIBaseURL  string
	Output      string
	HTTPTimeout string
	Quiet       bool
	NoColor     bool
	Trace       bool
	WaitTimeout string // 命令级 --wait-timeout，参与 task_wait_timeout 优先级
}

// Resolved is the effective configuration after precedence resolution.
type Resolved struct {
	ProfileName string
	APIBaseURL  string
	// APIBaseURLIsDefault reports that APIBaseURL came from DefaultAPIBaseURL
	// (production) rather than from a flag, env var, or profile.
	APIBaseURLIsDefault bool
	Output              output.Format
	HTTPTimeout         time.Duration
	TaskWaitTimeout     time.Duration
	Quiet               bool
	NoColor             bool
	Trace               bool
}

func parseDurationValue(what, v string) (time.Duration, error) {
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, clierr.Usage("无效的 %s 值 %q（示例：30s、10m）", what, v)
	}
	return d, nil
}

// ResolveProfileName applies flag > env > current_profile > "default".
func ResolveProfileName(f *File, flagProfile string, getenv Getenv) string {
	if flagProfile != "" {
		return flagProfile
	}
	if env := getenv("QIANJUE_PROFILE"); env != "" {
		return env
	}
	if f.CurrentProfile != "" {
		return f.CurrentProfile
	}
	return DefaultProfileName
}

// Resolve applies the fixed precedence flag > env > profile > default
// (cli-contract.md §7) and validates the API base URL when present.
func Resolve(f *File, o Overrides, getenv Getenv, stdoutIsTTY bool) (*Resolved, error) {
	name := ResolveProfileName(f, o.Profile, getenv)
	prof := f.Profiles[name]

	r := &Resolved{
		ProfileName:     name,
		HTTPTimeout:     DefaultHTTPTimeout,
		TaskWaitTimeout: DefaultTaskWaitTimeout,
		Quiet:           o.Quiet,
		NoColor:         o.NoColor || getenv("NO_COLOR") != "",
		Trace:           o.Trace,
	}

	pick := func(flagVal, envKey, profVal string) string {
		if flagVal != "" {
			return flagVal
		}
		if env := getenv(envKey); env != "" {
			return env
		}
		return profVal
	}

	r.APIBaseURL = strings.TrimRight(pick(o.APIBaseURL, "QIANJUE_API_BASE_URL", prof.APIBaseURL), "/")
	if r.APIBaseURL == "" {
		r.APIBaseURL = DefaultAPIBaseURL
		r.APIBaseURLIsDefault = true
	}
	if err := ValidateBaseURL(r.APIBaseURL); err != nil {
		return nil, err
	}

	if v := pick(o.Output, "QIANJUE_OUTPUT", prof.Output); v != "" {
		format, err := output.ParseFormat(v)
		if err != nil {
			return nil, err
		}
		r.Output = format
	} else if stdoutIsTTY {
		r.Output = output.FormatTable
	} else {
		r.Output = output.FormatJSON
	}

	if v := pick(o.HTTPTimeout, "QIANJUE_HTTP_TIMEOUT", prof.HTTPTimeout); v != "" {
		d, err := parseDurationValue("http-timeout", v)
		if err != nil {
			return nil, err
		}
		r.HTTPTimeout = d
	}
	if v := pick(o.WaitTimeout, "QIANJUE_TASK_WAIT_TIMEOUT", prof.TaskWaitTimeout); v != "" {
		d, err := parseDurationValue("task-wait-timeout", v)
		if err != nil {
			return nil, err
		}
		r.TaskWaitTimeout = d
	}
	return r, nil
}

// RequireAPIBaseURL is used by network commands.
func (r *Resolved) RequireAPIBaseURL() error {
	if r.APIBaseURL == "" {
		return clierr.Usage("未配置 API 根地址：先执行 qianjue config profile create %s --api-base-url '...'，或使用 --api-base-url / QIANJUE_API_BASE_URL", r.ProfileName)
	}
	return nil
}

// SortedProfileNames returns profile names in stable order for listing.
func SortedProfileNames(f *File) []string {
	names := make([]string, 0, len(f.Profiles))
	for n := range f.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ValidateProfileName keeps names path-safe: they become directory names under
// the state dir and keyring account prefixes.
func ValidateProfileName(name string) error {
	if name == "" {
		return clierr.Usage("Profile 名称不能为空")
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return clierr.Usage("Profile 名称只允许字母、数字、-、_、.（收到 %q）", name)
		}
	}
	if strings.HasPrefix(name, ".") {
		return clierr.Usage("Profile 名称不能以 . 开头（收到 %q）", name)
	}
	return nil
}

// apiBaseURLDisplay marks the built-in production default so a developer can
// tell at a glance that no dev/test endpoint was configured.
func (r *Resolved) apiBaseURLDisplay() string {
	if r.APIBaseURLIsDefault {
		return r.APIBaseURL + "  (内置默认：生产)"
	}
	return r.APIBaseURL
}

// String renders the resolved config for `config show` table output.
func (r *Resolved) TableRows() [][2]string {
	return [][2]string{
		{"Profile", r.ProfileName},
		{"API base URL", r.apiBaseURLDisplay()},
		{"Output", string(r.Output)},
		{"HTTP timeout", r.HTTPTimeout.String()},
		{"Task wait timeout", r.TaskWaitTimeout.String()},
	}
}

func emptyDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
