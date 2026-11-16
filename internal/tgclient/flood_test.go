package tgclient

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gotd/neo"
	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgmock"
	"github.com/stretchr/testify/require"
)

func floodClient(t *testing.T, m *tgmock.Mock, max time.Duration, clk *neo.Time, w *bytes.Buffer) *tg.Client {
	return tg.NewClient(FloodWait(max, clk, w).Handle(m))
}

func TestFloodWaitRetries(t *testing.T) {
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.HelpGetConfigRequest{}).ThenFlood(3)
	m.ExpectCall(&tg.HelpGetConfigRequest{}).ThenResult(&tg.Config{DCOptions: nil})
	clk := neo.NewTime(time.Now())
	var w bytes.Buffer
	api := floodClient(t, m, time.Minute, clk, &w)

	obs := clk.Observe()
	done := make(chan error, 1)
	go func() { _, err := api.HelpGetConfig(context.Background()); done <- err }()
	<-obs
	clk.Travel(4 * time.Second)
	require.NoError(t, <-done)
	require.Contains(t, w.String(), "rate limited by Telegram; waiting 3s")
}

func TestFloodWaitTooLong(t *testing.T) {
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.HelpGetConfigRequest{}).ThenFlood(3600)
	api := floodClient(t, m, time.Minute, neo.NewTime(time.Now()), &bytes.Buffer{})
	_, err := api.HelpGetConfig(context.Background())
	var fw *FloodWaitError
	require.True(t, errors.As(err, &fw))
	require.Equal(t, time.Hour, fw.Wait)
}

func TestFloodWaitGivesUp(t *testing.T) {
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.HelpGetConfigRequest{}).N(maxFloodRetries + 1).ThenFlood(1)
	clk := neo.NewTime(time.Now())
	api := floodClient(t, m, time.Minute, clk, &bytes.Buffer{})
	done := make(chan error, 1)
	go func() { _, err := api.HelpGetConfig(context.Background()); done <- err }()
	var err error
	for waiting := true; waiting; {
		select {
		case err = <-done:
			waiting = false
		case <-time.After(time.Millisecond):
			clk.Travel(2 * time.Second) // each retry timer needs its own advance
		}
	}
	var fw *FloodWaitError
	require.True(t, errors.As(err, &fw))
}

func TestFloodWaitCancel(t *testing.T) {
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.HelpGetConfigRequest{}).ThenFlood(60)
	clk := neo.NewTime(time.Now())
	api := floodClient(t, m, time.Minute, clk, &bytes.Buffer{})
	ctx, cancel := context.WithCancel(context.Background())
	obs := clk.Observe()
	done := make(chan error, 1)
	go func() { _, err := api.HelpGetConfig(ctx); done <- err }()
	<-obs
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestMapErrorUnauthorized(t *testing.T) {
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUserSelf{}}}).ThenUnregistered()
	_, err := peers.Options{}.Build(tg.NewClient(m)).Self(context.Background())
	var ar *AuthRequiredError
	require.True(t, errors.As(MapError(err), &ar))
}
