package post

import "errors"

// ErrInvalidPost is returned by New, Restore and Record for data that breaks an invariant.
var ErrInvalidPost = errors.New("invalid lesson post")
