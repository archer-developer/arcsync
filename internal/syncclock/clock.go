package syncclock

import "time"

// Clock abstracts time so tests can run without real sleeps.
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) *time.Timer
}

// Real returns a Clock backed by the system clock.
func Real() Clock { return realClock{} }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) *time.Timer {
	return time.AfterFunc(d, f)
}
