// Package tgclient wires gotd/td into the CLI: client construction, flood-wait
// handling, and the terminal login flow.
package tgclient

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/gotd/td/clock"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/nphuoctho/telegram-cli/internal/config"
)

// LoginClient is the part of gotd's auth client that login needs.
type LoginClient interface {
	Status(ctx context.Context) (*auth.Status, error)
	auth.FlowClient
}

// Session is what a command callback works with.
type Session struct {
	Auth  LoginClient // nil in tests that only use a mock API
	API   *tg.Client
	Peers *peers.Manager
}

// Options configures Run.
type Options struct {
	APIID        int
	APIHash      string
	DataDir      string
	MaxFloodWait time.Duration
	Clock        clock.Clock // nil = system clock
	Stderr       io.Writer
	Version      string
	// NeedAuth requires an existing session file (else AuthRequiredError, no
	// network) and verifies it with Peers.Init before running the callback.
	NeedAuth bool

	peerStorage peers.Storage // nil = in memory; set by Run
}

// SessionPath returns <dataDir>/session.json.
func SessionPath(dataDir string) string { return filepath.Join(dataDir, "session.json") }

// prepare checks the session file (when required) and creates the data dir.
func prepare(o Options) (sessionPath string, err error) {
	sessionPath = SessionPath(o.DataDir)
	if o.NeedAuth {
		if _, err := os.Stat(sessionPath); errors.Is(err, fs.ErrNotExist) {
			return "", &AuthRequiredError{}
		}
	}
	return sessionPath, os.MkdirAll(o.DataDir, 0o700)
}

// start builds the session over api, verifies authorization when required, and runs fn.
func start(ctx context.Context, o Options, api *tg.Client, authc LoginClient, fn func(context.Context, *Session) error) error {
	s := &Session{Auth: authc, API: api, Peers: peers.Options{Storage: o.peerStorage}.Build(api)}
	if o.NeedAuth {
		if err := s.Peers.Init(ctx); err != nil {
			return err
		}
	}
	return fn(ctx, s)
}

// fixMode makes the session file private; gotd only sets 0600 when it creates the file.
func fixMode(path string) error {
	if err := os.Chmod(path, 0o600); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Run connects to Telegram, runs fn, and disconnects.
func Run(ctx context.Context, o Options, fn func(context.Context, *Session) error) error {
	path, err := prepare(o)
	if err != nil {
		return err
	}
	store, err := LoadPeerStore(PeersPath(o.DataDir))
	if err != nil {
		if o.NeedAuth {
			return err
		}
		// auth login/logout must work even with a corrupt cache (logout deletes it).
		store = &PeerStore{path: PeersPath(o.DataDir), data: map[peers.Key]int64{}}
	}
	o.peerStorage = store
	client := telegram.NewClient(o.APIID, o.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: path},
		NoUpdates:      true,
		Clock:          o.Clock,
		Device:         telegram.DeviceConfig{DeviceModel: "telegram-cli " + o.Version, AppVersion: o.Version},
		Middlewares:    []telegram.Middleware{FloodWait(o.MaxFloodWait, o.Clock, o.Stderr)},
	})
	err = client.Run(ctx, func(ctx context.Context) error {
		ferr := start(ctx, o, client.API(), client.Auth(), fn)
		if serr := store.Flush(); serr != nil && ferr == nil {
			return serr
		}
		return ferr
	})
	if err == nil {
		err = fixMode(path)
	}
	return MapError(err)
}

// Login runs the interactive login unless the session is already authorized,
// and returns the logged-in account.
func Login(ctx context.Context, s *Session, term *Terminal) (*tg.User, error) {
	st, err := s.Auth.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !st.Authorized {
		if err := auth.NewFlow(term, auth.SendCodeOptions{}).Run(ctx, s.Auth); err != nil {
			var su *auth.SignUpRequired
			if errors.As(err, &su) {
				return nil, term.SignUpError()
			}
			return nil, err
		}
	}
	u, err := s.Peers.Self(ctx)
	if err != nil {
		return nil, err
	}
	return u.Raw(), nil
}

// ConfigOptions builds Options from the resolved config and credentials.
func ConfigOptions(r config.Resolved, cfg config.Config, stderr io.Writer, version string) (Options, error) {
	dir, err := config.DataDir()
	if err != nil {
		return Options{}, err
	}
	max, err := cfg.MaxFloodDuration()
	if err != nil {
		return Options{}, err
	}
	return Options{APIID: r.APIID, APIHash: r.APIHash, DataDir: dir, MaxFloodWait: max, Stderr: stderr, Version: version}, nil
}
