package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/gotd/td/tgmock"
	"github.com/stretchr/testify/require"
)

func expectSend(t *testing.T, m *tgmock.Mock, check func(*tg.MessagesSendMessageRequest), res tg.UpdatesClass) {
	m.ExpectFunc(func(b bin.Encoder) {
		req, ok := b.(*tg.MessagesSendMessageRequest)
		require.True(t, ok, "got %T", b)
		check(req)
	}).ThenResult(res)
}

func expectSelfInfo(m *tgmock.Mock) {
	m.ExpectCall(&tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUserSelf{}}}).ThenResult(alice())
}

func TestSendPeerTextReplyAndShortID(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	expectSelfInfo(m)
	expectSend(t, m, func(r *tg.MessagesSendMessageRequest) {
		require.Equal(t, &tg.InputPeerSelf{}, r.Peer)
		require.Equal(t, "hello", r.Message)
		require.Equal(t, &tg.InputReplyToMessage{ReplyToMsgID: 7}, r.ReplyTo)
	}, &tg.UpdateShortSentMessage{ID: 55})
	useMock(t, m)
	code, out, errS := exec(t, "", "send", "me", "hello", "--reply-to", "7")
	require.Equal(t, 0, code, errS)
	require.Equal(t, "Sent message 55 to Alice Liddell (@alice)\n", out)
}

func TestSendStdinAndChannelUpdate(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	expectSelfInfo(m)
	expectSend(t, m, func(r *tg.MessagesSendMessageRequest) { require.Equal(t, "hi", r.Message) },
		&tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateNewChannelMessage{Message: &tg.Message{ID: 99, PeerID: &tg.PeerChannel{ChannelID: 1}}}}})
	useMock(t, m)
	code, out, errS := exec(t, "hi\n", "send", "me")
	require.Equal(t, 0, code, errS)
	require.Contains(t, out, "message 99")
}

func TestSendEmptyStdinExit2(t *testing.T) {
	setup(t)
	for _, args := range [][]string{{"send", "me"}, {"send", "me", "-"}, {"send", "me", "  "}} {
		code, _, _ := exec(t, "\n", args...)
		require.Equal(t, 2, code)
	}
}

func TestSendJSONSelf(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	expectSelfInfo(m)
	expectSend(t, m, func(*tg.MessagesSendMessageRequest) {}, &tg.UpdateShortSentMessage{ID: 3})
	useMock(t, m)
	code, out, errS := exec(t, "", "send", "me", "x", "--json")
	require.Equal(t, 0, code, errS)
	require.JSONEq(t, `{"id":3,"chat_id":123456789}`, out)
}

func TestUploadSmallFileAsDocumentThenPhoto(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	pdf, jpg := filepath.Join(dir, "a.pdf"), filepath.Join(dir, "b.jpg")
	require.NoError(t, os.WriteFile(pdf, []byte("pdf"), 0o600))
	require.NoError(t, os.WriteFile(jpg, []byte("jpg"), 0o600))
	m := tgmock.NewRequire(t)
	save := func() {
		m.ExpectFunc(func(b bin.Encoder) { require.IsType(t, &tg.UploadSaveFilePartRequest{}, b) }).ThenTrue()
	}
	media := func(id int, check func(tg.InputMediaClass)) {
		m.ExpectFunc(func(b bin.Encoder) {
			r, ok := b.(*tg.MessagesSendMediaRequest)
			require.True(t, ok, "got %T", b)
			require.Equal(t, "cap", r.Message)
			check(r.Media)
		}).ThenResult(&tg.UpdateShortSentMessage{ID: id})
	}
	save()
	media(10, func(mm tg.InputMediaClass) {
		d, ok := mm.(*tg.InputMediaUploadedDocument)
		require.True(t, ok, "got %T", mm)
		require.Equal(t, "application/pdf", d.MimeType)
	})
	save()
	media(11, func(mm tg.InputMediaClass) { require.IsType(t, &tg.InputMediaUploadedPhoto{}, mm) })
	useMock(t, m)
	code, out, errS := exec(t, "", "upload", "me", pdf, jpg, "--caption", "cap")
	require.Equal(t, 0, code, errS)
	require.Contains(t, out, "sent as file  message 10")
	require.Contains(t, out, "sent as photo  message 11")
}

