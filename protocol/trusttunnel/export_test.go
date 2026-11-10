package trusttunnel

import "time"

func SetCheckIdleAfter(value time.Duration) (restore func()) {
	previous := checkIdleAfter
	checkIdleAfter = value
	return func() { checkIdleAfter = previous }
}
