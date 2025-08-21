package a2aclient

import "errors"

// ErrNotImplemented is used during the API design stage.
// TODO(yarshevchuk): remove once Client and Transport implementations are in place.
var ErrNotImplemented = errors.New("not implemented")
