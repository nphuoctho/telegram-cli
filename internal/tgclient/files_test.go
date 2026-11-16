package tgclient

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeName(t *testing.T) {
	long := strings.Repeat("é", 300)
	for in, want := range map[string]string{
		"../../etc/passwd": "passwd",
		"a/b":              "b",
		`a\b`:              "b",
		"a\x00b":           "a_b",
		"x\ny":             "x_y",
		"":                 "file",
		".":                "file",
		"..":               "file",
		"a/":               "file",
		"ok.pdf":           "ok.pdf",
	} {
		require.Equal(t, want, SafeName(in), "%q", in)
	}
	got := SafeName(long)
	require.LessOrEqual(t, len(got), 200)
	require.True(t, strings.HasPrefix(long, got))
}

func TestChooseKind(t *testing.T) {
	const mib = 1 << 20
	for _, tc := range []struct {
		path, as, want string
		size           int64
		err            bool
	}{
		{"a.jpg", "auto", "photo", 10 * mib, false},
		{"a.JPEG", "auto", "photo", 1, false},
		{"a.png", "auto", "photo", 1, false},
		{"a.webp", "auto", "photo", 1, false},
		{"a.jpg", "auto", "file", 12 * mib, false},
		{"a.pdf", "auto", "file", 1, false},
		{"a.jpg", "photo", "", 12 * mib, true},
		{"a.pdf", "photo", "photo", 1, false},
		{"a.jpg", "file", "file", 1, false},
		{"a.mp4", "video", "video", 1, false},
		{"a.mp4", "gif", "", 1, true},
	} {
		got, err := ChooseKind(tc.path, tc.size, tc.as)
		require.Equal(t, tc.err, err != nil, "%+v", tc)
		require.Equal(t, tc.want, got, "%+v", tc)
	}
}

func TestSizeLimits(t *testing.T) {
	require.EqualValues(t, 2_097_152_000, MaxUploadSize)
	require.EqualValues(t, 4_194_304_000, MaxUploadSizePremium)
}
