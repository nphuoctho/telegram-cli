package cmd

import (
	"context"
	"slices"

	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
)

const maxMessages = 1000

func newMessagesCmd() *cobra.Command {
	m := &cobra.Command{Use: "messages", Short: "Read and search messages"}
	var rLimit, before int
	read := &cobra.Command{
		Use:   "read <chat>",
		Short: "Print the newest messages of a chat, oldest first",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if before < 0 {
				return &config.UsageError{Msg: "--before must be a message ID"}
			}
			return listMessages(c, args[0], rLimit, func(api *tg.Client, p tg.InputPeerClass) *messages.Iterator {
				return query.Messages(api).GetHistory(p).OffsetID(before).BatchSize(batch(rLimit)).Iter()
			})
		},
	}
	read.Flags().IntVar(&rLimit, "limit", 20, "number of messages (max 1000)")
	read.Flags().IntVar(&before, "before", 0, "only messages older than this message ID")

	var sLimit int
	search := &cobra.Command{
		Use:   "search <chat> <query>",
		Short: "Search messages in one chat, oldest first",
		Args:  cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			return listMessages(c, args[0], sLimit, func(api *tg.Client, p tg.InputPeerClass) *messages.Iterator {
				return query.Messages(api).Search(p).Q(args[1]).BatchSize(batch(sLimit)).Iter()
			})
		},
	}
	search.Flags().IntVar(&sLimit, "limit", 20, "number of matches (max 1000)")
	m.AddCommand(read, search)
	return m
}

func batch(limit int) int { return min(limit, 100) }

// listMessages resolves chat, collects up to limit messages (newest first from
// Telegram) and prints them oldest first.
func listMessages(c *cobra.Command, chat string, limit int, iter func(*tg.Client, tg.InputPeerClass) *messages.Iterator) error {
	if limit < 1 || limit > maxMessages {
		return &config.UsageError{Msg: "--limit must be between 1 and 1000"}
	}
	out := []output.Message{}
	err := runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
		p, err := tgclient.ResolveChat(ctx, s.API, s.Peers, chat)
		if err != nil {
			return err
		}
		it := iter(s.API, p)
		for len(out) < limit && it.Next(ctx) {
			out = append(out, output.NewMessage(it.Value()))
		}
		return it.Err()
	})
	if err != nil {
		return err
	}
	slices.Reverse(out)
	if jsonMode(c) {
		return output.JSON(c.OutOrStdout(), out)
	}
	output.PrintMessages(c.OutOrStdout(), out)
	return nil
}
