//go:build android || ios || !(windows || linux || darwin)

package ntp

import (
	"os"
	"time"
)

func setSystemTime(nowTime time.Time) error {
	return os.ErrInvalid
}
