package tgclient

import (
	"errors"
	"fmt"
	"time"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tgerr"
	"github.com/nphuoctho/telegram-cli/internal/config"
)

// AuthRequiredError means there is no usable session (exit 4).
type AuthRequiredError struct{}

func (*AuthRequiredError) Error() string {
	return `not logged in; run "telegram-cli auth login"`
}

// FloodWaitError means Telegram asked to wait longer than we accept (exit 6).
type FloodWaitError struct{ Wait time.Duration }

func (e *FloodWaitError) Error() string {
	return fmt.Sprintf("Telegram asks to wait %s before retrying this request", e.Wait)
}

// MapError turns raw gotd errors into the typed errors the CLI maps to exit codes.
func MapError(err error) error {
	var ar *AuthRequiredError
	var fw *FloodWaitError
	var nc *config.NotConfiguredError
	switch {
	case err == nil, errors.As(err, &ar), errors.As(err, &fw), errors.As(err, &nc):
		return err
	case auth.IsUnauthorized(err):
		return &AuthRequiredError{}
	case tgerr.Is(err, "API_ID_INVALID"):
		return &config.NotConfiguredError{Msg: "Telegram rejected the API ID/hash; check https://my.telegram.org"}
	}
	return err
}