func TestUploadStopsOnFirstFailure(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	require.NoError(t, os.WriteFile(a, []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(b, []byte("b"), 0o600))
	m := tgmock.NewRequire(t)
	m.ExpectFunc(func(bin.Encoder) {}).ThenTrue()
	m.ExpectFunc(func(bin.Encoder) {}).ThenRPCErr(&tgerr.Error{Code: 400, Type: "MEDIA_EMPTY"})
	useMock(t, m) // a third request would fail the mock
	code, _, errS := exec(t, "", "upload", "me", a, b)
	require.Equal(t, 1, code)
	require.Contains(t, errS, "a.txt")
}

func TestUploadRejectsBeforeNetwork(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	big := filepath.Join(dir, "big.jpg")
	f, err := os.Create(big)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(12<<20))
	require.NoError(t, f.Close())
	// No mock installed: any network use would try to connect and fail differently.
	code, _, errS := exec(t, "", "upload", "me", big, "--as", "photo")
	require.Equal(t, 2, code, errS)
	huge := filepath.Join(dir, "huge.bin")
	f, err = os.Create(huge)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(4_194_304_001))
	require.NoError(t, f.Close())
	code, _, errS = exec(t, "", "upload", "me", huge)
	require.Equal(t, 2, code)
	require.Contains(t, errS, "file too large (limit 2000 MiB, 4000 MiB for Premium)")
	empty := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	code, _, errS = exec(t, "", "upload", "me", empty)
	require.Equal(t, 2, code)
	require.Contains(t, errS, "is empty")
	code, _, _ = exec(t, "", "upload", "me", dir)
	require.Equal(t, 2, code)
	code, _, _ = exec(t, "", "upload", "me", filepath.Join(dir, "missing"))
	require.Equal(t, 2, code)
}

func TestUploadOverStandardLimitNeedsPremium(t *testing.T) {
	setup(t)
	huge := filepath.Join(t.TempDir(), "huge.bin")
	f, err := os.Create(huge)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(2_097_152_001))
	require.NoError(t, f.Close())
	m := tgmock.NewRequire(t)
	m.ExpectCall(&tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUserSelf{}}}).ThenResult(alice())
	useMock(t, m)
	code, _, errS := exec(t, "", "upload", "me", huge)
	require.Equal(t, 2, code, errS)
	require.Contains(t, errS, "limit 2000 MiB")
}

func docMsg(id int, name string, size int64) *tg.Message {
	m := textMsg(id, "")
	m.Media = &tg.MessageMediaDocument{Document: &tg.Document{
		ID: 1, Size: size, MimeType: "application/pdf", FileReference: []byte{1},
		Attributes: []tg.DocumentAttributeClass{&tg.DocumentAttributeFilename{FileName: name}},
	}}
	return m
}

func expectGetMessages(m *tgmock.Mock, ms ...*tg.Message) {
	m.ExpectFunc(func(b bin.Encoder) {}).ThenResult(msgs(ms...))
}

func TestDownloadSaveSkipOverwrite(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	data := []byte("hello file")
	for i, args := range [][]string{{}, {}, {"--overwrite"}} {
		m := tgmock.NewRequire(t)
		expectGetMessages(m, docMsg(5, "../../evil.pdf", int64(len(data))))
		if i != 1 {
			m.ExpectFunc(func(b bin.Encoder) { require.IsType(t, &tg.UploadGetFileRequest{}, b) }).
				ThenResult(&tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: data})
		}
		useMock(t, m)
		code, out, errS := exec(t, "", append([]string{"download", "me", "5", "--dir", dir}, args...)...)
		require.Equal(t, 0, code, errS)
		target := filepath.Join(dir, "5_evil.pdf")
		if i == 1 {
			require.Equal(t, "skipped  "+target+"  (exists)\n", out)
		} else {
			require.Contains(t, out, "saved    "+target)
		}
		got, err := os.ReadFile(target)
		require.NoError(t, err)
		require.Equal(t, data, got)
	}
	require.NoFileExists(t, filepath.Join(dir, "5_evil.pdf.part"))
}

func TestDownloadRPCErrorRemovesPartAndOthersContinue(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	m := tgmock.NewRequire(t)
	expectGetMessages(m, docMsg(5, "a.pdf", 10), textMsg(6, "no media"), docMsg(8, "b.pdf", 3))
	m.ExpectFunc(func(bin.Encoder) {}).ThenRPCErr(&tgerr.Error{Code: 400, Type: "LOCATION_INVALID"})
	m.ExpectFunc(func(bin.Encoder) {}).ThenResult(&tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: []byte("abc")})
	useMock(t, m)
	code, out, errS := exec(t, "", "download", "me", "5", "6", "7", "8", "--dir", dir, "--json")
	require.Equal(t, 1, code)
	require.Contains(t, errS, "3 of 4")
	require.Contains(t, out, `"error":"message has no media"`)
	require.Contains(t, out, `"error":"message not found"`)
	require.Contains(t, out, `"status":"saved"`)
	require.NoFileExists(t, filepath.Join(dir, "5_a.pdf.part"))
	require.NoFileExists(t, filepath.Join(dir, "5_a.pdf"))
	require.FileExists(t, filepath.Join(dir, "8_b.pdf"))
	require.False(t, strings.Contains(out, "5_a.pdf"))
}

