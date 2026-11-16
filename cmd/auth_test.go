package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/gotd/td/tgmock"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

var selfReq = &tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUserSelf{}}}

func alice() *tg.UserClassVector {
	return &tg.UserClassVector{Elems: []tg.UserClass{&tg.User{
		Self: true, ID: 123456789, FirstName: "Alice", LastName: "Liddell", Username: "alice", Phone: "15551234567", AccessHash: 1,
	}}}
}

func useMock(t *testing.T, m *tgmock.Mock) {
	old := runTelegram
	t.Cleanup(func() { runTelegram = old })
	runTelegram = func(ctx context.Context, _ *cobra.Command, _ bool, fn func(context.Context, *tgclient.Session) error) error {
		api := tg.NewClient(m)
		return tgclient.MapError(fn(ctx, &tgclient.Session{API: api, Peers: peers.Options{}.Build(api)}))
	}
}

func creds(t *testing.T) {
	t.Setenv("TELEGRAM_APP_ID", testID)
	t.Setenv("TELEGRAM_APP_HASH", testHash)
}

func TestMeReadable(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectCall(selfReq).ThenResult(alice())
	useMock(t, m)
	code, out, errS := exec(t, "", "me")
	require.Equal(t, 0, code, errS)
	require.Equal(t, "ID:        123456789\nName:      Alice Liddell\nUsername:  @alice\nPhone:     +15551234567\nPremium:   no\n", out)
}

func TestMeJSON(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectCall(selfReq).ThenResult(alice())
	useMock(t, m)
	code, out, _ := exec(t, "", "me", "--json")
	require.Equal(t, 0, code)
	var got map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Equal(t, map[string]any{"id": 123456789.0, "first_name": "Alice", "last_name": "Liddell",
		"username": "alice", "phone": "15551234567", "bot": false, "premium": false}, got)
}

func TestMeUnregistered(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectCall(selfReq).ThenUnregistered()
	useMock(t, m)
	code, _, errS := exec(t, "", "me")
	require.Equal(t, 4, code)
	require.Contains(t, errS, "auth login")
}

func TestStatusAuthorized(t *testing.T) {
	setup(t)
	exec(t, testID+"\n"+testHash+"\n", "init")
	m := tgmock.NewRequire(t)
	m.ExpectCall(selfReq).ThenResult(alice())
	useMock(t, m)
	code, out, _ := exec(t, "", "auth", "status")
	require.Equal(t, 0, code)
	require.Contains(t, out, "Credentials: file (")
	require.Contains(t, out, "Logged in as Alice (@alice, id 123456789)")
}

func TestStatusEnvJSONUnauthorized(t *testing.T) {
	setup(t)
	creds(t)
	m := tgmock.NewRequire(t)
	m.ExpectCall(selfReq).ThenUnregistered()
	useMock(t, m)
	code, out, _ := exec(t, "", "auth", "status", "--json")
	require.Equal(t, 4, code)
	require.JSONEq(t, `{"credentials":"env","authorized":false,"user":null}`, out)
}

func TestStatusNotConfigured(t *testing.T) {
	setup(t)
	code, out, _ := exec(t, "", "auth", "status")
	require.Equal(t, 3, code)
	require.Contains(t, out, "Credentials: none")
}

func TestMeNoSessionNoNetwork(t *testing.T) {
	setup(t)
	creds(t)
	code, _, errS := exec(t, "", "me")
	require.Equal(t, 4, code)
	require.Contains(t, errS, "not logged in")
}

func TestMeNotConfigured(t *testing.T) {
	setup(t)
	code, _, _ := exec(t, "", "me")
	require.Equal(t, 3, code)
}

func session(t *testing.T, dataDir string) (string, string) {
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	s, p := filepath.Join(dataDir, "session.json"), filepath.Join(dataDir, "peers.json")
	require.NoError(t, os.WriteFile(s, []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(p, []byte("{}"), 0o600))
	return s, p
}

func TestLogout(t *testing.T) {
	_, dataDir := setup(t)
	creds(t)
	s, p := session(t, dataDir)
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.AuthLogOutRequest{}).ThenResult(&tg.AuthLoggedOut{})
	useMock(t, m)
	code, out, errS := exec(t, "", "auth", "logout")
	require.Equal(t, 0, code, errS)
	require.Contains(t, out, "Logged out")
	require.NoFileExists(t, s)
	require.NoFileExists(t, p)
}

func TestLogoutRPCFailsStillDeletes(t *testing.T) {
	_, dataDir := setup(t)
	creds(t)
	s, p := session(t, dataDir)
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.AuthLogOutRequest{}).ThenRPCErr(&tgerrBoom)
	useMock(t, m)
	code, _, errS := exec(t, "", "auth", "logout")
	require.Equal(t, 1, code)
	require.Contains(t, errS, "local session removed")
	require.NoFileExists(t, s)
	require.NoFileExists(t, p)
}

func TestLogoutNoSession(t *testing.T) {
	setup(t)
	code, out, _ := exec(t, "", "auth", "logout")
	require.Equal(t, 0, code)
	require.Contains(t, out, "Not logged in")
}

var tgerrBoom = tgerr.Error{Code: 500, Message: "INTERNAL", Type: "INTERNAL"}
