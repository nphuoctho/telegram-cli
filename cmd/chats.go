package cmd

import (
	"context"

	"github.com/gotd/td/telegram/query"
	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
)

func newChatsCmd() *cobra.Command {
	chats := &cobra.Command{Use: "chats", Short: "List chats"}
	var limit int
	list := &cobra.Command{
		Use:   "list",
		Short: "List chats, pinned first, then by last activity",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if limit < 0 {
				return &config.UsageError{Msg: "--limit must be 0 or more"}
			}
			out := []output.Chat{}
			err := runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
				it := query.GetDialogs(s.API).BatchSize(100).Iter()
				for (limit == 0 || len(out) < limit) && it.Next(ctx) {
					e := it.Value()
					if err := tgclient.ApplyDialog(ctx, s.Peers, e); err != nil {
						return err
					}
					if ch, ok := output.NewChat(e); ok {
						out = append(out, ch)
					}
				}
				return it.Err()
			})
			if err != nil {
				return err
			}
			if jsonMode(c) {
				return output.JSON(c.OutOrStdout(), out)
			}
			output.PrintChats(c.OutOrStdout(), out)
			return nil
		},
	}
	list.Flags().IntVar(&limit, "limit", 50, "maximum number of chats (0 = all)")
	chats.AddCommand(list)
	return chats
}
