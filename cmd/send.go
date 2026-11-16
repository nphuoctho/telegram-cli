package cmd

import (
	"context"
	"fmt"
	"io"
	"strings"

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
			var label string
			err := runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
				p, err := tgclient.ResolveChat(ctx, s.API, s.Peers, args[0])
				if err != nil {
					return err
				}
				// Look the peer up before sending so a failure here never follows a delivered message.
				if label, chatID, err = peerInfo(ctx, s, p); err != nil {
					return err
				}
				rb := message.NewSender(s.API).To(p)
				if replyTo > 0 {
					id, err = unpack.MessageID(rb.Reply(replyTo).Text(ctx, text))
				} else {
					id, err = unpack.MessageID(rb.Text(ctx, text))
				}
				return err
			})
			if err != nil {
				return err
			}
			if jsonMode(c) {
				return output.JSON(c.OutOrStdout(), map[string]any{"id": id, "chat_id": chatID})
			}
			fmt.Fprintf(c.OutOrStdout(), "Sent message %d to %s\n", id, label)
			return nil
		},
	}
	cmd.Flags().IntVar(&replyTo, "reply-to", 0, "reply to this message ID")
	return cmd
}

// peerInfo returns the display label ("Alice (@alice)") and TDLib-style chat ID of p.
func peerInfo(ctx context.Context, s *tgclient.Session, p tg.InputPeerClass) (string, int64, error) {
	peer, err := s.Peers.FromInputPeer(ctx, p)
	if err != nil {
		return "", 0, err
	}
	label := output.CleanLine(peer.VisibleName())
	if u, ok := peer.Username(); ok {
		label += " (@" + output.CleanLine(u) + ")"
	}
	return label, int64(peer.TDLibPeerID()), nil
}
