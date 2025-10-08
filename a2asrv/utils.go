package a2asrv

import (
	"iter"
)

// errorToSeq2 is a utility for creating a single (nil, error) element iter.Seq2 from the provided error.
func errorToSeq2[T any](err error) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		yield(zero, err)
	}
}
