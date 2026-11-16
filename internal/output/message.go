package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

// Sender is the "from" object of a message.
type Sender struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Username string `json:"username"`
}

// Media describes an attachment.
type Media struct {
	Type     string `json:"type"`
	FileName string `json:"file_name,omitempty"`
	MIMEType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// Message is the shared JSON message object.
type Message struct {
	ID        int       `json:"id"`
	ChatID    int64     `json:"chat_id"`
	Date      time.Time `json:"date"`
	From      *Sender   `json:"from"`
	Out       bool      `json:"out"`
	Text      string    `json:"text"`
	ReplyToID int       `json:"reply_to_id,omitempty"`
	Media     *Media    `json:"media,omitempty"`
	File      string    `json:"file,omitempty"`
	Service   bool      `json:"service"`
}

// NewMessage converts a history or search element. self (may be nil) is used as
// the sender of own messages in private chats, where Telegram can omit from_id.
// ok is false for anything that is not a real message.
func NewMessage(e messages.Elem, self func() *tg.User) (Message, bool) {
	switch m := e.Msg.(type) {
	case *tg.MessageService:
		return Message{
			ID: m.ID, ChatID: peerID(m.PeerID), Date: time.Unix(int64(m.Date), 0).Local(),
			From: sender(e, m.FromID, m.PeerID, m.Out, self), Out: m.Out, Service: true,
			Text: strings.TrimPrefix(fmt.Sprintf("%T", m.Action), "*tg."),
		}, true
	case *tg.Message:
		v := Message{
			ID: m.ID, ChatID: peerID(m.PeerID), Date: time.Unix(int64(m.Date), 0).Local(),
			From: sender(e, m.FromID, m.PeerID, m.Out, self), Out: m.Out, Text: m.Message,
		}
		if h, ok := m.ReplyTo.(*tg.MessageReplyHeader); ok {
			v.ReplyToID = h.ReplyToMsgID
		}
		v.Media = newMedia(e, m.Media)
		return v, true
	}
	return Message{}, false
}

// PeerID returns the Bot-API style ID of a peer.
func PeerID(p tg.PeerClass) int64 { return peerID(p) }

func peerID(p tg.PeerClass) int64 {
	var id constant.TDLibPeerID
	switch p := p.(type) {
	case *tg.PeerUser:
		id.User(p.UserID)
	case *tg.PeerChat:
		id.Chat(p.ChatID)
	case *tg.PeerChannel:
		id.Channel(p.ChannelID)
	}
	return int64(id)
}

func sender(e messages.Elem, from, chat tg.PeerClass, out bool, self func() *tg.User) *Sender {
	if from == nil {
		// Private chats may omit from_id: incoming is the other user, outgoing is us.
		pu, ok := chat.(*tg.PeerUser)
		if !ok {
			return nil
		}
		if out {
			if self == nil {
				return nil
			}
			u := self()
			if u == nil {
				return nil
			}
			return &Sender{ID: u.ID, Name: strings.TrimSpace(u.FirstName + " " + u.LastName), Username: u.Username}
		}
		from = pu
	}
	switch p := from.(type) {
	case *tg.PeerUser:
		if u, ok := e.Entities.User(p.UserID); ok {
			return &Sender{ID: u.ID, Name: strings.TrimSpace(u.FirstName + " " + u.LastName), Username: u.Username}
		}
		return &Sender{ID: p.UserID}
	case *tg.PeerChannel:
		s := &Sender{ID: peerID(p)}
		if c, ok := e.Entities.Channel(p.ChannelID); ok {
			s.Name, s.Username = c.Title, c.Username
		}
		return s
	case *tg.PeerChat:
		s := &Sender{ID: peerID(p)}
		if c, ok := e.Entities.Chat(p.ChatID); ok {
			s.Name = c.Title
		}
		return s
	}
	return nil
}

func newMedia(e messages.Elem, m tg.MessageMediaClass) *Media {
	switch m := m.(type) {
	case nil, *tg.MessageMediaEmpty, *tg.MessageMediaUnsupported, *tg.MessageMediaWebPage:
		return nil
	case *tg.MessageMediaPhoto:
		v := &Media{Type: "photo"}
		if f, ok := e.File(); ok {
			v.FileName, v.MIMEType = f.Name, f.MIMEType
		}
		if p, ok := m.Photo.AsNotEmpty(); ok {
			v.Size = photoSize(p)
		}
		return v
	case *tg.MessageMediaDocument:
		doc, ok := m.Document.AsNotEmpty()
		if !ok {
			return &Media{Type: "other"}
		}
		v := &Media{Type: docType(doc), MIMEType: doc.MimeType, Size: doc.Size}
		if f, ok := e.File(); ok {
			v.FileName = f.Name
		}
		return v
	}
	return &Media{Type: "other"}
}

func docType(d *tg.Document) string {
	typ := "document"
	for _, a := range d.Attributes {
		switch a := a.(type) {
		case *tg.DocumentAttributeSticker:
			return "sticker"
		case *tg.DocumentAttributeAudio:
			if a.Voice {
				return "voice"
			}
			typ = "audio"
		case *tg.DocumentAttributeAnimated:
			return "gif"
		case *tg.DocumentAttributeVideo:
			if typ == "document" {
				typ = "video"
			}
		}
	}
	return typ
}

func photoSize(p *tg.Photo) (n int64) {
	for _, s := range p.Sizes {
		switch s := s.(type) {
		case *tg.PhotoSize:
			n = max(n, int64(s.Size))
		case *tg.PhotoSizeProgressive:
			if len(s.Sizes) > 0 {
				n = max(n, int64(s.Sizes[len(s.Sizes)-1]))
			}
		}
	}
	return n
}

// PrintMessages writes messages in the readable format of spec section 6.
func PrintMessages(w io.Writer, msgs []Message) {
	for _, m := range msgs {
		fmt.Fprintf(w, "#%d  %s", m.ID, m.Date.Local().Format("2006-01-02 15:04"))
		if f := m.From; f != nil {
			name := f.Name
			if name == "" {
				name = fmt.Sprintf("id %d", f.ID)
			}
			if f.Username != "" {
				name += " (@" + f.Username + ")"
			}
			fmt.Fprintf(w, "  %s", name)
		}
		if m.ReplyToID != 0 {
			fmt.Fprintf(w, "  ↩%d", m.ReplyToID)
		}
		fmt.Fprintln(w)
		if m.Service {
			fmt.Fprintf(w, "  * %s\n", m.Text)
		} else if m.Text != "" {
			for _, l := range strings.Split(m.Text, "\n") {
				fmt.Fprintf(w, "  %s\n", l)
			}
		}
		if md := m.Media; md != nil {
			line := "[" + md.Type + "]"
			if md.FileName != "" {
				line += " " + md.FileName
			}
			if md.Size > 0 {
				line += " (" + Size(md.Size) + ")"
			}
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}
