package connectip

import (
	goerrors "errors"

	"github.com/sagernet/quic-go/quicvarint"
)

var contextIDZero = quicvarint.Append([]byte{}, 0)

// Go 1.26 errors.AsType, which the module's Go version does not have yet.
func asType[T error](err error) (T, bool) {
	var target T
	ok := goerrors.As(err, &target)
	return target, ok
}
