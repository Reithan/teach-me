// Package source implements §13 source resolution: user-configured converters,
// URI fetch, and git HEAD blob lookup. The package depends on internal/cite for
// citation parsing and hashing but never the other way around (no import cycle).
package source

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/reithan/teach-me/internal/cite"
	"github.com/reithan/teach-me/internal/config"
)

// Config holds the source-resolution knobs parsed from the user config file
// and the per-project .tmconfig. .tmconfig keys override the user config.
type Config struct {
	// Converters maps MIME type → command words.
	// Example: "text/html" → ["pandoc", "-f", "html", "-t", "plain"]
	Converters map[string][]string
	// ExtMIME maps a file extension (with leading dot, lower-cased) → MIME type.
	// Example: ".html" → "text/html"
	ExtMIME map[string]string
	// Versions maps program name → required pin (substring match of first version line).
	// Example: "pandoc" → "3.1.11"
	Versions map[string]string
	// VersionCmds maps program name → command words for the version check.
	// When absent for a program, the default is [program, "--version"].
	VersionCmds map[string][]string
	// Git is the git executable path. Empty means git is not configured.
	Git string
	// Repos maps alias → absolute local repo path.
	// Populated from "repo <alias> = <path>" config lines; written by tm repo add.
	Repos map[string]string
	// CacheTTL is the conversion and fetch cache TTL (§13.1).
	// CacheTTLSet is true when cache-ttl was explicitly set in a config file
	// (including when set to 0 to disable). When CacheTTLSet is false the
	// built-in default of 24h applies.
	// Use effectiveCacheTTL() on the Resolver for the resolved value.
	CacheTTL    time.Duration
	CacheTTLSet bool
	// ConfigPath is the user config file path used in error messages.
	ConfigPath string
}

// userConfigPath returns the user-level config path (see config.UserPath).
func userConfigPath() string {
	return config.UserPath()
}

// LoadConfig builds a Config by parsing the user config file and then the
// .tmconfig in the current working directory. .tmconfig keys override the user
// config. Absent config files are not errors; only read/parse failures are.
func LoadConfig() (*Config, error) {
	return LoadConfigPaths(userConfigPath(), ".tmconfig")
}

// LoadConfigPaths builds a Config from explicit file paths.
// userCfgPath is the user-level config (e.g. ~/.config/tm/config).
// tmcfgPath is the per-project .tmconfig override.
// Both files may be absent without error.
func LoadConfigPaths(userCfgPath, tmcfgPath string) (*Config, error) {
	cfg := &Config{
		Converters:  make(map[string][]string),
		ExtMIME:     make(map[string]string),
		Versions:    make(map[string]string),
		VersionCmds: make(map[string][]string),
		Repos:       make(map[string]string),
		ConfigPath:  userCfgPath,
	}

	// Parse user config (absent is OK).
	if err := parseConfigFile(userCfgPath, cfg); err != nil {
		return nil, err
	}

	// Parse .tmconfig (absent is OK; same lookup as state.ResolveFile).
	if err := parseConfigFile(tmcfgPath, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// parseConfigFile parses one config file (key=value lines) and applies the
// recognised source-related keys to cfg. Absent files are not errors.
// A "repo <alias>" key with an alias that does not match [A-Za-z0-9_-]+ is a
// config parse error (spec §13, §4.4).
func parseConfigFile(path string, cfg *Config) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck

	sc := bufio.NewScanner(f)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		rawKey := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if rawKey == "" || val == "" {
			continue
		}
		// Validate repo alias before applying (§4.4: alias matches [A-Za-z0-9_-]+).
		if strings.HasPrefix(rawKey, "repo ") {
			alias := strings.TrimSpace(rawKey[len("repo "):])
			if !cite.ValidAlias(alias) {
				return fmt.Errorf("%s line %d: invalid repo alias %q (must match [A-Za-z0-9_-]+)", path, lineNum, alias)
			}
		}
		// Validate cache-ttl is a valid Go duration before applying.
		if rawKey == "cache-ttl" {
			if _, parseErr := time.ParseDuration(val); parseErr != nil {
				return fmt.Errorf("%s line %d: invalid cache-ttl %q: %v", path, lineNum, val, parseErr)
			}
		}
		applyKey(rawKey, val, cfg)
	}
	return sc.Err()
}

// applyKey applies a single parsed key=value pair to cfg.
func applyKey(key, val string, cfg *Config) {
	switch {
	case key == "git":
		cfg.Git = val

	case strings.HasPrefix(key, "repo "):
		alias := strings.TrimSpace(key[len("repo "):])
		// Alias validity is pre-checked in parseConfigFile; accept here.
		if alias != "" {
			cfg.Repos[alias] = val
		}

	case strings.HasPrefix(key, "convert "):
		mime := strings.TrimSpace(key[len("convert "):])
		if mime != "" {
			cfg.Converters[mime] = splitWords(val)
		}

	case strings.HasPrefix(key, "ext "):
		ext := strings.TrimSpace(key[len("ext "):])
		if ext != "" {
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			cfg.ExtMIME[strings.ToLower(ext)] = val
		}

	case strings.HasPrefix(key, "version-cmd "):
		prog := strings.TrimSpace(key[len("version-cmd "):])
		if prog != "" {
			cfg.VersionCmds[prog] = splitWords(val)
		}

	case strings.HasPrefix(key, "version "):
		prog := strings.TrimSpace(key[len("version "):])
		if prog != "" {
			cfg.Versions[prog] = val
		}

	case key == "cache-ttl":
		// Validated by parseConfigFile before reaching here; ignore parse errors.
		if d, err := time.ParseDuration(val); err == nil {
			cfg.CacheTTL = d
			cfg.CacheTTLSet = true
		}

		// Unknown keys (file=, doc=, TM_* env keys, etc.) are silently ignored.
	}
}

// splitWords splits a command value on whitespace and returns the word slice.
// A value that is all-whitespace returns an empty slice.
func splitWords(val string) []string {
	return strings.Fields(val)
}

// ExtToMIME returns the MIME type for the given file extension (with or without
// leading dot), looking first in cfg.ExtMIME. Returns "" if not found.
func (cfg *Config) ExtToMIME(ext string) string {
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return cfg.ExtMIME[strings.ToLower(ext)]
}

// ConverterFor returns the command words for the converter matching mime, or
// nil when no converter is configured.
func (cfg *Config) ConverterFor(mime string) []string {
	// Strip parameters (e.g. "text/html; charset=utf-8" → "text/html").
	mime = stripMIMEParams(mime)
	return cfg.Converters[mime]
}

// VersionCmd returns the command words for the version check of program.
// Defaults to [program, "--version"] when not configured.
func (cfg *Config) VersionCmd(program string) []string {
	if cmds, ok := cfg.VersionCmds[program]; ok && len(cmds) > 0 {
		return cmds
	}
	return []string{program, "--version"}
}

// stripMIMEParams strips MIME parameters such as charset from a Content-Type value.
// For example, "text/html; charset=utf-8" becomes "text/html".
func stripMIMEParams(mime string) string {
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		return strings.TrimSpace(mime[:i])
	}
	return strings.TrimSpace(mime)
}
