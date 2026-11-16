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
	"sync/atomic"
	"time"

	"github.com/gotd/td/clock"
	"github.com/gotd/td/session"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"github.com/nphuoctho/telegram-cli/internal/config"
)

// Session is what a command callback works with.
type Session struct {
	Client *telegram.Client // nil in tests that only use a mock API
	API    *tg.Client
	Peers  *peers.Manager
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
	// Dispatcher, when set, switches to watch mode: updates are delivered to
	// it after fn returns, and Run blocks until ctx is cancelled.
	Dispatcher *tg.UpdateDispatcher
}

// SessionPath returns <dataDir>/session.json.
func SessionPath(dataDir string) string { return filepath.Join(dataDir, "session.json") }

// Run connects to Telegram, runs fn, and disconnects.
func Run(ctx context.Context, o Options, fn func(context.Context, *Session) error) error {
	path := SessionPath(o.DataDir)
	if o.NeedAuth {
		if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
			return &AuthRequiredError{}
		}
	}
	if err := os.MkdirAll(o.DataDir, 0o700); err != nil {
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
	// Watch mode: the update manager needs the peer manager, which needs the
	// client, so the handler is bound late. Updates before that are dropped.
	var hook atomic.Pointer[updates.Manager]
	var handler telegram.UpdateHandler
	if o.Dispatcher != nil {
		handler = telegram.UpdateHandlerFunc(func(ctx context.Context, u tg.UpdatesClass) error {
			if m := hook.Load(); m != nil {
				return m.Handle(ctx, u)
			}
			return nil
		})
	}
	client := telegram.NewClient(o.APIID, o.APIHash, telegram.Options{
		SessionStorage: &session.FileStorage{Path: path},
		NoUpdates:      o.Dispatcher == nil,
		UpdateHandler:  handler,
		Clock:          o.Clock,
		Device:         telegram.DeviceConfig{DeviceModel: "telegram-cli " + o.Version, AppVersion: o.Version},
		Middlewares:    []telegram.Middleware{FloodWait(o.MaxFloodWait, o.Clock, o.Stderr)},
	})
	err = client.Run(ctx, func(ctx context.Context) error {
		api := client.API()
		s := &Session{Client: client, API: api, Peers: peers.Options{Storage: store}.Build(api)}
		if o.NeedAuth {
			if err := s.Peers.Init(ctx); err != nil {
				return err
			}
		}
		ferr := fn(ctx, s)
		if ferr == nil && o.Dispatcher != nil {
			ferr = watch(ctx, s, o.Dispatcher, &hook)
		}
		if serr := store.Flush(); serr != nil && ferr == nil {
			return serr
		}
		return ferr
	})
	if err == nil {
		// gotd only sets 0600 when it creates the file; make sure.
		if cerr := os.Chmod(path, 0o600); cerr != nil && !errors.Is(cerr, fs.ErrNotExist) {
			return cerr
		}
	}
	return MapError(err)
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

// watch streams updates into d until ctx is cancelled.
func watch(ctx context.Context, s *Session, d *tg.UpdateDispatcher, hook *atomic.Pointer[updates.Manager]) error {
	self, err := s.Peers.Self(ctx)
	if err != nil {
		return err
	}
	m := updates.New(updates.Config{Handler: s.Peers.UpdateHook(d), AccessHasher: s.Peers})
	hook.Store(m)
	// Forget: start from "now", no catch-up of missed messages.
	return m.Run(ctx, s.API, self.ID(), updates.AuthOptions{Forget: true})
}
