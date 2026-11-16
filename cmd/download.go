package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gotd/td/telegram/query"
	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
)

type downloadResult struct {
	MessageID int    `json:"message_id"`
	Status    string `json:"status"`
	Path      string `json:"path,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Error     string `json:"error,omitempty"`
}

func newDownloadCmd() *cobra.Command {
	var dir string
	var overwrite bool
	cmd := &cobra.Command{
		Use:   "download <chat> <msg-id>...",
		Short: "Download the media of messages",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			ids := make([]int, 0, len(args)-1)
			for _, a := range args[1:] {
				n, err := strconv.Atoi(a)
				if err != nil || n < 1 {
					return &config.UsageError{Msg: fmt.Sprintf("invalid message ID %q", a)}
				}
				ids = append(ids, n)
			}
			if dir == "" {
				cfg, err := config.LoadConfig()
				if err != nil {
					return err
				}
				dir = cfg.DownloadDir
			}
			var results []downloadResult
			err := runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
				p, err := tgclient.ResolveChat(ctx, s.API, s.Peers, args[0])
				if err != nil {
					return err
				}
				elems, err := query.Messages(s.API).GetMessagesWithEntities(ctx, p, ids...)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
				for _, id := range ids {
					r := downloadResult{MessageID: id}
					var found bool
					for _, e := range elems {
						if e.Msg.GetID() != id { // the library already drops MessageEmpty
							continue
						}
						found = true
						f, ok := e.File()
						if !ok {
							r.Status, r.Error = "error", "message has no media"
							break
						}
						target := filepath.Join(dir, fmt.Sprintf("%d_%s", id, tgclient.SafeName(f.Name)))
						var total int64
						if v, ok := output.NewMessage(e, nil); ok && v.Media != nil {
							total = v.Media.Size
						}
						pr := output.NewProgress(c.ErrOrStderr(), filepath.Base(target))
						err := tgclient.DownloadTo(ctx, s.API, f.Location, total, target, overwrite, pr.Update)
						pr.Done()
						r.Path = target
						switch {
						case err == nil:
							r.Status, r.Size = "saved", total
							if st, serr := os.Stat(target); serr == nil {
								r.Size = st.Size()
							}
						case errors.Is(err, tgclient.ErrSkipped):
							r.Status = "skipped"
						case ctx.Err() != nil:
							return ctx.Err()
						default:
							// Session and flood-wait failures end the command with their own exit code.
							var ar *tgclient.AuthRequiredError
							var fw *tgclient.FloodWaitError
							if mapped := tgclient.MapError(err); errors.As(mapped, &ar) || errors.As(mapped, &fw) {
								return mapped
							}
							r.Status, r.Path, r.Error = "error", "", err.Error()
						}
						break
					}
					if !found {
						r.Status, r.Error = "error", "message not found"
					}
					results = append(results, r)
				}
				return nil
			})
			if err != nil {
				return err
			}
			failed := 0
			for _, r := range results {
				if r.Status == "error" {
					failed++
				}
			}
			if jsonMode(c) {
				err = output.JSON(c.OutOrStdout(), results)
			} else {
				printDownloads(c, results)
			}
			if err == nil && failed > 0 {
				err = fmt.Errorf("%d of %d messages failed", failed, len(results))
			}
			return err
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "destination directory (default: download_dir from config)")
	cmd.Flags().BoolVar(&overwrite, "overwrite", false, "replace files that already exist")
	return cmd
}

func printDownloads(c *cobra.Command, rs []downloadResult) {
	w := c.OutOrStdout()
	for _, r := range rs {
		switch r.Status {
		case "saved":
			fmt.Fprintf(w, "saved    %s  %s\n", r.Path, output.Size(r.Size))
		case "skipped":
			fmt.Fprintf(w, "skipped  %s  (exists)\n", r.Path)
		default:
			fmt.Fprintf(w, "error    message %d: %s\n", r.MessageID, r.Error)
		}
	}
}
