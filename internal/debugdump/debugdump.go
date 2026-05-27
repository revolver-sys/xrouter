package debugdump

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
)

var enabled bool

// Enable turns on debug dumping.
func Enable() { enabled = true }

// Enabled reports whether dumps are enabled.
func Enabled() bool { return enabled }

// EnableFromEnv enables dumps if XROUTER_DEBUG is set to a non-empty value.
func EnableFromEnv() {
	if os.Getenv("XROUTER_DEBUG") != "" {
		enabled = true
	}
}

// Dump prints a readable dump of any value.
// Uses JSON when possible; falls back to type + fmt.
func Dump(name string, v any) {
	if !enabled {
		return
	}

	// Try JSON first (nice for structs, maps, slices).
	if b, err := json.MarshalIndent(v, "", "  "); err == nil {
		fmt.Fprintf(os.Stderr, "\n[DUMP] %s (%s)\n%s\n", name, typeOf(v), string(b))
		return
	}

	// Fallback (for channels, funcs, complex types).
	fmt.Fprintf(os.Stderr, "\n[DUMP] %s (%s)\n%#v\n", name, typeOf(v), v)
}

// DumpJSON forces JSON dump (and reports marshal error if it fails).
func DumpJSON(name string, v any) {
	if !enabled {
		return
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n[DUMP] %s (%s) json marshal error: %v\n", name, typeOf(v), err)
		return
	}
	fmt.Fprintf(os.Stderr, "\n[DUMP] %s (%s)\n%s\n", name, typeOf(v), string(b))
}

func typeOf(v any) string {
	if v == nil {
		return "<nil>"
	}
	return reflect.TypeOf(v).String()
}
