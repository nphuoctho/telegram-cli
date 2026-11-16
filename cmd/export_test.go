package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
	"github.com/gotd/td/tgmock"
	"github.com/stretchr/testify/require"
)

func chdir(t *testing.T) string {
	dir := t.TempDir()
	old, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(old) })
	return dir
}

func dated(id int, day int, text string) *tg.Message {
	m := textMsg(id, text)
	m.Date = int(time.Date(2025, 11, day, 12, 0, 0, 0, time.UTC).Unix())
	return m
}

func expectSelf(m *tgmock.Mock) {
	m.ExpectCall(&tg.UsersGetUsersRequest{ID: []tg.InputUserClass{&tg.InputUserSelf{}}}).ThenResult(alice())
}

func TestExportMeAllMessagesOldestFirst(t *testing.T) {
	setup(t)
	dir := chdir(t)
	m := tgmock.NewRequire(t)
	expectSelf(m)
	expectHistory(t, m, func(r *tg.MessagesGetHistoryRequest) { require.Zero(t, r.OffsetDate) },
		msgs(dated(3, 3, "c"), dated(2, 2, "b"), dated(1, 1, "a")))
	useMock(t, m)
	code, out, errS := exec(t, "", "export", "me")
	require.Equal(t, 0, code, errS)
	require.Equal(t, "Exported 3 messages to saved-messages.json\n", out)
	b, err := os.ReadFile(filepath.Join(dir, "saved-messages.json"))
	require.NoError(t, err)
	var doc struct {
		Chat     map[string]any
		Since    *string
		Messages []struct{ ID int }
	}
	require.NoError(t, json.Unmarshal(b, &doc))
	require.Equal(t, "Saved Messages", doc.Chat["title"])
	require.Nil(t, doc.Since)
	require.Equal(t, []int{1, 2, 3}, []int{doc.Messages[0].ID, doc.Messages[1].ID, doc.Messages[2].ID})
}

func TestExportSinceUntilMarkdownStopsAtSince(t *testing.T) {
	setup(t)
	dir := chdir(t)
	m := tgmock.NewRequire(t)
	expectSelf(m)
	expectHistory(t, m, func(r *tg.MessagesGetHistoryRequest) {
		require.Equal(t, int(time.Date(2025, 11, 8, 0, 0, 0, 0, time.UTC).Unix()), r.OffsetDate) // whole 7th included
	}, msgs(dated(4, 7, "d"), dated(3, 5, "c"), dated(2, 1, "b"), dated(1, 1, "a")))
	useMock(t, m) // no second page expected: the loop stops at --since
	code, _, errS := exec(t, "", "export", "me", "--format", "md", "--since", "2025-11-02", "--until", "2025-11-07", "--output", "w.md")
	require.Equal(t, 0, code, errS)
	b, err := os.ReadFile(filepath.Join(dir, "w.md"))
	require.NoError(t, err)
	require.Contains(t, string(b), "## 2025-11-05")
	require.Contains(t, string(b), "## 2025-11-07")
	require.NotContains(t, string(b), "2025-11-01")
	require.Contains(t, string(b), "2 messages")
}

func TestExportLimitKeepsNewest(t *testing.T) {
	setup(t)
	dir := chdir(t)
	m := tgmock.NewRequire(t)
	expectSelf(m)
	expectHistory(t, m, func(*tg.MessagesGetHistoryRequest) {}, msgs(dated(3, 3, "c"), dated(2, 2, "b"), dated(1, 1, "a")))
	useMock(t, m)
	code, out, errS := exec(t, "", "export", "me", "--limit", "2", "--json")
	require.Equal(t, 0, code, errS)
	require.JSONEq(t, `{"output":"saved-messages.json","format":"json","messages":2,"files":0}`, out)
	b, _ := os.ReadFile(filepath.Join(dir, "saved-messages.json"))
	require.NotContains(t, string(b), `"text": "a"`)
}

func TestExportEmptyChat(t *testing.T) {
	setup(t)
	dir := chdir(t)
	m := tgmock.NewRequire(t)
	expectSelf(m)
	expectHistory(t, m, func(*tg.MessagesGetHistoryRequest) {}, msgs())
	useMock(t, m)
	code, out, errS := exec(t, "", "export", "me")
	require.Equal(t, 0, code, errS)
	require.Contains(t, out, "0 messages")
	b, _ := os.ReadFile(filepath.Join(dir, "saved-messages.json"))
	require.Contains(t, string(b), `"messages": []`)
}

func TestExportBadArgsExit2(t *testing.T) {
	setup(t)
	for _, args := range [][]string{
		{"export", "me", "--since", "2025-11-08", "--until", "2025-11-07"},
		{"export", "me", "--since", "nope"},
		{"export", "me", "--format", "xml"},
		{"export", "me", "--limit", "-1"},
	} {
		code, _, errS := exec(t, "", args...)
		require.Equal(t, 2, code, errS)
	}
}

func TestExportFailureLeavesNoFile(t *testing.T) {
	setup(t)
	dir := chdir(t)
	m := tgmock.NewRequire(t)
	expectSelf(m)
	m.Expect().ThenRPCErr(&tgerr.Error{Code: 500, Type: "INTERNAL"})
	useMock(t, m)
	code, _, _ := exec(t, "", "export", "me")
	require.Equal(t, 1, code)
	ents, _ := os.ReadDir(dir)
	require.Empty(t, ents)
}

