// Small output helpers shared by the subcommands.
package cli

import (
	"encoding/json"
	"io"
)

// writeJSON pretty-prints v to w with HTML escaping off (paths and commands
// stay readable).
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
