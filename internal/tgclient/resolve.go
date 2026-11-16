package tgclient

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// PeerNotFoundError means a chat identifier could not be resolved (exit 5).
type PeerNotFoundError struct{ Msg string }

func (e *PeerNotFoundError) Error() string { return e.Msg }

// NotFound builds the exit-5 error for the identifier the user typed.
func NotFound(arg string) *PeerNotFoundError {
	if numericID.MatchString(arg) {
		return &PeerNotFoundError{fmt.Sprintf(`chat %s not found; check "telegram-cli chats list"`, arg)}
	}
	return &PeerNotFoundError{fmt.Sprintf("chat %s not found", arg)}
}

var numericID = regexp.MustCompile(`^-?[0-9]+$`)

var notFoundRPC = []string{
	"USERNAME_NOT_OCCUPIED", "USERNAME_INVALID", "PEER_ID_INVALID", "CHANNEL_INVALID",
	"CHANNEL_PRIVATE", "USER_ID_INVALID", "CHAT_ID_INVALID",
}

// IsNotFound reports whether err means the chat does not exist or is not accessible.

func IsNotFound(err error) bool {
	var pnf *peers.PeerNotFoundError
	var phn *peers.PhoneNotFoundError
	if errors.As(err, &pnf) || errors.As(err, &phn) {
		return true
	}
	for _, c := range notFoundRPC {
		if tgerr.Is(err, c) {
			return true
		}
	}
	return false
}

// ResolveChat turns a chat identifier (spec section 5) into an input peer.
func ResolveChat(ctx context.Context, api *tg.Client, m *peers.Manager, arg string) (tg.InputPeerClass, error) {
	arg = strings.TrimSpace(arg)
	if strings.EqualFold(arg, "me") {
		return &tg.InputPeerSelf{}, nil
	}
	if !numericID.MatchString(arg) {
		p, err := m.Resolve(ctx, arg)
		if err != nil {
			if IsNotFound(err) {
				return nil, NotFound(arg)
			}
			return nil, err
		}
		return p.InputPeer(), nil
	}
	notFound := NotFound(arg)
	n, err := strconv.ParseInt(arg, 10, 64)
	id := constant.TDLibPeerID(n)
	if err != nil || !(id.IsUser() || id.IsChat() || id.IsChannel()) {
		return nil, notFound
	}
	p, err := m.ResolveTDLibID(ctx, id)
	if err != nil && IsNotFound(err) {
		if werr := WalkDialogs(ctx, api, m); werr != nil {
			return nil, werr
		}
		p, err = m.ResolveTDLibID(ctx, id)
	}
	if err != nil {
		if IsNotFound(err) {
			return nil, notFound
		}
		return nil, err
	}
	return p.InputPeer(), nil
}

// WalkDialogs pages through every dialog, applying the users and chats of each
// page to m so their access hashes are stored.
func WalkDialogs(ctx context.Context, api *tg.Client, m *peers.Manager) error {
	it := query.GetDialogs(api).BatchSize(100).Iter()
	for it.Next(ctx) {
		if err := ApplyDialog(ctx, m, it.Value()); err != nil {
			return err
		}
	}
	return it.Err()
}

// ApplyDialog stores the access hash of the dialog's own peer.
func ApplyDialog(ctx context.Context, m *peers.Manager, e dialogs.Elem) error {
	d, ok := e.Dialog.(*tg.Dialog)
	if !ok {
		return nil // archive folder entry
	}
	switch p := d.Peer.(type) {
	case *tg.PeerUser:
		if u, ok := e.Entities.User(p.UserID); ok {
			return m.Apply(ctx, []tg.UserClass{u}, nil)
		}
	case *tg.PeerChat:
		if c, ok := e.Entities.Chat(p.ChatID); ok {
			return m.Apply(ctx, nil, []tg.ChatClass{c})
		}
	case *tg.PeerChannel:
		if c, ok := e.Entities.Channel(p.ChannelID); ok {
			return m.Apply(ctx, nil, []tg.ChatClass{c})
		}
	}
	return nil
}
