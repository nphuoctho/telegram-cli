package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/stretchr/testify/require"
)

const (
	testID   = "1234567"
	testHash = "0123456789abcdef0123456789abcdef"
)

func setup(t *testing.T) (cfgDir, dataDir string) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "c"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "d"))
	t.Setenv("TELEGRAM_APP_ID", "")
	t.Setenv("TELEGRAM_APP_HASH", "")
	return filepath.Join(home, "c", "telegram-cli"), filepath.Join(home, "d", "telegram-cli")
}

func exec(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	root := newRootCmd()
	var out, errB bytes.Buffer
	root.SetArgs(args)
	root.SetIn(strings.NewReader(stdin))
	root.SetOut(&out)
	root.SetErr(&errB)
	code = run(context.Background(), root, &errB)
	require.NotContains(t, out.String()+errB.String(), testHash) // spec 11.6
	return code, out.String(), errB.String()
}

func TestInitHappyPath(t *testing.T) {
	cfgDir, dataDir := setup(t)
	code, out, errS := exec(t, testID+"\n"+testHash+"\n", "init")
	require.Equal(t, 0, code, errS)
	cred := filepath.Join(dataDir, "credentials.json")
	require.Contains(t, out, cred)
	require.Contains(t, out, filepath.Join(cfgDir, "config.toml"))
	require.NotContains(t, out+errS, testHash)

	st, err := os.Stat(dataDir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), st.Mode().Perm())
	st, err = os.Stat(cred)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	c, err := config.LoadCredentials()
	require.NoError(t, err)
	require.Equal(t, config.Credentials{APIID: 1234567, APIHash: testHash}, c)
	got, err := os.ReadFile(filepath.Join(cfgDir, "config.toml"))
	require.NoError(t, err)
	require.Equal(t, config.DefaultConfigTOML, string(got))
}

func TestInitRefusesWithoutForce(t *testing.T) {
	_, dataDir := setup(t)
	exec(t, testID+"\n"+testHash+"\n", "init")
	cred := filepath.Join(dataDir, "credentials.json")
	before, _ := os.ReadFile(cred)

	code, _, errS := exec(t, "42\n"+strings.Repeat("a", 32)+"\n", "init")
	require.Equal(t, 2, code)
	require.Contains(t, errS, "--force")
	after, _ := os.ReadFile(cred)
	require.Equal(t, before, after)

	code, _, _ = exec(t, "42\n"+strings.Repeat("a", 32)+"\n", "init", "--force")
	require.Equal(t, 0, code)
	c, _ := config.LoadCredentials()
	require.Equal(t, 42, c.APIID)
}

func TestInitInvalidInput(t *testing.T) {
	good := strings.Repeat("a", 32)
	for _, in := range []string{
		"abc\n" + good, "0\n" + good, "-1\n" + good,
		"1\n" + good[:31], "1\n" + strings.ToUpper(good), "1\n" + strings.Repeat("g", 32), "",
	} {
		_, dataDir := setup(t)
		code, _, _ := exec(t, in+"\n", "init")
		require.Equal(t, 2, code, in)
		_, err := os.Stat(filepath.Join(dataDir, "credentials.json"))
		require.True(t, os.IsNotExist(err), in)
	}
}

func TestInitJSON(t *testing.T) {
	cfgDir, dataDir := setup(t)
	code, out, errS := exec(t, testID+"\n"+testHash+"\n", "init", "--json")
	require.Equal(t, 0, code, errS)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Equal(t, map[string]any{
		"credentials":    filepath.Join(dataDir, "credentials.json"),
		"config":         filepath.Join(cfgDir, "config.toml"),
		"config_created": true,
	}, got)
	require.NotContains(t, out+errS, testHash)
}

func TestUnknownConfigKey(t *testing.T) {
	cfgDir, _ := setup(t)
	require.NoError(t, os.MkdirAll(cfgDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte("api_hash = \"x\"\n"), 0o600))
	code, _, errS := exec(t, testID+"\n"+testHash+"\n", "init")
	require.Equal(t, 2, code)
	require.Contains(t, errS, "api_hash")
}

func TestVersion(t *testing.T) {
	setup(t)
	code, out, _ := exec(t, "", "--version")
	require.Equal(t, 0, code)
	require.True(t, strings.HasPrefix(out, "telegram-cli "))
}

func TestExitCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
		name string
	}{
		{nil, 0, ""},
		{errors.New("boom"), 1, "ERROR"},
		{&config.UsageError{Msg: "x"}, 2, "USAGE"},
		{fmt.Errorf("wrap: %w", &config.UsageError{Msg: "x"}), 2, "USAGE"},
		{&config.NotConfiguredError{Msg: "x"}, 3, "NOT_CONFIGURED"},
		{context.Canceled, 130, "INTERRUPTED"},
	} {
		code, name := exitCode(tc.err)
		require.Equal(t, tc.code, code)
		require.Equal(t, tc.name, name)
	}
}

func TestJSONErrorShape(t *testing.T) {
	setup(t)
	code, out, errS := exec(t, "abc\n", "init", "--json")
	require.Equal(t, 2, code)
	require.Empty(t, out)
	var got struct {
		Error struct{ Code, Message string }
	}
	require.NoError(t, json.Unmarshal([]byte(errS[strings.Index(errS, "{"):]), &got))
	require.Equal(t, "USAGE", got.Error.Code)
	require.NotEmpty(t, got.Error.Message)
}

func TestUnknownFlagIsUsage(t *testing.T) {
	setup(t)
	code, _, _ := exec(t, "", "init", "--nope")
	require.Equal(t, 2, code)
}

func TestWrongArgCountExitsUsage(t *testing.T) {
	setup(t)
	for _, args := range [][]string{
		{"send"}, {"send", "a", "b", "c"}, {"upload"}, {"download"},
		{"messages", "read"}, {"messages", "search"}, {"chats", "list", "x"},
		{"me", "x"}, {"init", "x"}, {"auth", "status", "x"},
	} {
		code, _, _ := exec(t, "", args...)
		require.Equal(t, 2, code, "%v", args)
	}
}
