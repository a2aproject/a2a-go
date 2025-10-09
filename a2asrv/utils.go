package a2asrv

import (
	"github.com/a2aproject/a2a-go/a2a"
	"iter"
)

// errorToSeq2 is a utility for creating a single (nil, error) element iter.Seq2 from the provided error.
func errorToSeq2(err error) iter.Seq2[a2a.Event, error] {
	return func(yield func(a2a.Event, error) bool) {
		yield(nil, err)
	}
}