func TestUploadJSONPartialFailureKeepsChatID(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.mp4")
	require.NoError(t, os.WriteFile(a, []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(b, []byte("b"), 0o600))
	m := tgmock.NewRequire(t)
	expectSelfInfo(m)
	m.ExpectFunc(func(bin.Encoder) {}).ThenTrue()
	m.ExpectFunc(func(bin.Encoder) {}).ThenResult(&tg.UpdateShortSentMessage{ID: 4})
	m.ExpectFunc(func(bin.Encoder) {}).ThenTrue()
	m.ExpectFunc(func(b bin.Encoder) {
		r, ok := b.(*tg.MessagesSendMediaRequest)
		require.True(t, ok, "got %T", b)
		d := r.Media.(*tg.InputMediaUploadedDocument)
		require.Contains(t, d.Attributes, &tg.DocumentAttributeVideo{SupportsStreaming: true})
	}).ThenRPCErr(&tgerr.Error{Code: 400, Type: "MEDIA_EMPTY"})
	useMock(t, m)
	code, out, _ := exec(t, "", "upload", "me", a, b, "--as", "video", "--json")
	require.Equal(t, 1, code)
	require.JSONEq(t, `[{"path":"`+a+`","size":1,"as":"video","id":4,"chat_id":123456789}]`, out)
}

func TestDownloadUnauthorizedExit4(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	expectGetMessages(m, docMsg(5, "a.pdf", 10), docMsg(6, "b.pdf", 10))
	m.ExpectFunc(func(bin.Encoder) {}).ThenUnregistered() // second message must not be attempted
	useMock(t, m)
	code, _, _ := exec(t, "", "download", "me", "5", "6", "--dir", t.TempDir())
	require.Equal(t, 4, code)
}

func TestDownloadEmptyMessageIsNotFound(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	m.ExpectFunc(func(bin.Encoder) {}).ThenResult(&tg.MessagesMessages{Messages: []tg.MessageClass{&tg.MessageEmpty{ID: 9}}})
	useMock(t, m)
	code, out, _ := exec(t, "", "download", "me", "9", "--dir", t.TempDir())
	require.Equal(t, 1, code)
	require.Contains(t, out, "message not found")
}

func TestDownloadCtrlCRemovesPart(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	m := tgmock.NewRequire(t)
	expectGetMessages(m, docMsg(5, "a.pdf", 10))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.ExpectFunc(func(bin.Encoder) { cancel() }).ThenRPCErr(&tgerr.Error{Code: 500, Type: "INTERNAL"})
	useMock(t, m)
	root := newRootCmd()
	var out, errB bytes.Buffer
	root.SetArgs([]string{"download", "me", "5", "--dir", dir})
	root.SetOut(&out)
	root.SetErr(&errB)
	require.Equal(t, 130, run(ctx, root, &errB), errB.String())
	require.NoFileExists(t, filepath.Join(dir, "5_a.pdf.part"))
}

func TestDownloadCreatesDir(t *testing.T) {
	setup(t)
	dir := filepath.Join(t.TempDir(), "new", "sub")
	m := tgmock.NewRequire(t)
	expectGetMessages(m, docMsg(5, "a.pdf", 3))
	m.ExpectFunc(func(bin.Encoder) {}).ThenResult(&tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: []byte("abc")})
	useMock(t, m)
	code, _, errS := exec(t, "", "download", "me", "5", "--dir", dir)
	require.Equal(t, 0, code, errS)
	require.FileExists(t, filepath.Join(dir, "5_a.pdf"))
}

func TestSendStripsControlCharsFromName(t *testing.T) {
	setup(t)
	m := tgmock.NewRequire(t)
	u := alice().Elems[0].(*tg.User)
	u.FirstName, u.LastName, u.Username = "x\x1b]0;pwn\x07\rA\x9d", "", "u\x1b"
	m.ExpectCall(&tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUserSelf{}}}).
		ThenResult(&tg.UserClassVector{Elems: []tg.UserClass{u}})
	expectSend(t, m, func(*tg.MessagesSendMessageRequest) {}, &tg.UpdateShortSentMessage{ID: 1})
	useMock(t, m)
	code, out, errS := exec(t, "", "send", "me", "x")
	require.Equal(t, 0, code, errS)
	for _, bad := range []string{"\x1b", "\r", "\x07", "\x9d"} {
		require.NotContains(t, out, bad)
	}
	require.True(t, strings.HasPrefix(out, "Sent message 1 to "))
}

func TestDownloadFatalErrorKeepsSavedResults(t *testing.T) {
	setup(t)
	dir := t.TempDir()
	m := tgmock.NewRequire(t)
	expectGetMessages(m, docMsg(5, "a.pdf", 3), docMsg(6, "b.pdf", 3))
	m.ExpectFunc(func(bin.Encoder) {}).ThenResult(&tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: []byte("abc")})
	m.ExpectFunc(func(bin.Encoder) {}).ThenUnregistered()
	useMock(t, m)
	code, out, _ := exec(t, "", "download", "me", "5", "6", "--dir", dir, "--json")
	require.Equal(t, 4, code)
	require.Contains(t, out, `"message_id":5,"status":"saved"`)
}
