package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/gotd/td/constant"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
)

// Chat is the shared JSON chat object.
type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username"`
	Unread   *int   `json:"unread,omitempty"`
}

// NewChat converts a dialog. ok is false for entries that are not chats (archive folders)
// or whose entity is missing from the response.
func NewChat(e dialogs.Elem) (Chat, bool) {
	d, ok := e.Dialog.(*tg.Dialog)
	if !ok {
		return Chat{}, false
	}
	var c Chat
	var id constant.TDLibPeerID
	switch p := d.Peer.(type) {
	case *tg.PeerUser:
		u, ok := e.Entities.User(p.UserID)
		if !ok {
			return Chat{}, false
		}
		id.User(u.ID)
		c = Chat{ID: int64(id), Type: "user", Title: strings.TrimSpace(u.FirstName + " " + u.LastName), Username: u.Username}
		if u.Bot {
			c.Type = "bot"
		}
		if u.Self {
			c.Title, c.Username = "Saved Messages", ""
		}
	case *tg.PeerChat:
		g, ok := e.Entities.Chat(p.ChatID)
		if !ok {
			return Chat{}, false
		}
		id.Chat(g.ID)
		c = Chat{ID: int64(id), Type: "group", Title: g.Title}
	case *tg.PeerChannel:
		g, ok := e.Entities.Channel(p.ChannelID)
		if !ok {
			return Chat{}, false
		}
		id.Channel(g.ID)
		c = Chat{ID: int64(id), Type: "supergroup", Title: g.Title, Username: g.Username}
		if g.Broadcast {
			c.Type = "channel"
		}
	default:
		return Chat{}, false
	}
	c.Unread = &d.UnreadCount
	return c, true
}

// PrintChats writes the readable `chats list` table.
func PrintChats(w io.Writer, chats []Chat) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tTYPE\tUNREAD\tNAME")
	for _, c := range chats {
		name := clean(c.Title)
		if c.Username != "" {
			name += " (@" + clean(c.Username) + ")"
		}
		unread := 0
		if c.Unread != nil {
			unread = *c.Unread
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n", strconv.FormatInt(c.ID, 10), c.Type, unread, name)
	}
	tw.Flush()
}
