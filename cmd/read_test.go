package cmd

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/gotd/td/tgmock"
	"github.com/stretchr/testify/require"
)

func init() { time.Local = time.UTC }

func textMsg(id int, text string) *tg.Message {
	return &tg.Message{ID: id, Date: 1763276525, Message: text, PeerID: &tg.PeerUser{UserID: 123456789},
		FromID: &tg.PeerUser{UserID: 123456789}, Out: true}
}

func msgs(ms ...*tg.Message) *tg.MessagesMessages {
	r := &tg.MessagesMessages{Users: []tg.UserClass{alice().Elems[0]}}
	for _, m := range ms {
		r.Messages = append(r.Messages, m)
	}
	return r
}

func expectHistory(t *testing.T, m *tgmock.Mock, check func(*tg.MessagesGetHistoryRequest), res tg.MessagesMessagesClass) {
	m.ExpectFunc(func(b bin.Encoder) {
		req, ok := b.(*tg.MessagesGetHistoryRequest)
		require.True(t, ok, "got %T", b)
		check(req)
	}).ThenResult(res)
}

func TestMessagesReadOldestFirstLimitBefore(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	expectHistory(t, m, func(r *tg.MessagesGetHistoryRequest) {
		require.Equal(t, &tg.InputPeerSelf{}, r.Peer)
		require.Equal(t, 10, r.OffsetID)
		require.Equal(t, 2, r.Limit)
	}, msgs(textMsg(9, "second"), textMsg(8, "first")))
	useMock(t, m)
	code, out, errS := exec(t, "", "messages", "read", "me", "--limit", "2", "--before", "10")
	require.Equal(t, 0, code, errS)
	require.Equal(t, "#8  2025-11-16 07:02  Alice Liddell (@alice)\n  first\n#9  2025-11-16 07:02  Alice Liddell (@alice)\n  second\n", out)
}

func TestMessagesReadJSON(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	expectHistory(t, m, func(*tg.MessagesGetHistoryRequest) {}, msgs(textMsg(2, "b"), textMsg(1, "a")))
	useMock(t, m)
	code, out, errS := exec(t, "", "messages", "read", "me", "--json")
	require.Equal(t, 0, code, errS)
	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 2)
	require.EqualValues(t, 1, got[0]["id"])
	require.EqualValues(t, 123456789, got[0]["chat_id"])
	require.Equal(t, map[string]any{"id": 123456789.0, "name": "Alice Liddell", "username": "alice"}, got[0]["from"])
	require.Equal(t, "2025-11-16T07:02:05Z", got[0]["date"])
	require.Equal(t, true, got[0]["out"])
	require.Equal(t, false, got[0]["service"])
	require.NotContains(t, got[0], "media")
}

func TestMessagesReadBadLimit(t *testing.T) {
	setup(t)
	for _, l := range []string{"0", "1001", "-3"} {
		code, _, errS := exec(t, "", "messages", "read", "me", "--limit", l)
		require.Equal(t, 2, code, errS)
	}
}

func TestMessagesReadUnknownPeerExit5(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "nosuchuser123456"}).
		ThenRPCErr(&tgerr.Error{Code: 400, Type: "USERNAME_NOT_OCCUPIED"})
	useMock(t, m)
	code, _, errS := exec(t, "", "messages", "read", "@nosuchuser123456")
	require.Equal(t, 5, code)
	require.Contains(t, errS, "@nosuchuser123456")
}

func TestMessagesSearch(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectFunc(func(b bin.Encoder) {
		req, ok := b.(*tg.MessagesSearchRequest)
		require.True(t, ok, "got %T", b)
		require.Equal(t, "hello", req.Q)
		require.Equal(t, &tg.InputPeerSelf{}, req.Peer)
		require.Equal(t, 5, req.Limit)
	}).ThenResult(msgs(textMsg(7, "hello again"), textMsg(3, "hello")))
	useMock(t, m)
	code, out, errS := exec(t, "", "messages", "search", "me", "hello", "--limit", "5", "--json")
	require.Equal(t, 0, code, errS)
	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 2)
	require.EqualValues(t, 3, got[0]["id"])
	require.EqualValues(t, 7, got[1]["id"])
}

