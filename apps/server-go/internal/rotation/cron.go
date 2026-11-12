package rotation

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	minInterval = 300
	maxInterval = 366 * 24 * 3600
)

type schedule struct {
	minute, hour, dom, month, dow cronField
}

type cronField struct {
	any  bool
	bits [64]bool
}

func validatePolicy(interval int, cron string) error {
	cron = strings.TrimSpace(cron)
	if cron != "" && interval != 0 {
		return fmt.Errorf("set interval_seconds or cron, not both")
	}
	if cron == "" {
		if interval < minInterval || interval > maxInterval {
			return fmt.Errorf("interval_seconds out of range")
		}
		return nil
	}
	s, err := parseCron(cron)
	if err != nil {
		return err
	}
	first, err := s.next(time.Now().UTC())
	if err != nil {
		return err
	}
	second, err := s.next(first)
	if err != nil {
		return err
	}
	if second.Sub(first) < 5*time.Minute {
		return fmt.Errorf("cron fires more often than every 5 minutes")
	}
	return nil
}

func nextRun(after time.Time, interval int, cron string) (time.Time, error) {
	cron = strings.TrimSpace(cron)
	if cron == "" {
		if interval < minInterval {
			return time.Time{}, fmt.Errorf("interval_seconds out of range")
		}
		return after.UTC().Add(time.Duration(interval) * time.Second), nil
	}
	s, err := parseCron(cron)
	if err != nil {
		return time.Time{}, err
	}
	return s.next(after.UTC())
}

func parseCron(cron string) (schedule, error) {
	parts := strings.Fields(cron)
	if len(parts) != 5 {
		return schedule{}, fmt.Errorf("cron must be five fields")
	}
	var s schedule
	var err error
	if s.minute, err = parseField(parts[0], 0, 59); err != nil {
		return schedule{}, err
	}
	if s.hour, err = parseField(parts[1], 0, 23); err != nil {
		return schedule{}, err
	}
	if s.dom, err = parseField(parts[2], 1, 31); err != nil {
		return schedule{}, err
	}
	if s.month, err = parseField(parts[3], 1, 12); err != nil {
		return schedule{}, err
	}
	if s.dow, err = parseField(parts[4], 0, 7); err != nil {
		return schedule{}, err
	}
	if s.dow.bits[7] {
		s.dow.bits[0] = true
	}
	return s, nil
}

func parseField(spec string, min, max int) (cronField, error) {
	var f cronField
	if spec == "*" {
		f.any = true
		for i := min; i <= max && i < len(f.bits); i++ {
			f.bits[i] = true
		}
		return f, nil
	}
	for _, part := range strings.Split(spec, ",") {
		step := 1
		rangeSpec := part
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return cronField{}, fmt.Errorf("bad cron step")
			}
			step = n
			rangeSpec = part[:i]
		}
		start, end := min, max
		switch {
		case rangeSpec == "*":
		case strings.Contains(rangeSpec, "-"):
			a, b, ok := strings.Cut(rangeSpec, "-")
			lo, e1 := strconv.Atoi(a)
			hi, e2 := strconv.Atoi(b)
			if !ok || e1 != nil || e2 != nil || lo < min || hi > max || lo > hi {
				return cronField{}, fmt.Errorf("bad cron range")
			}
			start, end = lo, hi
		default:
			n, err := strconv.Atoi(rangeSpec)
			if err != nil || n < min || n > max {
				return cronField{}, fmt.Errorf("bad cron value")
			}
			start, end = n, n
		}
		for i := start; i <= end; i += step {
			f.bits[i] = true
		}
	}
	return f, nil
}

func (s schedule) next(after time.Time) (time.Time, error) {
	t := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(366 * 24 * time.Hour)
	for !t.After(limit) {
		if s.matches(t) {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("cron has no occurrence within a year")
}

func (s schedule) matches(t time.Time) bool {
	if !s.minute.bits[t.Minute()] || !s.hour.bits[t.Hour()] || !s.month.bits[int(t.Month())] {
		return false
	}
	dom := s.dom.any || s.dom.bits[t.Day()]
	dow := s.dow.any || s.dow.bits[int(t.Weekday())]
	if s.dom.any {
		return dow
	}
	if s.dow.any {
		return dom
	}
	return dom || dow
}

// nextAfter advances an interval schedule in whole periods until it is after
// now, preserving the original phase. A cron schedule is absolute, so it is
// recomputed from now.
func nextAfter(due time.Time, interval int, cron string, now time.Time) (time.Time, error) {
	if strings.TrimSpace(cron) != "" {
		return nextRun(now, 0, cron)
	}
	if interval < minInterval {
		return time.Time{}, fmt.Errorf("interval_seconds out of range")
	}
	next := due.Add(time.Duration(interval) * time.Second)
	for !next.After(now) {
		next = next.Add(time.Duration(interval) * time.Second)
	}
	return next, nil
}
