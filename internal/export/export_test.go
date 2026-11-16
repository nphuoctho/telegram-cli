package export

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/stretchr/testify/require"
)

func init() { time.Local = time.UTC }

func TestParseDate(t *testing.T) {
	d, err := ParseDate("2025-11-07", false)
	require.NoError(t, err)
	require.Equal(t, "2025-11-07T00:00:00Z", d.Format(time.RFC3339))
	d, err = ParseDate("2025-11-07", true)
	require.NoError(t, err)
	require.Equal(t, "2025-11-07T23:59:59Z", d.Format(time.RFC3339))
	d, err = ParseDate("2025-11-07T10:00:00+07:00", true)
	require.NoError(t, err)
	require.Equal(t, int64(1762484400), d.Unix())
	for _, bad := range []string{"", "yesterday", "2025-13-01", "11/07/2025"} {
		_, err = ParseDate(bad, false)
		require.ErrorAs(t, err, new(*config.UsageError), bad)
	}
}

func TestSlug(t *testing.T) {
	for _, c := range []struct {
		in   output.Chat
		want string
	}{
		{output.Chat{ID: 1, Username: "GoLang", Title: "Go Developers"}, "golang"},
		{output.Chat{ID: 1, Title: "Go  Developers!"}, "go-developers"},
		{output.Chat{ID: 1, Title: "Saved Messages"}, "saved-messages"},
		{output.Chat{ID: -100123, Title: "Привет 你好"}, "chat--100123"},
		{output.Chat{ID: 7}, "chat-7"},
	} {
		require.Equal(t, c.want, Slug(c.in))
	}
}

var chat = output.Chat{ID: -1001, Type: "supergroup", Title: "Go Developers", Username: "golang"}

func sample() Doc {
	alice := &output.Sender{ID: 1, Name: "Alice", Username: "alice"}
	at := func(d, h, m int) time.Time { return time.Date(2025, 11, d, h, m, 5, 0, time.UTC) }
	since := at(15, 0, 0)
	return Doc{Chat: chat, ExportedAt: time.Date(2025, 11, 16, 15, 0, 0, 0, time.UTC), Since: &since,
		Messages: []output.Message{
			{ID: 1, ChatID: -1001, Date: at(15, 9, 0), From: alice, Text: "joined", Service: true},
			{ID: 2, ChatID: -1001, Date: at(16, 14, 2), From: alice, Text: "Hello *there*",
				Media: &output.Media{Type: "photo", FileName: "p.jpg", Size: 2048}, File: "go_media/2_p.jpg"},
			{ID: 3, ChatID: -1001, Date: at(16, 14, 5), From: &output.Sender{ID: 9}, Text: "re", ReplyToID: 2,
				Media: &output.Media{Type: "document", FileName: "my slides.pdf", Size: 2202009}},
			{ID: 4, ChatID: -1001, Date: at(16, 14, 6), Media: &output.Media{Type: "document", FileName: "my slides.pdf"}, File: "go_media/4_my slides.pdf"},
		}}
}

func golden(t *testing.T, path string, got []byte) {
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, string(want), string(got))
}

func TestWriteMarkdownGolden(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, WriteMarkdown(&b, sample()))
	golden(t, "testdata/export.md", b.Bytes())
}

func TestWriteJSONGolden(t *testing.T) {
	var b bytes.Buffer
	d := sample()
	require.NoError(t, WriteJSON(&b, d))
	golden(t, "testdata/export.json", b.Bytes())
}

func TestWriteEmpty(t *testing.T) {
	var b bytes.Buffer
	require.NoError(t, WriteJSON(&b, Doc{Chat: chat}))
	require.Contains(t, b.String(), `"messages": []`)
	b.Reset()
	require.NoError(t, WriteMarkdown(&b, Doc{Chat: chat, ExportedAt: time.Now()}))
	require.Contains(t, b.String(), "0 messages\n")
}