func TestChatsList(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectFunc(func(b bin.Encoder) {
		req, ok := b.(*tg.MessagesGetDialogsRequest)
		require.True(t, ok, "got %T", b)
		require.Equal(t, 100, req.Limit)
	}).ThenResult(&tg.MessagesDialogs{
		Dialogs: []tg.DialogClass{
			&tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 1234567890}, TopMessage: 1, UnreadCount: 3},
			&tg.Dialog{Peer: &tg.PeerUser{UserID: 123456789}, TopMessage: 2},
			&tg.Dialog{Peer: &tg.PeerChat{ChatID: 4}, TopMessage: 3},
		},
		Messages: []tg.MessageClass{
			&tg.Message{ID: 1, PeerID: &tg.PeerChannel{ChannelID: 1234567890}, Date: 3},
			&tg.Message{ID: 2, PeerID: &tg.PeerUser{UserID: 123456789}, Date: 2},
			&tg.Message{ID: 3, PeerID: &tg.PeerChat{ChatID: 4}, Date: 1},
		},
		Chats: []tg.ChatClass{
			&tg.Channel{ID: 1234567890, AccessHash: 1, Title: "Go Developers", Username: "golang", Megagroup: true, Photo: &tg.ChatPhotoEmpty{}},
			&tg.Chat{ID: 4, Title: "Family", Photo: &tg.ChatPhotoEmpty{}},
		},
		Users: []tg.UserClass{alice().Elems[0]},
	})
	useMock(t, m)
	code, out, errS := exec(t, "", "chats", "list", "--limit", "2")
	require.Equal(t, 0, code, errS)
	require.Equal(t, `ID              TYPE        UNREAD  NAME
-1001234567890  supergroup  3       Go Developers (@golang)
123456789       user        0       Saved Messages
`, out)
}

func TestChatsListJSON(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.Expect().ThenResult(&tg.MessagesDialogs{
		Dialogs:  []tg.DialogClass{&tg.Dialog{Peer: &tg.PeerUser{UserID: 123456789}, TopMessage: 2}},
		Messages: []tg.MessageClass{&tg.Message{ID: 2, PeerID: &tg.PeerUser{UserID: 123456789}, Date: 2}},
		Users:    []tg.UserClass{alice().Elems[0]},
	})
	useMock(t, m)
	code, out, errS := exec(t, "", "chats", "list", "--limit", "0", "--json")
	require.Equal(t, 0, code, errS)
	require.JSONEq(t, `[{"id":123456789,"type":"user","title":"Saved Messages","username":"","unread":0}]`, out)
}

func TestMessagesReadChannelPrivateExit5(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.ContactsResolveUsernameRequest{Username: "priv"}).ThenResult(&tg.ContactsResolvedPeer{
		Peer:  &tg.PeerChannel{ChannelID: 5},
		Chats: []tg.ChatClass{&tg.Channel{ID: 5, AccessHash: 1, Title: "P", Photo: &tg.ChatPhotoEmpty{}}},
	})
	m.Expect().ThenRPCErr(&tgerr.Error{Code: 400, Type: "CHANNEL_PRIVATE"})
	useMock(t, m)
	code, _, errS := exec(t, "", "messages", "read", "@priv")
	require.Equal(t, 5, code, errS)
	require.Contains(t, errS, "@priv")
}

func TestMessagesSearchChannelPrivateExit5(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.Expect().ThenRPCErr(&tgerr.Error{Code: 400, Type: "PEER_ID_INVALID"})
	useMock(t, m)
	code, _, errS := exec(t, "", "messages", "search", "me", "x")
	require.Equal(t, 5, code, errS)
}

func TestMessagesReadSkipsEmptyAndFillsOwnSender(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	own := &tg.Message{ID: 3, Date: 1763276525, Message: "mine", Out: true, PeerID: &tg.PeerUser{UserID: 777}}
	res := &tg.MessagesMessages{Users: []tg.UserClass{&tg.User{ID: 777, AccessHash: 1, FirstName: "Bob"}}}
	res.Messages = []tg.MessageClass{own, &tg.MessageEmpty{ID: 2}, textMsg(1, "old")}
	expectHistory(t, m, func(*tg.MessagesGetHistoryRequest) {}, res)
	m.ExpectCall(selfReq).ThenResult(alice())
	useMock(t, m)
	code, out, errS := exec(t, "", "messages", "read", "me", "--limit", "2", "--json")
	require.Equal(t, 0, code, errS)
	var got []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	require.Len(t, got, 2) // the empty entry is neither printed nor counted
	require.EqualValues(t, 1, got[0]["id"])
	require.EqualValues(t, 3, got[1]["id"])
	require.Equal(t, "Alice Liddell", got[1]["from"].(map[string]any)["name"])
}
