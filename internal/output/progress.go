package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

const progressInterval = 100 * time.Millisecond

// Progress draws one updating line on a terminal and stays silent otherwise
// (spec section 8.3).
type Progress struct {
	W    io.Writer
	Name string
	TTY  bool
	Now  func() time.Time // nil = time.Now

	last time.Time
}

// NewProgress writes to w, drawing only when w is a terminal.
func NewProgress(w io.Writer, name string) *Progress {
	f, ok := w.(*os.File)
	return &Progress{W: w, Name: name, TTY: ok && term.IsTerminal(int(f.Fd()))}
}

func (p *Progress) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

// Update redraws the line at most 10 times per second; the final state (done >= total) always draws.
func (p *Progress) Update(done, total int64) {
	if !p.TTY {
		return
	}
	t := p.now()
	if done < total && !p.last.IsZero() && t.Sub(p.last) < progressInterval {
		return
	}
	p.last = t
	fmt.Fprintf(p.W, "\r\x1b[K%s", p.line(done, total))
}

// Done ends the line so later output starts fresh.
func (p *Progress) Done() {
	if p.TTY && !p.last.IsZero() {
		fmt.Fprint(p.W, "\r\x1b[K")
	}
}

func (p *Progress) line(done, total int64) string {
	const width = 20
	if total <= 0 {
		return fmt.Sprintf("%s  %s", p.Name, Size(done))
	}
	done = min(max(done, 0), total)
	n := int(done * width / total)
	bar := strings.Repeat("=", n)
	if n < width {
		bar += ">" + strings.Repeat(" ", width-n-1)
	}
	return fmt.Sprintf("%s  [%s]  %s / %s  %d%%", p.Name, bar, Size(done), Size(total), done*100/total)
}
