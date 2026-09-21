package webhooks

import "testing"

func TestRetryKeepsPending(t *testing.T) {
	st, next := retryAfterFail(1)
	if st != "pending" || next <= 0 {
		t.Fatalf("first fail should stay pending, got %s next=%d", st, next)
	}
	st, next = retryAfterFail(maxAttempts)
	if st != "dead" || next != 0 {
		t.Fatalf("max attempts should dead, got %s next=%d", st, next)
	}
}
