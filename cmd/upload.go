package cmd

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
)

type uploadResult struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	As     string `json:"as"`
	ID     int    `json:"id"`
	ChatID int64  `json:"chat_id"`
}

func newUploadCmd() *cobra.Command {
	var caption, as string
	cmd := &cobra.Command{
		Use:   "upload <chat> <file>...",
		Short: "Upload files, one message per file",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			plans, err := tgclient.PlanUploads(args[1:], as)
			if err != nil {
				return err
			}
			results := []uploadResult{}
			err = runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
				if err := tgclient.CheckPremiumLimit(ctx, s.API, plans); err != nil {
					return err
				}
				p, err := tgclient.ResolveChat(ctx, s.API, s.Peers, args[0])
				if err != nil {
					return err
				}
				var chatID int64
				if jsonMode(c) {
					if _, chatID, err = peerInfo(ctx, s, p); err != nil {
						return err
					}
				}
				for _, pl := range plans {
					pr := output.NewProgress(c.ErrOrStderr(), filepath.Base(pl.Path))
					id, err := tgclient.SendFile(ctx, s.API, p, pl, caption, pr.Update)
					pr.Done()
					if err != nil {
						return fmt.Errorf("upload %s: %w", pl.Path, err)
					}
					results = append(results, uploadResult{pl.Path, pl.Size, pl.As, id, chatID})
					if !jsonMode(c) {
						fmt.Fprintf(c.OutOrStdout(), "%s  %s  sent as %s  message %d\n", pl.Path, output.Size(pl.Size), pl.As, id)
					}
				}
				return nil
			})
			if jsonMode(c) && len(results) > 0 {
				_ = output.JSON(c.OutOrStdout(), results) // earlier files stay sent; report them even on failure
			}
			return err
		},
	}
	cmd.Flags().StringVar(&caption, "caption", "", "caption for every file (plain text)")
	cmd.Flags().StringVar(&as, "as", "auto", "send as auto, file, photo or video")
	return cmd
}
