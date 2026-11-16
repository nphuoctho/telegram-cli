// Package output prints readable or JSON results and errors.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gotd/td/tg"
)

// JSON writes v as one indented-free JSON document followed by a newline.
func JSON(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }

// Error prints an error as "error: msg" or as the JSON error object.
func Error(w io.Writer, asJSON bool, code, msg string) {
	if asJSON {
		_ = JSON(w, map[string]any{"error": map[string]string{"code": code, "message": msg}})
		return
	}
	fmt.Fprintf(w, "error: %s\n", msg)
}

// Size formats bytes with binary units.
func Size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 4; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

// User is the shared JSON user object.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	Phone     string `json:"phone,omitempty"`
	Bot       bool   `json:"bot"`
	Premium   bool   `json:"premium"`
}

// NewUser converts a tg.User; the phone is included only when withPhone is set (own account).
func NewUser(u *tg.User, withPhone bool) User {
	v := User{ID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Username: u.Username, Bot: u.Bot, Premium: u.Premium}
	if withPhone {
		v.Phone = u.Phone
	}
	return v
}

// Name is "First Last".
func (u User) Name() string { return strings.TrimSpace(u.FirstName + " " + u.LastName) }

// Summary is "Alice (@alice, id 123)".
func (u User) Summary() string {
	first := u.FirstName
	if first == "" {
		first = u.Name()
	}
	if u.Username != "" {
		return fmt.Sprintf("%s (@%s, id %d)", first, u.Username, u.ID)
	}
	return fmt.Sprintf("%s (id %d)", first, u.ID)
}

// PrintUser writes the readable `me` block.
func PrintUser(w io.Writer, u User) {
	premium := "no"
	if u.Premium {
		premium = "yes"
	}
	fmt.Fprintf(w, "ID:        %d\nName:      %s\n", u.ID, u.Name())
	if u.Username != "" {
		fmt.Fprintf(w, "Username:  @%s\n", u.Username)
	}
	if u.Phone != "" {
		fmt.Fprintf(w, "Phone:     +%s\n", strings.TrimPrefix(u.Phone, "+"))
	}
	fmt.Fprintf(w, "Premium:   %s\n", premium)
}
