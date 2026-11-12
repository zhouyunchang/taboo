package rotation

import (
	"testing"
	"time"
)

func TestCronNextAndMinimumGap(t *testing.T) {
	if err := validatePolicy(86400, ""); err != nil {
		t.Fatal(err)
	}
	if err := validatePolicy(60, ""); err == nil {
		t.Fatal("short interval accepted")
	}
	if err := validatePolicy(0, "* * * * *"); err == nil {
		t.Fatal("every minute accepted")
	}
	if err := validatePolicy(0, "*/5 * * * *"); err != nil {
		t.Fatal(err)
	}
	if err := validatePolicy(3600, "0 3 * * *"); err == nil {
		t.Fatal("both schedules accepted")
	}
	s, err := parseCron("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	next, err := s.next(time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC))
	if err != nil || next.Hour() != 3 || next.Minute() != 0 || !next.After(time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)) {
		t.Fatalf("next=%s err=%v", next, err)
	}
	when, err := nextRun(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), 3600, "")
	if err != nil || !when.Equal(time.Date(2026, 11, 1, 1, 0, 0, 0, time.UTC)) {
		t.Fatalf("interval next=%s err=%v", when, err)
	}
}
