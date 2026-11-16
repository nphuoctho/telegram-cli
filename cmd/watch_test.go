package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgmock"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/nphuoctho/telegram-cli/internal/tgclient"
)

func channelMsg(id int, text string) *tg.UpdateNewChannelMessage {
	return &tg.UpdateNewChannelMessage{Message: &tg.Message{
		ID: id, Date: 1763276525, Message: text, PeerID: &tg.PeerChannel{ChannelID: 77},
		FromID: &tg.PeerUser{UserID: 123456789},
	}}
}

func feed(t *testing.T, d *tg.UpdateDispatcher, ups ...tg.UpdateClass) {
	err := d.Handle(context.Background(), &tg.Updates{
		Updates: ups,
		Users:   []tg.UserClass{alice().Elems[0]},
		Chats:   []tg.ChatClass{&tg.Channel{ID: 77, Title: "Gophers"}},
	})
	require.NoError(t, err)
}

// fakeWatch replaces the Telegram connection: after fn, it feeds updates and cancels.
func fakeWatch(t *testing.T, cancel context.CancelFunc, ups ...tg.UpdateClass) {
	old := runWatch
	t.Cleanup(func() { runWatch = old })
	runWatch = func(ctx context.Context, _ *cobra.Command, d *tg.UpdateDispatcher, fn func(context.Context, *tgclient.Session) error) error {
		if err := fn(ctx, nil); err != nil {
			return err
		}
		feed(t, d, ups...)
		cancel()
		return ctx.Err()
	}
}

func runCancelable(t *testing.T, cancel context.CancelFunc, ctx context.Context, args ...string) (int, string, string) {
	root := newRootCmd()
	var out, errB bytes.Buffer
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(&errB)
	code := run(ctx, root, &errB)
	return code, out.String(), errB.String()
}

func twoUpdates() []tg.UpdateClass {
	return []tg.UpdateClass{
		&tg.UpdateNewMessage{Message: textMsg(5, "hi")},
		channelMsg(6, "hello group"),
	}
}

func TestWatchReadableTwoMessagesExit0(t *testing.T) {
	setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	fakeWatch(t, cancel, twoUpdates()...)
	code, out, errS := runCancelable(t, cancel, ctx, "watch")
	require.Equal(t, 0, code, errS)
	require.Empty(t, errS)
	require.Equal(t, "[Alice Liddell]\n#5  2025-11-16 07:02  Alice Liddell (@alice)\n  hi\n"+
		"[Gophers]\n#6  2025-11-16 07:02  Alice Liddell (@alice)\n  hello group\n", out)
}

func TestWatchJSONLines(t *testing.T) {
	setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	fakeWatch(t, cancel, twoUpdates()...)
	code, out, errS := runCancelable(t, cancel, ctx, "watch", "--json")
	require.Equal(t, 0, code, errS)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Len(t, lines, 2)
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &m))
	require.Equal(t, "hello group", m["text"])
	require.EqualValues(t, -1000000000077, m["chat_id"])
}

func TestWatchFilterDropsOtherChats(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectCall(selfReq).ThenResult(alice())
	useMock(t, m)
	ctx, cancel := context.WithCancel(context.Background())
	old := runWatch
	t.Cleanup(func() { runWatch = old })
	runWatch = func(ctx context.Context, _ *cobra.Command, d *tg.UpdateDispatcher, fn func(context.Context, *tgclient.Session) error) error {
		// "me" resolves to self (alice's ID); reuse the mocked session.
		if err := runTelegram(ctx, nil, true, fn); err != nil {
			return err
		}
		feed(t, d, twoUpdates()...)
		cancel()
		return ctx.Err()
	}
	code, out, errS := runCancelable(t, cancel, ctx, "watch", "me", "--json")
	require.Equal(t, 0, code, errS)
	require.Equal(t, 1, strings.Count(out, "\n"), out)
	require.Contains(t, out, `"text":"hi"`)
}
