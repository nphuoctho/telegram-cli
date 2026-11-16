// Package config resolves paths, loads config.toml and credentials.json, and
// writes files atomically with private permissions.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const appDir = "telegram-cli"

// UsageError is a bad argument, flag, or config file (exit 2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// NotConfiguredError means the API ID or hash is missing (exit 3).
type NotConfiguredError struct{ Msg string }

func (e *NotConfiguredError) Error() string { return e.Msg }

func usagef(format string, a ...any) error { return &UsageError{fmt.Sprintf(format, a...)} }

// xdgDir returns the honored XDG dir, falling back to $HOME/<fallback>.
func xdgDir(env, fallback string) (string, error) {
	if v := os.Getenv(env); filepath.IsAbs(v) {
		return filepath.Join(v, appDir), nil
	}
	home := os.Getenv("HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = h
	}
	return filepath.Join(home, fallback, appDir), nil
}

func ConfigDir() (string, error) { return xdgDir("XDG_CONFIG_HOME", ".config") }
func DataDir() (string, error)   { return xdgDir("XDG_DATA_HOME", filepath.Join(".local", "share")) }

func ConfigPath() (string, error) {
	d, err := ConfigDir()
	return filepath.Join(d, "config.toml"), err
}

func CredentialsPath() (string, error) {
	d, err := DataDir()
	return filepath.Join(d, "credentials.json"), err
}

// Config is the content of config.toml.
type Config struct {
	DownloadDir  string `toml:"download_dir"`
	MaxFloodWait string `toml:"max_flood_wait"`
}

const DefaultConfigTOML = `# Directory where ` + "`download`" + ` saves files when --dir is not given. "~" is expanded.
download_dir = "."

# Longest FLOOD_WAIT the CLI will sleep through before giving up (Go duration).
max_flood_wait = "2m"
`

func defaults() Config { return Config{DownloadDir: ".", MaxFloodWait: "2m"} }

// MaxFloodDuration parses MaxFloodWait.
func (c Config) MaxFloodDuration() (time.Duration, error) {
	return time.ParseDuration(c.MaxFloodWait)
}

// LoadConfig reads config.toml; a missing file yields defaults.
func LoadConfig() (Config, error) {
	p, err := ConfigPath()
	if err != nil {
		return Config{}, err
	}
	c := defaults()
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return Config{}, err
	}
	md, err := toml.Decode(string(data), &c)
	if err != nil {
		return Config{}, usagef("invalid %s: %v", p, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		return Config{}, usagef("invalid %s: unknown key %q", p, u[0].String())
	}
	if d, err := c.MaxFloodDuration(); err != nil || d < 0 {
		return Config{}, usagef("invalid %s: max_flood_wait %q is not a duration", p, c.MaxFloodWait)
	}
	if strings.HasPrefix(c.DownloadDir, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			c.DownloadDir = filepath.Join(home, c.DownloadDir[1:])
		}
	}
	return c, nil
}

// Credentials is the content of credentials.json.
type Credentials struct {
	APIID   int    `json:"api_id"`
	APIHash string `json:"api_hash"`
}

var hashRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// ParseAPIID parses and validates an API ID string.
func ParseAPIID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || id <= 0 {
		return 0, usagef("API ID must be a positive integer")
	}
	return id, nil
}

func (c Credentials) Validate() error {
	if c.APIID <= 0 {
		return usagef("API ID must be a positive integer")
	}
	if !hashRE.MatchString(c.APIHash) {
		return usagef("API hash must be 32 lowercase hex characters")
	}
	return nil
}

func LoadCredentials() (Credentials, error) {
	p, err := CredentialsPath()
	if err != nil {
		return Credentials{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Credentials{}, err
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, usagef("invalid %s: %v", p, err)
	}
	if err := c.Validate(); err != nil {
		return Credentials{}, usagef("invalid %s: %v", p, err)
	}
	return c, nil
}

// SaveCredentials validates and atomically writes credentials.json, returning its path.
func SaveCredentials(c Credentials) (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	p, err := CredentialsPath()
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(c)
	return p, WriteFileAtomic(p, append(data, '\n'))
}

// Resolved is the effective API identity after merging env and file.
type Resolved struct {
	APIID   int
	APIHash string
	Source  string // env, file, mixed, none
}

const notConfiguredMsg = `run "telegram-cli init" or set TELEGRAM_APP_ID and TELEGRAM_APP_HASH`

// Resolve merges credentials.json with TELEGRAM_APP_ID / TELEGRAM_APP_HASH.
// It returns a *NotConfiguredError when either value is still missing; the
// partial Resolved (with Source) is returned alongside.
func Resolve() (Resolved, error) {
	var r Resolved
	file, err := LoadCredentials()
	bothEnv := os.Getenv("TELEGRAM_APP_ID") != "" && os.Getenv("TELEGRAM_APP_HASH") != ""
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		if !bothEnv { // env fully overrides the file, so a bad file is irrelevant then
			return r, err
		}
		file, err = Credentials{}, fs.ErrNotExist
	}
	fromFile := err == nil
	r.APIID, r.APIHash = file.APIID, file.APIHash
	envID, envHash := false, false
	if v := os.Getenv("TELEGRAM_APP_ID"); v != "" {
		id, err := ParseAPIID(v)
		if err != nil {
			return r, usagef("TELEGRAM_APP_ID: %v", err)
		}
		r.APIID, envID = id, true
	}
	if v := os.Getenv("TELEGRAM_APP_HASH"); v != "" {
		if !hashRE.MatchString(v) {
			return r, usagef("TELEGRAM_APP_HASH must be 32 lowercase hex characters")
		}
		r.APIHash, envHash = v, true
	}
	switch {
	case !envID && !envHash && !fromFile:
		r.Source = "none"
	case !envID && !envHash:
		r.Source = "file"
	case envID && envHash || !fromFile:
		r.Source = "env"
	default:
		r.Source = "mixed"
	}
	if r.APIID == 0 || r.APIHash == "" {
		return r, &NotConfiguredError{notConfiguredMsg}
	}
	return r, nil
}

// WriteFileAtomic writes data with mode 0600 into a 0700 dir via temp file + rename.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name()) // no-op after a successful rename
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// TightenPermissions makes the data dir 0700 and its files 0600 when group or
// others can access them, returning one warning per fixed path.
func TightenPermissions() ([]string, error) {
	dir, err := DataDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var warns []string
	fix := func(p string, want fs.FileMode) error {
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		if st.Mode().Perm()&0o077 == 0 {
			return nil
		}
		if err := os.Chmod(p, want); err != nil {
			return err
		}
		warns = append(warns, fmt.Sprintf("tightened permissions of %s to %04o", p, want))
		return nil
	}
	if err := fix(dir, 0o700); err != nil {
		return warns, err
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			if err := fix(filepath.Join(dir, e.Name()), 0o600); err != nil {
				return warns, err
			}
		}
	}
	return warns, nil
}
