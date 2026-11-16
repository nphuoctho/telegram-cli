package tgclient

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
)

// Terminal implements auth.UserAuthenticator with prompts on Err.
// Typed secrets are never echoed to Err or anywhere else.
type Terminal struct {
	In        io.Reader
	Err       io.Writer
	PhoneFlag string // from --phone; empty = prompt

	br *bufio.Reader
}

var _ auth.UserAuthenticator = (*Terminal)(nil)

func (t *Terminal) line(prompt string) (string, error) {
	fmt.Fprint(t.Err, prompt)
	if t.br == nil {
		t.br = bufio.NewReader(t.In)
	}
	s, err := t.br.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && s != "") {
		return "", errors.New("unexpected end of input")
	}
	return strings.TrimSpace(s), nil
}

func (t *Terminal) Phone(context.Context) (string, error) {
	if t.PhoneFlag != "" {
		return t.PhoneFlag, nil
	}
	return t.line("Phone (international format): ")
}

func (t *Terminal) Password(context.Context) (string, error) {
	if f, ok := t.In.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(t.Err, "2FA password: ")
		b, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(t.Err)
		return string(b), err
	}
	return t.line("2FA password: ")
}

func (t *Terminal) Code(_ context.Context, sent *tg.AuthSentCode) (string, error) {
	where := "you"
	switch sent.Type.(type) {
	case *tg.AuthSentCodeTypeApp:
		where = "the Telegram app"
	case *tg.AuthSentCodeTypeSMS:
		where = "SMS"
	case *tg.AuthSentCodeTypeCall, *tg.AuthSentCodeTypeFlashCall, *tg.AuthSentCodeTypeMissedCall:
		where = "a phone call"
	}
	return t.line("Code sent to " + where + ": ")
}

func (t *Terminal) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error { return nil }

func (t *Terminal) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("no Telegram account for this number; sign up in an official app first")
}

// SignUpError is the error shown when the phone has no account.
func (t *Terminal) SignUpError() error {
	_, err := t.SignUp(context.Background())
	return err
}
