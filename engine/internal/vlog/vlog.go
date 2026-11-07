// Package vlog is verbose engine tracing (per-track and per-request detail).
// It prints only when ADAM_ENGINE_DEBUG is set, so normal runs keep the
// terminal to events and errors.
package vlog

import (
	"fmt"
	"log"
	"os"
)

var enabled = os.Getenv("ADAM_ENGINE_DEBUG") != ""

// Printf logs like log.Printf when verbose tracing is enabled.
func Printf(format string, args ...any) {
	if enabled {
		_ = log.Output(2, fmt.Sprintf(format, args...))
	}
}
