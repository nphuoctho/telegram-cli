package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
)

// runWatch is replaced in tests with a fake update source.
var runWatch = func(ctx context.Context, c *cobra.Command, d *tg.UpdateDispatcher, fn func(context.Context, *tgclient.Session) error) error {
	return runTelegramOpts(ctx, c, true, d, fn)
}

func newWatchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch [<chat>...]",
		Short: "Stream new messages until Ctrl-C",
		RunE: func(c *cobra.Command, args []string) error {
			d := tg.NewUpdateDispatcher()
			dp := &d
			ctx, stop := context.WithCancel(c.Context())
			defer stop()
			w := &watcher{out: c.OutOrStdout(), json: jsonMode(c), stop: stop}
			d.OnNewMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
				return w.handle(e, u.Message)
			})
			d.OnNewChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
				return w.handle(e, u.Message)
			})
			err := runWatch(ctx, c, dp, func(ctx context.Context, s *tgclient.Session) error {
				if s != nil {
					w.setSelf(func() *tg.User {
						// shortcut: a failed Self lookup prints own messages without a sender, retry never needed for a cache hit.
						u, err := s.Peers.Self(ctx)
						if err != nil {
							return nil
						}
						return u.Raw()
					})
				}
				if len(args) == 0 {
					return nil
				}
				filter := map[int64]bool{}
				for _, a := range args {
					p, err := tgclient.ResolveChat(ctx, s.API, s.Peers, a)
					if err != nil {
						return err
					}
					_, id, err := peerInfo(ctx, s, p)
					if err != nil {
						return err
					}
					filter[id] = true
				}
				w.setFilter(filter)
				return nil
			})
			// Ctrl-C and a closed stdout (e.g. piping to head) are normal ways to stop.
			if errors.Is(err, context.Canceled) && (c.Context().Err() != nil || w.closed()) {
				return nil
			}
			return err
		},
	}
}

type watcher struct {
	out  io.Writer
	json bool

	mu     sync.Mutex
	filter map[int64]bool // nil = all chats
	self   func() *tg.User
	stop   context.CancelFunc
	broken bool // stdout write failed; the watch is stopping
}

func (w *watcher) closed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.broken
}

func (w *watcher) setSelf(f func() *tg.User) {
	w.mu.Lock()
	w.self = f
	w.mu.Unlock()
}

func (w *watcher) setFilter(f map[int64]bool) {
	w.mu.Lock()
	w.filter = f
	w.mu.Unlock()
}

func (w *watcher) handle(e tg.Entities, mc tg.MessageClass) error {
	nm, ok := mc.AsNotEmpty()
	if !ok {
		return nil
	}
	ents := peer.EntitiesFromUpdate(e)
	w.mu.Lock()
	defer w.mu.Unlock()
	m, ok := output.NewMessage(messages.Elem{Msg: nm, Entities: ents}, w.self)
	if !ok {
		return nil
	}
	if w.filter != nil && !w.filter[m.ChatID] {
		return nil
	}
	var buf bytes.Buffer
	if w.json {
		_ = output.JSON(&buf, m)
	} else {
		fmt.Fprintf(&buf, "[%s]\n", output.CleanLine(chatName(ents, nm)))
		output.PrintMessages(&buf, []output.Message{m})
	}
	if _, err := w.out.Write(buf.Bytes()); err != nil {
		w.broken = true
		w.stop()
	}
	return nil
}

func chatName(e peer.Entities, m tg.NotEmptyMessage) string {
	switch p := m.GetPeerID().(type) {
	case *tg.PeerUser:
		if u, ok := e.User(p.UserID); ok {
			if n := strings.TrimSpace(u.FirstName + " " + u.LastName); n != "" {
				return n
			}
		}
	case *tg.PeerChat:
		if c, ok := e.Chat(p.ChatID); ok {
			return c.Title
		}
	case *tg.PeerChannel:
		if c, ok := e.Channel(p.ChannelID); ok {
			return c.Title
		}
	}
	return fmt.Sprintf("chat %d", output.PeerID(m.GetPeerID()))
}
