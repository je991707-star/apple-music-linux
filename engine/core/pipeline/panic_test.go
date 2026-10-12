package pipeline

import (
	"context"
	"io"
	"strings"
	"testing"
)

type panicSource struct{}

func (panicSource) Stream(context.Context, io.Writer) error { panic("slice bounds out of range") }

type panicStage struct{}

func (panicStage) Process(context.Context, io.Reader, io.Writer) error { panic("bad box") }

type copyStage struct{}

func (copyStage) Process(_ context.Context, r io.Reader, w io.Writer) error {
	_, err := io.Copy(w, r)
	return err
}

type okSource struct{}

func (okSource) Stream(_ context.Context, w io.Writer) error {
	_, err := w.Write([]byte("audio"))
	return err
}

// A malformed fragment that makes a parser panic must fail that one stream,
// not crash the engine — including when the source runs in its own goroutine.
func TestRunTurnsPanicsIntoErrors(t *testing.T) {
	for name, st := range map[string]*Stream{
		"source alone":         {Source: panicSource{}},
		"source behind stages": {Source: panicSource{}, Stages: []Stage{copyStage{}, copyStage{}}},
		"stage":                {Source: okSource{}, Stages: []Stage{panicStage{}}},
	} {
		err := Run(context.Background(), st, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "panicked") {
			t.Errorf("%s: got %v, want a panic turned into an error", name, err)
		}
	}
}
