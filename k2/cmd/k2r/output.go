//go:build linux

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

type output struct {
	w    io.Writer
	json bool
}

var out = &output{w: os.Stdout}

func (o *output) printResult(msg string) {
	if o.json {
		return
	}
	fmt.Fprintln(o.w, msg)
}

func (o *output) printf(format string, args ...any) {
	if o.json {
		return
	}
	fmt.Fprintf(o.w, format, args...)
}

func (o *output) ok(data any) {
	if !o.json {
		return
	}
	json.NewEncoder(o.w).Encode(map[string]any{"ok": true, "data": data})
}

func (o *output) fail(code, message, hint string) {
	if !o.json {
		fmt.Fprintf(os.Stderr, "Error: %s\n", message)
		if hint != "" {
			fmt.Fprintf(os.Stderr, "  %s\n", hint)
		}
		return
	}
	json.NewEncoder(o.w).Encode(map[string]any{
		"ok": false, "error": message, "code": code, "hint": hint,
	})
}
