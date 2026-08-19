package config

import (
	"os"
	"path/filepath"

	"github.com/zriyox/qianjue-cli/internal/clierr"
)

// Getenv abstracts environment lookup so tests can inject XDG overrides.
type Getenv func(string) string

func homeDir(getenv Getenv) (string, error) {
	if h := getenv("HOME"); h != "" {
		return h, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", clierr.LocalStorage("无法确定用户主目录: %v", err)
	}
	return h, nil
}

// ConfigDir is ${XDG_CONFIG_HOME:-$HOME/.config}/qianjue (cli-contract.md §6.1).
func ConfigDir(getenv Getenv) (string, error) {
	if x := getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "qianjue"), nil
	}
	h, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".config", "qianjue"), nil
}

// ConfigPath is the config.toml location.
func ConfigPath(getenv Getenv) (string, error) {
	dir, err := ConfigDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// StateDir is ${XDG_STATE_HOME:-$HOME/.local/state}/qianjue.
func StateDir(getenv Getenv) (string, error) {
	if x := getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "qianjue"), nil
	}
	h, err := homeDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".local", "state", "qianjue"), nil
}

// RequestsDir holds the local idempotency request logs for one profile.
func RequestsDir(getenv Getenv, profile string) (string, error) {
	s, err := StateDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(s, "requests", profile), nil
}

// LocksDir holds cross-process lock files (refresh serialization).
func LocksDir(getenv Getenv) (string, error) {
	s, err := StateDir(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(s, "locks"), nil
}
