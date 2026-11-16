package tgclient

import (
	"context"
	"testing"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/gotd/td/tgmock"
	"github.com/stretchr/testify/require"
)

func newMgr(t *testing.T) (*tgmock.Mock, *tg.Client, *peers.Manager, *PeerStore) {
	m := tgmock.NewRequire(t)
	api := tg.NewClient(m)
	store, err := LoadPeerStore(t.TempDir() + "/peers.json")
	require.NoError(t, err)
	return m, api, peers.Options{Storage: store}.Build(api), store
}

func userReq(id, hash int64) *tg.UsersGetUsersRequest {
	return &tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUser{UserID: id, AccessHash: hash}}}
}

func users(us ...*tg.User) *tg.UserClassVector {
	v := &tg.UserClassVector{}
	for _, u := range us {
		v.Elems = append(v.Elems, u)
	}
	return v
}

func asNotFound(t *testing.T, err error) *PeerNotFoundError {
	var pnf *PeerNotFoundError
	require.ErrorAs(t, err, &pnf)
	return pnf
}

func TestResolveMe(t *testing.T) {
	_, api, mgr, _ := newMgr(t)
	for _, in := range []string{"me", "ME"} {
		p, err := ResolveChat(context.Background(), api, mgr, in)
		require.NoError(t, err)
		require.Equal(t, &tg.InputPeerSelf{}, p)
	}
}

func TestResolveIDsFromStore(t *testing.T) {
	ctx := context.Background()
	m, api, mgr, store := newMgr(t)
	require.NoError(t, store.Save(ctx, peers.Key{Prefix: "users_", ID: 42}, peers.Value{AccessHash: 7}))
	require.NoError(t, store.Save(ctx, peers.Key{Prefix: "channel_", ID: 555}, peers.Value{AccessHash: 8}))

	m.ExpectCall(userReq(42, 7)).ThenResult(users(&tg.User{ID: 42, AccessHash: 7, FirstName: "Bob"}))
	p, err := ResolveChat(ctx, api, mgr, "42")
	require.NoError(t, err)
	require.Equal(t, &tg.InputPeerUser{UserID: 42, AccessHash: 7}, p)

	m.ExpectCall(&tg.ChannelsGetChannelsRequest{ID: []tg.InputChannelClass{&tg.InputChannel{ChannelID: 555, AccessHash: 8}}}).
		ThenResult(&tg.MessagesChats{Chats: []tg.ChatClass{&tg.Channel{ID: 555, AccessHash: 8, Title: "Chan", Megagroup: true, Photo: &tg.ChatPhotoEmpty{}}}})
	p, err = ResolveChat(ctx, api, mgr, "-1000000000555")
	require.NoError(t, err)
	require.Equal(t, &tg.InputPeerChannel{ChannelID: 555, AccessHash: 8}, p)

	m.ExpectCall(&tg.MessagesGetChatsRequest{ID: []int64{9}}).
		ThenResult(&tg.MessagesChats{Chats: []tg.ChatClass{&tg.Chat{ID: 9, Title: "Grp", Photo: &tg.ChatPhotoEmpty{}}}})
	p, err = ResolveChat(ctx, api, mgr, "-9")
	require.NoError(t, err)
	require.Equal(t, &tg.InputPeerChat{ChatID: 9}, p)
}

func TestResolveDialogWalkFallback(t *testing.T) {
	ctx := context.Background()
	m, api, mgr, store := newMgr(t)
	m.ExpectCall(userReq(42, 0)).ThenRPCErr(&tgerr.Error{Code: 400, Type: "USER_ID_INVALID"})
	m.Expect().ThenResult(&tg.MessagesDialogs{
		Dialogs:  []tg.DialogClass{&tg.Dialog{Peer: &tg.PeerUser{UserID: 42}, TopMessage: 1}},
		Messages: []tg.MessageClass{&tg.Message{ID: 1, PeerID: &tg.PeerUser{UserID: 42}, Date: 1}},
		Users:    []tg.UserClass{&tg.User{ID: 42, AccessHash: 77, FirstName: "Bob"}},
	})
	p, err := ResolveChat(ctx, api, mgr, "42")
	require.NoError(t, err)
	require.Equal(t, &tg.InputPeerUser{UserID: 42, AccessHash: 77}, p)
	v, ok, _ := store.Find(ctx, peers.Key{Prefix: "users_", ID: 42})
	require.True(t, ok)
	require.EqualValues(t, 77, v.AccessHash)
}

func TestResolveNeverFound(t *testing.T) {
	m, api, mgr, _ := newMgr(t)
	m.ExpectCall(userReq(99, 0)).ThenRPCErr(&tgerr.Error{Code: 400, Type: "USER_ID_INVALID"})
	m.Expect().ThenResult(&tg.MessagesDialogs{})
	m.ExpectCall(userReq(99, 0)).ThenRPCErr(&tgerr.Error{Code: 400, Type: "USER_ID_INVALID"})
	_, err := ResolveChat(context.Background(), api, mgr, "99")
	require.Equal(t, `chat 99 not found; check "telegram-cli chats list"`, asNotFound(t, err).Error())

	_, err = ResolveChat(context.Background(), api, mgr, "0")
	asNotFound(t, err)
}

func TestResolveUsername(t *testing.T) {
	ctx := context.Background()
	m, api, mgr, _ := newMgr(t)
	m.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "alice"}).ThenResult(&tg.ContactsResolvedPeer{
		Peer:  &tg.PeerUser{UserID: 5},
		Users: []tg.UserClass{&tg.User{ID: 5, AccessHash: 6, Username: "alice"}},
	})
	p, err := ResolveChat(ctx, api, mgr, "@alice")
	require.NoError(t, err)
	require.Equal(t, &tg.InputPeerUser{UserID: 5, AccessHash: 6}, p)

	m.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "nosuchuser123456"}).
		ThenRPCErr(&tgerr.Error{Code: 400, Type: "USERNAME_NOT_OCCUPIED"})
	_, err = ResolveChat(ctx, api, mgr, "@nosuchuser123456")
	require.Contains(t, asNotFound(t, err).Error(), "@nosuchuser123456")
}

func TestResolvePhoneIsNotID(t *testing.T) {
	m, api, mgr, _ := newMgr(t)
	// "+1555..." must go to the phone path (contacts.getContacts), never ResolveTDLibID.
	m.ExpectCall(&tg.ContactsGetContactsRequest{}).ThenResult(&tg.ContactsContacts{
		Users: []tg.UserClass{&tg.User{ID: 8, AccessHash: 9, Phone: "15551234567"}},
	})
	p, err := ResolveChat(context.Background(), api, mgr, "+15551234567")
	require.NoError(t, err)
	require.Equal(t, &tg.InputPeerUser{UserID: 8, AccessHash: 9}, p)
}
