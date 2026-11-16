package tgclient

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"
)

type fakeFlow struct {
	needPassword, noAccount bool
	gotCode, gotPass        string
}

func (f *fakeFlow) SendCode(context.Context, string, auth.SendCodeOptions) (tg.AuthSentCodeClass, error) {
	return &tg.AuthSentCode{Type: &tg.AuthSentCodeTypeApp{}, PhoneCodeHash: "h"}, nil
}

func (f *fakeFlow) SignIn(_ context.Context, _, code, _ string) (*tg.AuthAuthorization, error) {
	f.gotCode = code
	switch {
	case f.noAccount:
		return nil, &auth.SignUpRequired{}
	case f.needPassword:
		return nil, auth.ErrPasswordAuthNeeded
	}
	return &tg.AuthAuthorization{User: &tg.User{}}, nil
}

func (f *fakeFlow) Password(_ context.Context, p string) (*tg.AuthAuthorization, error) {
	f.gotPass = p
	return &tg.AuthAuthorization{User: &tg.User{}}, nil
}

func (f *fakeFlow) SignUp(context.Context, auth.SignUp) (*tg.AuthAuthorization, error) {
	panic("must not sign up")
}

func runFlow(t *testing.T, f *fakeFlow, input string) (stderr string, err error) {
	var errB bytes.Buffer
	term := &Terminal{In: strings.NewReader(input), Err: &errB}
	err = auth.NewFlow(term, auth.SendCodeOptions{}).Run(context.Background(), f)
	return errB.String(), err
}

func TestFlowCodeOnly(t *testing.T) {
	f := &fakeFlow{}
	errS, err := runFlow(t, f, "+15551234567\n12345\n")
	require.NoError(t, err)
	require.Equal(t, "12345", f.gotCode)
	require.Contains(t, errS, "Code sent to the Telegram app")
	require.NotContains(t, errS, "12345")
}

func TestFlowCodeAnd2FA(t *testing.T) {
	f := &fakeFlow{needPassword: true}
	errS, err := runFlow(t, f, "+15551234567\n12345\nhunter2\n")
	require.NoError(t, err)
	require.Equal(t, "hunter2", f.gotPass)
	require.NotContains(t, errS, "hunter2")
	require.NotContains(t, errS, "12345")
}

func TestFlowSignUpRequired(t *testing.T) {
	f := &fakeFlow{noAccount: true}
	_, err := runFlow(t, f, "+15551234567\n12345\n")
	require.ErrorContains(t, err, "sign up in an official app")
}

func TestPhoneFlag(t *testing.T) {
	f := &fakeFlow{}
	var errB bytes.Buffer
	term := &Terminal{In: strings.NewReader("12345\n"), Err: &errB, PhoneFlag: "+1555"}
	require.NoError(t, auth.NewFlow(term, auth.SendCodeOptions{}).Run(context.Background(), f))
	require.NotContains(t, errB.String(), "Phone")
}
