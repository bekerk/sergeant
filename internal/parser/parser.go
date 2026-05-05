package parser

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

type Kind int

const (
	KindUnknown Kind = iota
	KindAdd
	KindReset
	KindStatusFor
	KindStatusAll
	KindPaySet
	KindPaySetForm
	KindPayRemove
	KindPayClear
	KindPayShowSelf
	KindPayShowFor
)

type Command struct {
	Kind     Kind
	Target   string
	Sign     int   // +1 or -1, set on KindAdd
	Minor    int64 // amount in 1/100 of major unit, set on KindAdd
	Currency string
	// Payment-method commands.
	PayMethod string // e.g. "bank", "blik", "revolut" - lowercase, [a-z0-9-]
	PayValue  string // free-form, trimmed
}

var ErrUnrecognized = errors.New("unrecognized command")

var (
	mentionRE = regexp.MustCompile(`^<@([A-Z0-9]+)(?:\|[^>]*)?>`)
	amountRE  = regexp.MustCompile(`^([+-])(\d+(?:\.\d{1,2})?)$`)
)

func Parse(text string) (Command, error) {
	text = strings.TrimSpace(text)
	if text == "?" || strings.EqualFold(text, "status") {
		return Command{Kind: KindStatusAll}, nil
	}

	// Pay commands without a leading user mention: pay / pay set / pay rm / pay clear.
	if fields := strings.Fields(text); len(fields) > 0 && strings.EqualFold(fields[0], "pay") {
		return parsePay(fields)
	}

	m := mentionRE.FindStringSubmatch(text)
	if m == nil {
		return Command{}, ErrUnrecognized
	}
	target := m[1]
	fields := strings.Fields(text[len(m[0]):])
	if len(fields) == 0 {
		return Command{}, ErrUnrecognized
	}

	switch first := fields[0]; {
	case first == "?" || strings.EqualFold(first, "status"):
		if len(fields) != 1 {
			return Command{}, ErrUnrecognized
		}
		return Command{Kind: KindStatusFor, Target: target}, nil

	case strings.EqualFold(first, "pay"):
		if len(fields) != 1 {
			return Command{}, ErrUnrecognized
		}
		return Command{Kind: KindPayShowFor, Target: target}, nil

	case strings.EqualFold(first, "reset"):
		if len(fields) > 2 {
			return Command{}, ErrUnrecognized
		}
		c := Command{Kind: KindReset, Target: target}
		if len(fields) == 2 {
			ccy, err := NormalizeCurrency(fields[1])
			if err != nil {
				return Command{}, ErrUnrecognized
			}
			c.Currency = ccy
		}
		return c, nil

	default:
		if len(fields) > 2 {
			return Command{}, ErrUnrecognized
		}
		am := amountRE.FindStringSubmatch(fields[0])
		if am == nil {
			return Command{}, ErrUnrecognized
		}
		minor, err := parseMinor(am[2])
		if err != nil {
			return Command{}, ErrUnrecognized
		}
		sign := 1
		if am[1] == "-" {
			sign = -1
		}
		c := Command{Kind: KindAdd, Target: target, Sign: sign, Minor: minor}
		if len(fields) == 2 {
			ccy, err := NormalizeCurrency(fields[1])
			if err != nil {
				return Command{}, ErrUnrecognized
			}
			c.Currency = ccy
		}
		return c, nil
	}
}

// parsePay handles the no-target pay forms:
//
//	pay                          → KindPayShowSelf
//	pay set <method> <value...>  → KindPaySet
//	pay rm <method>              → KindPayRemove
//	pay clear                    → KindPayClear
func parsePay(fields []string) (Command, error) {
	if len(fields) == 1 {
		return Command{Kind: KindPayShowSelf}, nil
	}
	switch sub := strings.ToLower(fields[1]); sub {
	case "set":
		if len(fields) == 2 {
			// Bare `pay set` → handler should open the modal.
			return Command{Kind: KindPaySetForm}, nil
		}
		if len(fields) < 4 {
			return Command{}, ErrUnrecognized
		}
		method, ok := normalizeMethod(fields[2])
		if !ok {
			return Command{}, ErrUnrecognized
		}
		value := strings.TrimSpace(strings.Join(fields[3:], " "))
		if value == "" || len(value) > 200 {
			return Command{}, ErrUnrecognized
		}
		return Command{Kind: KindPaySet, PayMethod: method, PayValue: value}, nil
	case "rm", "remove":
		if len(fields) != 3 {
			return Command{}, ErrUnrecognized
		}
		method, ok := normalizeMethod(fields[2])
		if !ok {
			return Command{}, ErrUnrecognized
		}
		return Command{Kind: KindPayRemove, PayMethod: method}, nil
	case "clear":
		if len(fields) != 2 {
			return Command{}, ErrUnrecognized
		}
		return Command{Kind: KindPayClear}, nil
	}
	return Command{}, ErrUnrecognized
}

// normalizeMethod returns a lowercase method label or false if invalid.
// Allowed: 1-20 chars of [a-z0-9-].
func normalizeMethod(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) == 0 || len(s) > 20 {
		return "", false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return "", false
		}
	}
	return s, true
}

func NormalizeCurrency(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) != 3 {
		return "", errors.New("invalid currency")
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return "", errors.New("invalid currency")
		}
	}
	return s, nil
}

func parseMinor(s string) (int64, error) {
	intPart, fracPart, hasFrac := strings.Cut(s, ".")
	whole, err := strconv.ParseInt(intPart, 10, 64)
	if err != nil || whole < 0 {
		return 0, errors.New("bad amount")
	}
	if !hasFrac {
		return whole * 100, nil
	}
	if len(fracPart) == 0 || len(fracPart) > 2 {
		return 0, errors.New("bad amount")
	}
	frac, err := strconv.ParseInt(fracPart, 10, 64)
	if err != nil || frac < 0 {
		return 0, errors.New("bad amount")
	}
	if len(fracPart) == 1 {
		frac *= 10
	}
	return whole*100 + frac, nil
}
