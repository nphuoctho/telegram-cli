package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
	"github.com/spf13/cobra"
)

// runTelegram is the seam tests replace with a mock-backed session.
var runTelegram = func(ctx context.Context, c *cobra.Command, needAuth bool, fn func(context.Context, *tgclient.Session) error) error {
	r, err := config.Resolve()
	if err != nil {
		return err
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	o, err := tgclient.ConfigOptions(r, cfg, c.ErrOrStderr(), version())
	if err != nil {
		return err
	}
	o.NeedAuth = needAuth
	return tgclient.Run(ctx, o, fn)
}

func newAuthCmd() *cobra.Command {
	a := &cobra.Command{Use: "auth", Short: "Log in, log out, and show the session"}
	var phone string
	login := &cobra.Command{
		Use:   "login",
		Short: "Log in with phone, code, and 2FA password",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			term := &tgclient.Terminal{In: c.InOrStdin(), Err: c.ErrOrStderr(), PhoneFlag: phone}
			return runTelegram(c.Context(), c, false, func(ctx context.Context, s *tgclient.Session) error {
				st, err := s.Client.Auth().Status(ctx)
				if err != nil {
					return err
				}
				if !st.Authorized {
					if err := s.Client.Auth().IfNecessary(ctx, auth.NewFlow(term, auth.SendCodeOptions{})); err != nil {
						var su *auth.SignUpRequired
						if errors.As(err, &su) {
							return term.SignUpError()
						}
						return err
					}
				}
				u, err := s.Peers.Self(ctx)
				if err != nil {
					return err
				}
				return printSelf(c, u.Raw(), true)
			})
		},
	}
	login.Flags().StringVar(&phone, "phone", "", "phone number in international format")

	logout := &cobra.Command{
		Use:   "logout",
		Short: "Log out and delete the local session",
		Args:  cobra.NoArgs,
		RunE:  runLogout,
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show credentials source and login state",
		Args:  cobra.NoArgs,
		RunE:  runStatus,
	}
	a.AddCommand(login, logout, status)
	return a
}

func printSelf(c *cobra.Command, u *tg.User, login bool) error {
	v := output.NewUser(u, true)
	out := c.OutOrStdout()
	switch {
	case login && jsonMode(c):
		return output.JSON(out, map[string]any{"user": v})
	case login:
		fmt.Fprintf(out, "Logged in as %s\n", v.Summary())
	case jsonMode(c):
		return output.JSON(out, v)
	default:
		output.PrintUser(out, v)
	}
	return nil
}

func newMeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "me",
		Short: "Show your account",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
				u, err := s.Peers.Self(ctx)
				if err != nil {
					return err
				}
				return printSelf(c, u.Raw(), false)
			})
		},
	}
}

func runStatus(c *cobra.Command, _ []string) error {
	source := "none"
	r, rerr := config.Resolve()
	if r.Source != "" {
		source = r.Source
	}
	var user *output.User
	err := rerr
	if err == nil {
		err = runTelegram(c.Context(), c, true, func(ctx context.Context, s *tgclient.Session) error {
			u, err := s.Peers.Self(ctx)
			if err != nil {
				return err
			}
			v := output.NewUser(u.Raw(), true)
			user = &v
			return nil
		})
	} else if _, ok := err.(*config.NotConfiguredError); !ok {
		return err // bad credentials or env values are usage errors
	}
	err = tgclient.MapError(err)
	var ar *tgclient.AuthRequiredError
	if err != nil && !errors.As(err, &ar) && !isNotConfigured(err) {
		return err
	}

	out := c.OutOrStdout()
	if jsonMode(c) {
		if jerr := output.JSON(out, map[string]any{"credentials": source, "authorized": user != nil, "user": user}); jerr != nil {
			return jerr
		}
		return err
	}
	switch source {
	case "file":
		p, _ := config.CredentialsPath()
		fmt.Fprintf(out, "Credentials: file (%s)\n", p)
	case "none":
		fmt.Fprintln(out, "Credentials: none")
	default:
		fmt.Fprintf(out, "Credentials: %s\n", source)
	}
	if user != nil {
		fmt.Fprintf(out, "Logged in as %s\n", user.Summary())
	} else {
		fmt.Fprintln(out, "Not logged in")
	}
	return err
}

func isNotConfigured(err error) bool {
	var nc *config.NotConfiguredError
	return errors.As(err, &nc)
}

func runLogout(c *cobra.Command, _ []string) error {
	dir, err := config.DataDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(tgclient.SessionPath(dir)); errors.Is(err, fs.ErrNotExist) {
		if jsonMode(c) {
			return output.JSON(c.OutOrStdout(), map[string]any{"logged_out": false})
		}
		fmt.Fprintln(c.OutOrStdout(), "Not logged in")
		return nil
	}
	rpcErr := runTelegram(c.Context(), c, false, func(ctx context.Context, s *tgclient.Session) error {
		_, err := s.API.AuthLogOut(ctx)
		if auth.IsUnauthorized(err) {
			return nil // already logged out server-side
		}
		return err
	})
	for _, name := range []string{"session.json", "peers.json"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if rpcErr != nil {
		return fmt.Errorf("local session removed, but Telegram logout failed: %w; end the session in Settings > Devices", rpcErr)
	}
	if jsonMode(c) {
		return output.JSON(c.OutOrStdout(), map[string]any{"logged_out": true})
	}
	fmt.Fprintln(c.OutOrStdout(), "Logged out")
	return nil
}
