package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProgressThrottleAndLine(t *testing.T) {
	var b bytes.Buffer
	now := time.Unix(0, 0)
	p := &Progress{W: &b, Name: "cat.jpg", TTY: true, Now: func() time.Time { return now }}
	p.Update(1, 100)
	first := b.String()
	require.Contains(t, first, "cat.jpg  [>                   ]  1 B / 100 B  1%")
	now = now.Add(50 * time.Millisecond)
	p.Update(50, 100)
	require.Equal(t, first, b.String(), "throttled")
	now = now.Add(60 * time.Millisecond)
	p.Update(51, 100)
	require.Contains(t, b.String(), "51%")
	p.Update(100, 100) // final state is never throttled
	require.Contains(t, b.String(), "[====================]  100 B / 100 B  100%")
}

func TestProgressSilentWithoutTerminal(t *testing.T) {
	var b bytes.Buffer
	p := NewProgress(&b, "x")
	p.Update(1, 2)
	p.Done()
	require.Empty(t, b.String())
}

func TestProgressThrottleUnknownTotal(t *testing.T) {
	var b bytes.Buffer
	p := &Progress{W: &b, Name: "x", TTY: true, Now: func() time.Time { return time.Unix(0, 0) }}
	p.Update(1, 0)
	first := b.String()
	p.Update(2, 0)
	require.Equal(t, first, b.String())
}

func TestProgressNameShortenedAndCleaned(t *testing.T) {
	var b bytes.Buffer
	p := NewProgress(&b, "a\x1bb"+strings.Repeat("é", 100))
	require.LessOrEqual(t, len([]rune(p.Name)), 30)
	require.NotContains(t, p.Name, "\x1b")
	require.True(t, strings.HasSuffix(p.Name, "…"))
}
