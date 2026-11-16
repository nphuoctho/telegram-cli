package output

import (
	"bytes"
	"testing"
	"time"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/stretchr/testify/require"
)

func init() { time.Local = time.UTC }

func nm(e messages.Elem) Message {
	m, _ := NewMessage(e, nil)
	return m
}

func elem(m tg.NotEmptyMessage) messages.Elem {
	return messages.Elem{Msg: m, Entities: peer.NewEntities(
		map[int64]*tg.User{1: {ID: 1, FirstName: "Alice", Username: "alice"}},
		map[int64]*tg.Chat{},
		map[int64]*tg.Channel{5: {ID: 5, Title: "News", Username: "news"}},
	)}
}

func TestNewMessageMedia(t *testing.T) {
	pm := &tg.PeerUser{UserID: 1}
	doc := func(mime string, attrs ...tg.DocumentAttributeClass) *tg.MessageMediaDocument {
		return &tg.MessageMediaDocument{Document: &tg.Document{ID: 1, Date: 1763276525, MimeType: mime, Size: 2048, Attributes: attrs}}
	}
	cases := map[string]struct {
		media tg.MessageMediaClass
		want  *Media
	}{
		"none":     {nil, nil},
		"webpage":  {&tg.MessageMediaWebPage{}, nil},
		"photo":    {&tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 9, Date: 1763276525, Sizes: []tg.PhotoSizeClass{&tg.PhotoSize{Type: "x", W: 10, H: 10, Size: 500}}}}, &Media{Type: "photo", FileName: "photo9_2025-11-16_07-02-05.jpg", MIMEType: "image/jpeg", Size: 500}},
		"video":    {doc("video/mp4", &tg.DocumentAttributeVideo{}, &tg.DocumentAttributeFilename{FileName: "a.mp4"}), &Media{Type: "video", FileName: "a.mp4", MIMEType: "video/mp4", Size: 2048}},
		"audio":    {doc("audio/mpeg", &tg.DocumentAttributeAudio{}), &Media{Type: "audio", MIMEType: "audio/mpeg", Size: 2048}},
		"voice":    {doc("audio/ogg", &tg.DocumentAttributeAudio{Voice: true}), &Media{Type: "voice", MIMEType: "audio/ogg", Size: 2048}},
		"sticker":  {doc("image/webp", &tg.DocumentAttributeSticker{}), &Media{Type: "sticker", MIMEType: "image/webp", Size: 2048}},
		"gif":      {doc("video/mp4", &tg.DocumentAttributeAnimated{}, &tg.DocumentAttributeVideo{}), &Media{Type: "gif", MIMEType: "video/mp4", Size: 2048}},
		"document": {doc("application/pdf", &tg.DocumentAttributeFilename{FileName: "s.pdf"}), &Media{Type: "document", FileName: "s.pdf", MIMEType: "application/pdf", Size: 2048}},
		"other":    {&tg.MessageMediaGeo{}, &Media{Type: "other"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := nm(elem(&tg.Message{ID: 1, PeerID: pm, Date: 1, Media: c.media}))
			if c.want != nil && name != "photo" {
				// document names come from gotd's generated fallback unless a filename attribute exists
				if c.want.FileName == "" {
					got.Media.FileName = ""
				}
			}
			require.Equal(t, c.want, got.Media)
		})
	}
}

func TestNewMessageFields(t *testing.T) {
	m := nm(elem(&tg.Message{
		ID: 10, Date: 1763276525, Message: "hi\nthere", PeerID: &tg.PeerChannel{ChannelID: 5},
		ReplyTo: &tg.MessageReplyHeader{ReplyToMsgID: 3},
	}))
	require.Equal(t, int64(-1000000000005), m.ChatID)
	require.Nil(t, m.From) // channel post without from
	require.Equal(t, 3, m.ReplyToID)

	m = nm(elem(&tg.Message{ID: 11, Date: 1, PeerID: &tg.PeerChannel{ChannelID: 5}, FromID: &tg.PeerUser{UserID: 1}}))
	require.Equal(t, &Sender{ID: 1, Name: "Alice", Username: "alice"}, m.From)

	// private chat incoming message without from_id
	m = nm(elem(&tg.Message{ID: 12, Date: 1, PeerID: &tg.PeerUser{UserID: 1}}))
	require.Equal(t, int64(1), m.From.ID)

	m = nm(elem(&tg.MessageService{ID: 13, Date: 1, PeerID: &tg.PeerChat{ChatID: 2}, FromID: &tg.PeerUser{UserID: 1}, Action: &tg.MessageActionChatAddUser{}}))
	require.True(t, m.Service)
	require.Equal(t, "MessageActionChatAddUser", m.Text)
	require.Equal(t, int64(-2), m.ChatID)
}

func TestPrintMessages(t *testing.T) {
	var b bytes.Buffer
	PrintMessages(&b, []Message{
		{ID: 1234, Date: time.Unix(1763276525, 0), From: &Sender{ID: 1, Name: "Alice", Username: "alice"}, ReplyToID: 1200,
			Text: "Hello\nthere", Media: &Media{Type: "photo", FileName: "p.jpg", Size: 183244}},
		{ID: 2, Date: time.Unix(1763276525, 0), Service: true, Text: "MessageActionPinMessage"},
	})
	require.Equal(t, `#1234  2025-11-16 07:02  Alice (@alice)  ↩1200
  Hello
  there
  [photo] p.jpg (178.9 KiB)
#2  2025-11-16 07:02
  * MessageActionPinMessage
`, b.String())
}

func TestNewMessageOwnWithoutFrom(t *testing.T) {
	e := elem(&tg.Message{ID: 1, Date: 1, Out: true, PeerID: &tg.PeerUser{UserID: 1}})
	m, ok := NewMessage(e, func() *tg.User { return &tg.User{ID: 9, FirstName: "Me", Username: "me"} })
	require.True(t, ok)
	require.Equal(t, &Sender{ID: 9, Name: "Me", Username: "me"}, m.From)
	m, _ = NewMessage(e, nil)
	require.Nil(t, m.From)
}
