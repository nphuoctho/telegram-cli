// Package export fetches a chat's history and writes it as JSON or Markdown.
package export

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gotd/td/telegram/peers"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
	"github.com/nphuoctho/telegram-cli/internal/tgclient"
)

// ParseDate parses YYYY-MM-DD (local midnight) or RFC 3339. With end set, a plain date
// means the last second of that day so --until covers the whole day.
func ParseDate(s string, end bool) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		if end {
			t = t.AddDate(0, 0, 1).Add(-time.Second)
		}
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, &config.UsageError{Msg: fmt.Sprintf("invalid date %q; use YYYY-MM-DD or RFC 3339", s)}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug names the default output file: username or title, else chat-<id>.
func Slug(c output.Chat) string {
	for _, s := range []string{c.Username, c.Title} {
		if s = strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-"); s != "" {
			return s
		}
	}
	return "chat-" + strconv.FormatInt(c.ID, 10)
}

// ChatFromPeer builds the chat object of a resolved peer.
func ChatFromPeer(p peers.Peer) output.Chat {
	id := int64(p.TDLibPeerID())
	switch p := p.(type) {
	case peers.User:
		u := p.Raw()
		c := output.Chat{ID: id, Type: "user", Title: strings.TrimSpace(u.FirstName + " " + u.LastName), Username: u.Username}
		if u.Bot {
			c.Type = "bot"
		}
		if u.Self {
			c.Title, c.Username = "Saved Messages", ""
		}
		return c
	case peers.Channel:
		g := p.Raw()
		c := output.Chat{ID: id, Type: "supergroup", Title: g.Title, Username: g.Username}
		if g.Broadcast {
			c.Type = "channel"
		}
		return c
	}
	return output.Chat{ID: id, Type: "group", Title: p.VisibleName()}
}

// CheckOutput rejects an output path that is an existing directory.
func CheckOutput(path string) error {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return &config.UsageError{Msg: path + " is a directory; --output must be a file"}
	}
	return nil
}

