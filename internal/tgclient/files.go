package tgclient

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/telegram/message/unpack"
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
	"github.com/nphuoctho/telegram-cli/internal/config"
	"github.com/nphuoctho/telegram-cli/internal/output"
)

const (
	MaxUploadSize        int64 = 4000 * 512 * 1024 // 2,097,152,000
	MaxUploadSizePremium int64 = 2 * MaxUploadSize
	maxPhotoSize         int64 = 10 << 20
)

// UploadPlan is one validated file to upload.
type UploadPlan struct {
	Path string
	Size int64
	As   string // file, photo or video
}

// SafeName reduces an untrusted file name to a single safe path element.
func SafeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r == 0 || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	// Keep names within the common 255-byte file system limit, cutting on a rune boundary.
	if len(name) > 200 {
		ext := filepath.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		stem := name[:len(name)-len(ext)]
		for len(stem)+len(ext) > 200 {
			_, n := utf8.DecodeLastRuneInString(stem)
			stem = stem[:len(stem)-n]
		}
		name = stem + ext
	}
	if name == "" || name == "." || name == ".." {
		return "file"
	}
	return name
}

// ChooseKind picks how a file is sent (spec section 8.1).
func ChooseKind(path string, size int64, as string) (string, error) {
	switch as {
	case "auto":
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg", ".jpeg", ".png", ".webp":
			if size <= maxPhotoSize {
				return "photo", nil
			}
		}
		return "file", nil
	case "photo":
		if size > maxPhotoSize {
			return "", &config.UsageError{Msg: fmt.Sprintf("%s is over 10 MiB and cannot be sent as a photo", path)}
		}
		return "photo", nil
	case "file", "video":
		return as, nil
	}
	return "", &config.UsageError{Msg: "--as must be auto, file, photo or video"}
}

// PlanUploads validates every path before any network call.
func PlanUploads(paths []string, as string) ([]UploadPlan, error) {
	plans := make([]UploadPlan, 0, len(paths))
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, &config.UsageError{Msg: fmt.Sprintf("cannot upload %s: %v", output.CleanLine(p), errors.Unwrap(err))}
		}
		if !st.Mode().IsRegular() {
			return nil, &config.UsageError{Msg: output.CleanLine(p) + " is not a regular file"}
		}
		if st.Size() == 0 {
			return nil, &config.UsageError{Msg: output.CleanLine(p) + " is empty"}
		}
		if st.Size() > MaxUploadSizePremium {
			return nil, tooLarge(MaxUploadSize, "limit %d MiB, %d MiB for Premium")
		}
		kind, err := ChooseKind(p, st.Size(), as)
		if err != nil {
			return nil, err
		}
		plans = append(plans, UploadPlan{Path: p, Size: st.Size(), As: kind})
	}
	return plans, nil
}

func tooLarge(limit int64, format string) error {
	return &config.UsageError{Msg: fmt.Sprintf("file too large ("+format+")", limit>>20, MaxUploadSizePremium>>20)}
}

// CheckPremiumLimit asks Telegram for the account type, but only when a file
// is over the regular limit.
func CheckPremiumLimit(ctx context.Context, api *tg.Client, plans []UploadPlan) error {
	for _, p := range plans {
		if p.Size <= MaxUploadSize {
			continue
		}
		users, err := api.UsersGetUsers(ctx, []tg.InputUserClass{&tg.InputUserSelf{}})
		if err != nil {
			return err
		}
		if len(users) == 1 {
			if u, ok := users[0].(*tg.User); ok && u.Premium {
				return nil
			}
		}
		return tooLarge(MaxUploadSize, "limit %d MiB")
	}
	return nil
}

// SendFile uploads one planned file and sends it as its own message.
func SendFile(ctx context.Context, api *tg.Client, peer tg.InputPeerClass, p UploadPlan, caption string, progress func(done, total int64)) (int, error) {
	up := uploader.NewUploader(api)
	if progress != nil {
		up = up.WithProgress(progressFunc(progress))
	}
	f, err := up.FromPath(ctx, p.Path)
	if err != nil {
		return 0, err
	}
	var text []message.StyledTextOption
	if caption != "" {
		text = append(text, styling.Plain(caption))
	}
	var media message.MediaOption
	switch p.As {
	case "photo":
		media = message.UploadedPhoto(f, text...)
	default:
		doc := message.UploadedDocument(f, text...).Filename(filepath.Base(p.Path))
		if m := mime.TypeByExtension(filepath.Ext(p.Path)); m != "" {
			doc = doc.MIME(m)
		}
		if p.As == "video" {
			media = doc.Video().SupportsStreaming()
		} else {
			media = doc
		}
	}
	return unpack.MessageID(message.NewSender(api).To(peer).Media(ctx, media))
}

type progressFunc func(done, total int64)

func (f progressFunc) Chunk(_ context.Context, s uploader.ProgressState) error {
	f(s.Uploaded, s.Total)
	return nil
}

// ErrSkipped is returned by DownloadTo when the target already exists.
var ErrSkipped = errors.New("exists")

// DownloadTo downloads loc into target via target+".part". An existing target
// yields ErrSkipped unless overwrite is set. The .part file never outlives a failure.
func DownloadTo(ctx context.Context, api *tg.Client, loc tg.InputFileLocationClass, size int64, target string, mode os.FileMode, overwrite bool, progress func(done, total int64)) (err error) {
	if _, serr := os.Stat(target); serr == nil && !overwrite {
		return ErrSkipped
	}
	part := target + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(part)
		}
	}()
	b := downloader.NewDownloader().Download(api, loc)
	if size > 0 {
		b = b.WithSize(size)
	}
	if progress != nil {
		b = b.WithProgress(progress)
	}
	if _, err = b.Stream(ctx, f); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(part, target)
}
