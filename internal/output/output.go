// Package output prints readable or JSON results and errors.
package output

import (
	"encoding/json"
	"fmt"
	"io"
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