// Fetch reads history newest first from until (inclusive; nil = now) back to since,
// keeping at most limit messages (0 = all).
func Fetch(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, since, until *time.Time, limit int, self func() *tg.User) ([]messages.Elem, error) {
	batch := 100
	if limit > 0 {
		batch = min(limit, batch)
	}
	q := query.Messages(api).GetHistory(peer).BatchSize(batch)
	if until != nil {
		q = q.OffsetDate(int(until.Unix()) + 1) // offset date is exclusive
	}
	it := q.Iter()
	var out []messages.Elem
	for it.Next(ctx) {
		e := it.Value()
		if since != nil && int64(e.Msg.GetDate()) < since.Unix() {
			break
		}
		if _, ok := output.NewMessage(e, self); !ok {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, it.Err()
}

// Convert turns fetched elements (newest first) into messages, oldest first.
func Convert(elems []messages.Elem, self func() *tg.User) []output.Message {
	out := make([]output.Message, len(elems))
	for i, e := range elems {
		out[len(elems)-1-i], _ = output.NewMessage(e, self) // Fetch already dropped non-messages
	}
	return out
}

// DownloadMedia saves each attachment into dir and sets File (relative to dir's
// parent) on the matching message. elems are newest first, msgs oldest first (see Convert).
// Failures are warnings; the number of files present is returned.
func DownloadMedia(ctx context.Context, api *tg.Client, elems []messages.Elem, msgs []output.Message, dir string, warn io.Writer) (int, error) {
	files := 0
	for i, e := range elems {
		m := &msgs[len(msgs)-1-i]
		f, ok := e.File()
		if !ok || m.Media == nil {
			continue
		}
		name := fmt.Sprintf("%d_%s", m.ID, tgclient.SafeName(f.Name))
		target := filepath.Join(dir, name)
		err := os.MkdirAll(dir, 0o700)
		if err == nil {
			err = os.Chmod(dir, 0o700) // also for a directory left by an earlier run
		}
		if err == nil {
			err = tgclient.DownloadTo(ctx, api, f.Location, m.Media.Size, target, 0o600, false, nil)
		}
		switch {
		case err == nil || errors.Is(err, tgclient.ErrSkipped):
			m.File = filepath.ToSlash(filepath.Join(filepath.Base(dir), name))
			files++
		case ctx.Err() != nil:
			return files, ctx.Err()
		default:
			fmt.Fprintf(warn, "warning: message %d: download failed: %s\n", m.ID, output.CleanLine(err.Error()))
		}
	}
	return files, nil
}

// Doc is the JSON export document.
type Doc struct {
	Chat       output.Chat      `json:"chat"`
	ExportedAt time.Time        `json:"exported_at"`
	Since      *time.Time       `json:"since"`
	Until      *time.Time       `json:"until"`
	Messages   []output.Message `json:"messages"`
}

// WriteJSON renders d.
func WriteJSON(w io.Writer, d Doc) error {
	if d.Messages == nil {
		d.Messages = []output.Message{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(d)
}

var mdEsc = strings.NewReplacer(
	`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "(", `\(`, ")", `\)`,
	"<", `\<`, ">", `\>`, "#", `\#`, "|", `\|`, "&", `\&`, "!", `\!`,
)

// mdLine makes untrusted single-line text safe inside a Markdown line.
func mdLine(s string) string { return mdEsc.Replace(output.CleanLine(s)) }

// mdPath percent-encodes a relative path for use as a link target.
func mdPath(p string) string {
	return strings.NewReplacer("(", "%28", ")", "%29").Replace((&url.URL{Path: p}).EscapedPath())
}

// mdQuote puts every line of a message body in a block quote, so text from other
// users cannot start a heading, header line or list at the top level of the file.
func mdQuote(s string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(output.CleanText(s), "\r\n", "\n"), "\r", "\n"), "\n")
	for i, l := range lines {
		l = strings.NewReplacer("<", `\<`, "[", `\[`).Replace(l) // no raw HTML or links
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// WriteMarkdown renders d per spec section 9. All text from Telegram is escaped or quoted.
func WriteMarkdown(w io.Writer, d Doc) error {
	var b strings.Builder
	title := mdLine(d.Chat.Title)
	if title == "" {
		title = fmt.Sprintf("Chat %d", d.Chat.ID)
	}
	if d.Chat.Username != "" {
		title += " (@" + mdLine(d.Chat.Username) + ")"
	}
	fmt.Fprintf(&b, "# %s\n\nExported %s · %d messages", title, d.ExportedAt.Format("2006-01-02 15:04"), len(d.Messages))
	if n := len(d.Messages); n > 0 {
		fmt.Fprintf(&b, " · %s to %s", d.Messages[0].Date.Local().Format("2006-01-02"), d.Messages[n-1].Date.Local().Format("2006-01-02"))
	}
	b.WriteString("\n")
	day := ""
	for _, m := range d.Messages {
		t := m.Date.Local()
		if dd := t.Format("2006-01-02"); dd != day {
			day = dd
			fmt.Fprintf(&b, "\n## %s\n", day)
		}
		name := "Unknown"
		if f := m.From; f != nil {
			if name = mdLine(f.Name); name == "" {
				name = "id " + strconv.FormatInt(f.ID, 10)
			}
		}
		if m.Service {
			fmt.Fprintf(&b, "\n*%s: %s*\n", name, mdLine(m.Text))
			continue
		}
		fmt.Fprintf(&b, "\n**%s** · %s · #%d", name, t.Format("15:04"), m.ID)
		if m.ReplyToID != 0 {
			fmt.Fprintf(&b, " · reply to #%d", m.ReplyToID)
		}
		b.WriteString("\n")
		if m.Text != "" {
			b.WriteString("\n" + mdQuote(m.Text) + "\n")
		}
		if md := m.Media; md != nil {
			label := mdLine(md.Type)
			if md.FileName != "" {
				label = mdLine(md.FileName)
			}
			switch {
			case m.File != "" && md.Type == "photo":
				fmt.Fprintf(&b, "\n![%s](%s)\n", md.Type, mdPath(m.File))
			case m.File != "":
				fmt.Fprintf(&b, "\n[%s](%s)\n", label, mdPath(m.File))
			default:
				desc := mdLine(md.Type)
				if md.FileName != "" {
					desc += ": " + mdLine(md.FileName)
				}
				if md.Size > 0 {
					desc += ", " + output.Size(md.Size)
				}
				fmt.Fprintf(&b, "\n[%s]\n", desc)
			}
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}
