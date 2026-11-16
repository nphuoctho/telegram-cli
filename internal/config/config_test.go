package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func env(t *testing.T, cfg, data string) string {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("TELEGRAM_APP_ID", "")
	t.Setenv("TELEGRAM_APP_HASH", "")
	return home
}

const hash = "0123456789abcdef0123456789abcdef"

func TestPaths(t *testing.T) {
	home := env(t, "", "")
	p, _ := ConfigPath()
	require.Equal(t, filepath.Join(home, ".config", "telegram-cli", "config.toml"), p)
	p, _ = CredentialsPath()
	require.Equal(t, filepath.Join(home, ".local", "share", "telegram-cli", "credentials.json"), p)

	x := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(x, "c"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(x, "d"))
	p, _ = ConfigPath()
	require.Equal(t, filepath.Join(x, "c", "telegram-cli", "config.toml"), p)
	p, _ = CredentialsPath()
	require.Equal(t, filepath.Join(x, "d", "telegram-cli", "credentials.json"), p)

	t.Setenv("XDG_CONFIG_HOME", "relative")
	t.Setenv("XDG_DATA_HOME", "relative")
	p, _ = ConfigPath()
	require.Equal(t, filepath.Join(home, ".config", "telegram-cli", "config.toml"), p)
	p, _ = CredentialsPath()
	require.Equal(t, filepath.Join(home, ".local", "share", "telegram-cli", "credentials.json"), p)
}

func TestConfigDefaultsAndUnknownKey(t *testing.T) {
	env(t, "", "")
	c, err := LoadConfig()
	require.NoError(t, err)
	require.Equal(t, ".", c.DownloadDir)
	d, _ := c.MaxFloodDuration()
	require.Equal(t, 2*time.Minute, d)

	p, _ := ConfigPath()
	require.NoError(t, WriteFileAtomic(p, []byte("download_dir = \"/x\"\nmax_flood_wait = \"5s\"\n")))
	c, err = LoadConfig()
	require.NoError(t, err)
	require.Equal(t, "/x", c.DownloadDir)

	for _, body := range []string{"api_hash = \"x\"\n", "session = 1\n", "typo = 1\n"} {
		require.NoError(t, WriteFileAtomic(p, []byte(body)))
		_, err = LoadConfig()
		var ue *UsageError
		require.ErrorAs(t, err, &ue)
		require.Contains(t, err.Error(), strings.Fields(body)[0])
	}
	require.NoError(t, WriteFileAtomic(p, []byte("max_flood_wait = \"soon\"\n")))
	_, err = LoadConfig()
	require.Error(t, err)
}

func TestCredentialsValidation(t *testing.T) {
	for _, tc := range []struct {
		id   int
		hash string
		ok   bool
	}{
		{1, hash, true},
		{0, hash, false},
		{-1, hash, false},
		{1, hash[:31], false},
		{1, hash + "0", false},
		{1, strings.ToUpper(hash[:20]) + hash[20:] + "A", false},
		{1, strings.Repeat("g", 32), false},
		{1, "", false},
	} {
		err := Credentials{tc.id, tc.hash}.Validate()
		require.Equal(t, tc.ok, err == nil, "%v", tc)
	}
}

func TestResolve(t *testing.T) {
	env(t, "", "")
	_, err := Resolve()
	var nc *NotConfiguredError
	require.ErrorAs(t, err, &nc)

	t.Setenv("TELEGRAM_APP_ID", "7")
	r, err := Resolve()
	require.ErrorAs(t, err, &nc) // hash still missing
	require.Equal(t, "env", r.Source)
	t.Setenv("TELEGRAM_APP_HASH", hash)
	r, err = Resolve()
	require.NoError(t, err)
	require.Equal(t, Resolved{7, hash, "env"}, r)

	t.Setenv("TELEGRAM_APP_ID", "")
	t.Setenv("TELEGRAM_APP_HASH", "")
	_, err = SaveCredentials(Credentials{1, strings.Repeat("a", 32)})
	require.NoError(t, err)
	r, err = Resolve()
	require.NoError(t, err)
	require.Equal(t, "file", r.Source)

	t.Setenv("TELEGRAM_APP_ID", "9") // each var overrides only its own field
	r, _ = Resolve()
	require.Equal(t, Resolved{9, strings.Repeat("a", 32), "mixed"}, r)
	t.Setenv("TELEGRAM_APP_ID", "")
	t.Setenv("TELEGRAM_APP_HASH", hash)
	r, _ = Resolve()
	require.Equal(t, Resolved{1, hash, "mixed"}, r)

	t.Setenv("TELEGRAM_APP_HASH", "bad")
	_, err = Resolve()
	var ue *UsageError
	require.ErrorAs(t, err, &ue)
	require.False(t, errors.Is(err, os.ErrNotExist))
}

func TestPermissionTightening(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no unix modes")
	}
	env(t, "", "")
	dir, _ := DataDir()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	f := filepath.Join(dir, "session.json")
	require.NoError(t, os.WriteFile(f, []byte("{}"), 0o644))
	require.NoError(t, os.Chmod(dir, 0o755))
	warns, err := TightenPermissions()
	require.NoError(t, err)
	require.Len(t, warns, 2)
	st, _ := os.Stat(dir)
	require.Equal(t, os.FileMode(0o700), st.Mode().Perm())
	st, _ = os.Stat(f)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	warns, _ = TightenPermissions()
	require.Empty(t, warns)
}

func TestAtomicWrite(t *testing.T) {
	d := filepath.Join(t.TempDir(), "sub")
	p := filepath.Join(d, "f")
	require.NoError(t, WriteFileAtomic(p, []byte("a")))
	require.NoError(t, WriteFileAtomic(p, []byte("b")))
	got, _ := os.ReadFile(p)
	require.Equal(t, "b", string(got))
	ents, _ := os.ReadDir(d)
	require.Len(t, ents, 1)
	if runtime.GOOS != "windows" {
		st, _ := os.Stat(p)
		require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	}
}
