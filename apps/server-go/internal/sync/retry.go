package sync

import "time"

func retryAfterFail(attempts int) (status string, nextAt int64) {
	if attempts >= maxAttempts {
		return "dead", 0
	}
	idx := attempts - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(backoffSec) {
		idx = len(backoffSec) - 1
	}
	return "pending", time.Now().Unix() + int64(backoffSec[idx])
}