func mediaMsg(id int) *tg.Message {
	m := dated(id, 3, "pic")
	m.Media = &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: int64(id), AccessHash: 1, FileReference: []byte{1}, Date: 1,
		Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "x", W: 1, H: 1, Size: 3}}}}
	return m
}

func TestExportMediaDownloadsOnceAndWarnsOnFailure(t *testing.T) {
	setup(t)
	dir := chdir(t)
	m := tgmock.NewRequire(t)
	expectSelf(m)
	expectHistory(t, m, func(*tg.MessagesGetHistoryRequest) {}, msgs(mediaMsg(2), mediaMsg(1)))
	m.ExpectFunc(func(b bin.Encoder) { require.IsType(t, &tg.UploadGetFileRequest{}, b) }).
		ThenResult(&tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: []byte("abc")})
	m.ExpectFunc(func(b bin.Encoder) { require.IsType(t, &tg.UploadGetFileRequest{}, b) }).
		ThenRPCErr(&tgerr.Error{Code: 400, Type: "FILE_REFERENCE_EXPIRED"})
	useMock(t, m)
	code, out, errS := exec(t, "", "export", "me", "--media")
	require.Equal(t, 0, code, errS)
	require.Contains(t, errS, "warning: message 1")
	require.Contains(t, out, "(1 files)", errS)
	ents, err := os.ReadDir(filepath.Join(dir, "saved-messages_media"))
	require.NoError(t, err)
	require.Len(t, ents, 1)
	b, _ := os.ReadFile(filepath.Join(dir, "saved-messages.json"))
	var doc struct{ Messages []map[string]any }
	require.NoError(t, json.Unmarshal(b, &doc))
	require.Equal(t, "saved-messages_media/"+ents[0].Name(), doc.Messages[1]["file"])
	require.NotContains(t, doc.Messages[0], "file")

	// Re-run: the existing file is skipped (no new getFile call), the other is retried.
	expectSelf(m)
	expectHistory(t, m, func(*tg.MessagesGetHistoryRequest) {}, msgs(mediaMsg(2), mediaMsg(1)))
	m.ExpectFunc(func(b bin.Encoder) { require.IsType(t, &tg.UploadGetFileRequest{}, b) }).
		ThenResult(&tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: []byte("abc")})
	code, out, errS = exec(t, "", "export", "me", "--media")
	require.Equal(t, 0, code, errS)
	require.Contains(t, out, "(2 files)")
}

func TestExportMultiPageStopsMidSecondPage(t *testing.T) {
	setup(t)
	chdir(t)
	m := tgmock.NewRequire(t)
	expectSelf(m)
	var page1 []*tg.Message
	for id := 102; id > 2; id-- {
		page1 = append(page1, dated(id, 5, "x"))
	}
	expectHistory(t, m, func(r *tg.MessagesGetHistoryRequest) { require.Equal(t, 100, r.Limit) }, &tg.MessagesMessagesSlice{Count: 102, Messages: msgClasses(page1), Users: alice().Elems})
	expectHistory(t, m, func(r *tg.MessagesGetHistoryRequest) { require.Equal(t, 100, r.Limit) },
		msgs(dated(2, 3, "y"), dated(1, 1, "old")))
	useMock(t, m)
	code, out, errS := exec(t, "", "export", "me", "--since", "2025-11-02")
	require.Equal(t, 0, code, errS)
	require.Contains(t, out, "Exported 101 messages")
}

func TestExportNestedOutputWithMedia(t *testing.T) {
	setup(t)
	dir := chdir(t)
	m := tgmock.NewRequire(t)
	expectSelf(m)
	expectHistory(t, m, func(*tg.MessagesGetHistoryRequest) {}, msgs(mediaMsg(1)))
	m.ExpectFunc(func(b bin.Encoder) { require.IsType(t, &tg.UploadGetFileRequest{}, b) }).
		ThenResult(&tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: []byte("abc")})
	useMock(t, m)
	code, out, errS := exec(t, "", "export", "me", "--media", "--output", "sub/x.md", "--format", "md", "--json")
	require.Equal(t, 0, code, errS)
	require.JSONEq(t, `{"output":"sub/x.md","format":"md","messages":1,"files":1,"media_dir":"sub/x_media"}`, out)
	ents, err := os.ReadDir(filepath.Join(dir, "sub", "x_media"))
	require.NoError(t, err)
	require.Len(t, ents, 1)
	st, err := os.Stat(filepath.Join(dir, "sub", "x_media"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), st.Mode().Perm())
	fi, err := ents[0].Info()
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
	b, _ := os.ReadFile(filepath.Join(dir, "sub", "x.md"))
	require.Contains(t, string(b), "](x_media/1_")
}

func TestExportOutputDirectoryExit2BeforeFetch(t *testing.T) {
	setup(t)
	dir := chdir(t)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "d"), 0o755))
	useMock(t, tgmock.NewRequire(t)) // any request would fail the test
	code, _, errS := exec(t, "", "export", "me", "--output", "d")
	require.Equal(t, 2, code, errS)
}

func msgClasses(ms []*tg.Message) (r []tg.MessageClass) {
	for _, m := range ms {
		r = append(r, m)
	}
	return r
}
