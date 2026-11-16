package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/unpack"
	"github.com/gotd/td/tg"
	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
)

func newSendCmd() *cobra.Command {
	var replyTo int
	cmd := &cobra.Command{
		Use:   "send <chat> [text]",
		Short: "Send a text message (text from stdin when omitted or -)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			if replyTo < 0 {
				return &config.UsageError{Msg: "--reply-to must be a message ID"}
			}
			text := ""
			if len(args) == 2 {
				text = args[1]
			}
			if len(args) == 1 || text == "-" {
				b, err := io.ReadAll(c.InOrStdin())
				if err != nil {
					return err
				}
				text = strings.TrimRight(string(b), "\r\n")
			}
			if strings.TrimSpace(text) == "" {
				return &config.UsageError{Msg: "message text is empty"}
			}
			var id int
			var chatID int64
			err := runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
				p, err := tgclient.ResolveChat(ctx, s.API, s.Peers, args[0])
				if err != nil {
					return err
				}
				rb := message.NewSender(s.API).To(p)
				if replyTo > 0 {
					id, err = unpack.MessageID(rb.Reply(replyTo).Text(ctx, text))
				} else {
					id, err = unpack.MessageID(rb.Text(ctx, text))
				}
				if err != nil {
					return err
				}
				if jsonMode(c) {
					chatID, err = inputPeerID(ctx, s, p)
				}
				return err
			})
			if err != nil {
				return err
			}
			if jsonMode(c) {
				return output.JSON(c.OutOrStdout(), map[string]any{"id": id, "chat_id": chatID})
			}
			fmt.Fprintf(c.OutOrStdout(), "Sent message %d to %s\n", id, args[0])
			return nil
		},
	}
	cmd.Flags().IntVar(&replyTo, "reply-to", 0, "reply to this message ID")
	return cmd
}

// inputPeerID returns the TDLib-style chat ID of p (the same form as the shared JSON objects).
func inputPeerID(ctx context.Context, s *tgclient.Session, p tg.InputPeerClass) (int64, error) {
	var id constant.TDLibPeerID
	switch p := p.(type) {
	case *tg.InputPeerSelf:
		u, err := s.Peers.Self(ctx)
		if err != nil {
			return 0, err
		}
		id.User(u.ID())
	case *tg.InputPeerUser:
		id.User(p.UserID)
	case *tg.InputPeerChat:
		id.Chat(p.ChatID)
	case *tg.InputPeerChannel:
		id.Channel(p.ChannelID)
	}
	return int64(id), nil
}
