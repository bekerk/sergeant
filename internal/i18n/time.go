package i18n

import (
	"fmt"
	"time"
)

func (t *Translator) Since(now, then int64) string {
	diff := now - then
	if diff < 0 {
		diff = 0
	}
	switch t.locale {
	case "pl":
		return polishSince(diff, then)
	default:
		return englishSince(diff, then)
	}
}

const (
	secondsInMinute = 60
	secondsInHour   = 60 * secondsInMinute
	secondsInDay    = 24 * secondsInHour
	secondsInWeek   = 7 * secondsInDay
)

func englishSince(diff, then int64) string {
	switch {
	case diff < secondsInMinute:
		return "just now"
	case diff < secondsInHour:
		n := diff / secondsInMinute
		return fmt.Sprintf("%d %s ago", n, enPlural(n, "minute", "minutes"))
	case diff < secondsInDay:
		n := diff / secondsInHour
		return fmt.Sprintf("%d %s ago", n, enPlural(n, "hour", "hours"))
	case diff < secondsInWeek:
		n := diff / secondsInDay
		return fmt.Sprintf("%d %s ago", n, enPlural(n, "day", "days"))
	default:
		return time.Unix(then, 0).UTC().Format("2006-01-02")
	}
}

func polishSince(diff, then int64) string {
	switch {
	case diff < secondsInMinute:
		return "przed chwilą"
	case diff < secondsInHour:
		n := diff / secondsInMinute
		return fmt.Sprintf("%d %s temu", n, plPlural(n, "minutę", "minuty", "minut"))
	case diff < secondsInDay:
		n := diff / secondsInHour
		return fmt.Sprintf("%d %s temu", n, plPlural(n, "godzinę", "godziny", "godzin"))
	case diff < secondsInWeek:
		n := diff / secondsInDay
		return fmt.Sprintf("%d %s temu", n, plPlural(n, "dzień", "dni", "dni"))
	default:
		return time.Unix(then, 0).UTC().Format("2006-01-02")
	}
}

func enPlural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func plPlural(n int64, one, few, many string) string {
	if n == 1 {
		return one
	}
	lastDigit := n % 10
	lastTwo := n % 100
	if lastDigit >= 2 && lastDigit <= 4 && (lastTwo < 12 || lastTwo > 14) {
		return few
	}
	return many
}
