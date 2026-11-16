package tgclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgmock"
	"github.com/stretchr/testify/require"
)

func TestPrepareNeedAuthWithoutSession(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	_, err := prepare(Options{DataDir: dir, NeedAuth: true})
	var ar *AuthRequiredError
	require.True(t, errors.As(err, &ar))
	require.NoDirExists(t, dir) // no side effects before the check
}

func TestPrepareCreatesDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	p, err := prepare(Options{DataDir: dir})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "session.json"), p)
	st, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), st.Mode().Perm())
}

func TestPrepareWithSession(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(SessionPath(dir), []byte("{}"), 0o600))
	_, err := prepare(Options{DataDir: dir, NeedAuth: true})
	require.NoError(t, err)
}

var selfReq = &tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUserSelf{}}}

func TestStartInitsPeersWhenNeedAuth(t *testing.T) {
	m := tgmock.NewRequire(t)
	m.ExpectCall(selfReq).ThenResult(&tg.UserClassVector{Elems: []tg.UserClass{&tg.User{ID: 1, Self: true}}})
	called := false
	err := start(context.Background(), Options{NeedAuth: true}, tg.NewClient(m), nil, func(_ context.Context, s *Session) error {
		called = true
		require.NotNil(t, s.Peers)
		return nil
	})
	require.NoError(t, err)
	require.True(t, called)
}

func TestStartUnauthorizedSkipsCallback(t *testing.T) {
	m := tgmock.NewRequire(t)
	m.ExpectCall(selfReq).ThenUnregistered()
	err := start(context.Background(), Options{NeedAuth: true}, tg.NewClient(m), nil, func(context.Context, *Session) error {
		t.Fatal("callback must not run")
		return nil
	})
	var ar *AuthRequiredError
	require.True(t, errors.As(MapError(err), &ar))
}

func TestStartWithoutNeedAuthMakesNoCall(t *testing.T) {
	m := tgmock.NewRequire(t) // any call fails the test
	require.NoError(t, start(context.Background(), Options{}, tg.NewClient(m), nil, func(context.Context, *Session) error { return nil }))
}

func TestFixMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, os.WriteFile(p, []byte("{}"), 0o644))
	require.NoError(t, fixMode(p))
	st, _ := os.Stat(p)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	require.NoError(t, fixMode(filepath.Join(t.TempDir(), "missing")))
}
