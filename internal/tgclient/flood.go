package tgclient

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/clock"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const maxFloodRetries = 5

// FloodWait retries requests that hit FLOOD_WAIT when the wait is at most max.
func FloodWait(max time.Duration, c clock.Clock, w io.Writer) telegram.Middleware {
	if c == nil {
		c = clock.System
	}
	return telegram.MiddlewareFunc(func(next tg.Invoker) telegram.InvokeFunc {
		return func(ctx context.Context, input bin.Encoder, output bin.Decoder) error {
			for retry := 0; ; retry++ {
				err := next.Invoke(ctx, input, output)
				d, ok := tgerr.AsFloodWait(err)
				if !ok {
					return err
				}
				if d > max || retry >= maxFloodRetries {
					return &FloodWaitError{Wait: d}
				}
				fmt.Fprintf(w, "rate limited by Telegram; waiting %s\n", d)
				// FloodWait returns the original flood error on success; only a false result is a real failure.
				if slept, werr := tgerr.FloodWait(ctx, err, tgerr.FloodWaitWithClock(c)); !slept {
					return werr
				}
			}
		}
	})
}
