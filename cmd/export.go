package cmd

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/export"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	var format, outPath, sinceS, untilS string
	var limit int
	var media bool
	cmd := &cobra.Command{
		Use:   "export <chat>",
		Short: "Export a chat's history to a JSON or Markdown file",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if format != "json" && format != "md" {
				return &config.UsageError{Msg: "--format must be json or md"}
			}
			if limit < 0 {
				return &config.UsageError{Msg: "--limit must be 0 or more"}
			}
			var since, until *time.Time
			if sinceS != "" {
				t, err := export.ParseDate(sinceS, false)
				if err != nil {
					return err
				}
				since = &t
			}
			if untilS != "" {
				t, err := export.ParseDate(untilS, true)
				if err != nil {
					return err
				}
				until = &t
			}
			if since != nil && until != nil && since.After(*until) {
				return &config.UsageError{Msg: "--since is after --until"}
			}
			var sum struct {
				Output   string `json:"output"`
				Format   string `json:"format"`
				Messages int    `json:"messages"`
				Files    int    `json:"files"`
				MediaDir string `json:"media_dir,omitempty"`
			}
			err := runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
				p, err := tgclient.ResolveChat(ctx, s.API, s.Peers, args[0])
				if err != nil {
					return err
				}
				peer, err := s.Peers.FromInputPeer(ctx, p)
				if err != nil {
					return err
				}
				chat := export.ChatFromPeer(peer)
				path := outPath
				if path == "" {
					path = export.Slug(chat) + "." + format
				}
				elems, err := export.Fetch(ctx, s.API, p, since, until, limit)
				if err != nil {
					return err
				}
				msgs := export.Convert(elems)
				sum.Output, sum.Format, sum.Messages = path, format, len(msgs)
				if media {
					dir := strings.TrimSuffix(path, filepath.Ext(path)) + "_media"
					sum.MediaDir = dir
					if sum.Files, err = export.DownloadMedia(ctx, s.API, elems, msgs, dir, c.ErrOrStderr()); err != nil {
						return err
					}
				}
				doc := export.Doc{Chat: chat, ExportedAt: time.Now(), Since: since, Until: until, Messages: msgs}
				var buf bytes.Buffer
				if format == "md" {
					err = export.WriteMarkdown(&buf, doc)
				} else {
					err = export.WriteJSON(&buf, doc)
				}
				if err != nil {
					return err
				}
				return config.WriteFileAtomic(path, buf.Bytes())
			})
			if err != nil {
				return err
			}
			if jsonMode(c) {
				return output.JSON(c.OutOrStdout(), sum)
			}
			if media {
				fmt.Fprintf(c.OutOrStdout(), "Exported %d messages (%d files) to %s\n", sum.Messages, sum.Files, sum.Output)
			} else {
				fmt.Fprintf(c.OutOrStdout(), "Exported %d messages to %s\n", sum.Messages, sum.Output)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&format, "format", "json", "json or md")
	f.StringVar(&outPath, "output", "", "output file (default: <chat-slug>.<format>)")
	f.StringVar(&sinceS, "since", "", "oldest message date (YYYY-MM-DD or RFC 3339)")
	f.StringVar(&untilS, "until", "", "newest message date, whole day included (YYYY-MM-DD or RFC 3339)")
	f.IntVar(&limit, "limit", 0, "keep only the newest N messages (0 = all)")
	f.BoolVar(&media, "media", false, "download attachments into <output>_media/")
	return cmd
}
