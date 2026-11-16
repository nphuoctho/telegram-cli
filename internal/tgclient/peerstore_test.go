package tgclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotd/td/telegram/peers"
	"github.com/stretchr/testify/require"
)

func TestPeerStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "peers.json")
	s, err := LoadPeerStore(path)
	require.NoError(t, err)
	require.NoError(t, s.Flush()) // nothing changed: no file
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))

	require.NoError(t, s.Save(ctx, peers.Key{Prefix: "users_", ID: 123}, peers.Value{AccessHash: 456}))
	require.NoError(t, s.Save(ctx, peers.Key{Prefix: "channel_", ID: 789}, peers.Value{AccessHash: -1011}))
	require.NoError(t, s.SavePhone(ctx, "15551234567", peers.Key{Prefix: "users_", ID: 123}))
	require.NoError(t, s.SaveContactsHash(ctx, 99))
	require.NoError(t, s.Flush())

	st, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
	b, _ := os.ReadFile(path)
	require.JSONEq(t, `{"users_123":456,"channel_789":-1011}`, string(b))
	require.NotContains(t, string(b), "15551234567")

	s2, err := LoadPeerStore(path)
	require.NoError(t, err)
	v, ok, _ := s2.Find(ctx, peers.Key{Prefix: "channel_", ID: 789})
	require.True(t, ok)
	require.EqualValues(t, -1011, v.AccessHash)
	_, _, ok, _ = s2.FindPhone(ctx, "15551234567")
	require.False(t, ok)
}

func TestPeerStoreCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "peers.json")
	for _, content := range []string{"{nope", `{"bogus":1}`} {
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, err := LoadPeerStore(path)
		require.ErrorContains(t, err, "corrupt")
	}
}
