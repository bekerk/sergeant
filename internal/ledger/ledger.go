package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"sergeant/internal/i18n"
	"sergeant/internal/parser"
	"sergeant/internal/store"
)

type Reply struct {
	Text      string
	Ephemeral bool
}

type Ledger struct {
	store           *store.SQLite
	defaultCurrency string
	t               *i18n.Translator
}

func New(s *store.SQLite, defaultCurrency string, tr *i18n.Translator) *Ledger {
	return &Ledger{store: s, defaultCurrency: defaultCurrency, t: tr}
}

var ErrSelfTarget = errors.New("creditor and debtor cannot be the same")

func (l *Ledger) Apply(ctx context.Context, creditor string, c parser.Command) (Reply, error) {
	switch c.Kind {
	case parser.KindAdd:
		if c.Target == creditor {
			return Reply{}, ErrSelfTarget
		}
		ccy := c.Currency
		if ccy == "" {
			ccy = l.defaultCurrency
		}
		after, err := l.store.AddDelta(ctx, creditor, c.Target, ccy, c.Minor*int64(c.Sign))
		if err != nil {
			return Reply{}, err
		}
		if after == 0 {
			return Reply{Text: l.t.T(i18n.LedgerTabCleared, c.Target, ccy)}, nil
		}
		return Reply{Text: l.t.T(i18n.LedgerNowOwes, c.Target, formatMinor(after), ccy)}, nil

	case parser.KindReset:
		if c.Target == creditor {
			return Reply{}, ErrSelfTarget
		}
		if c.Currency == "" {
			if err := l.store.ResetPair(ctx, creditor, c.Target); err != nil {
				return Reply{}, err
			}
			return Reply{Text: l.t.T(i18n.LedgerResetAll, c.Target)}, nil
		}
		if err := l.store.ResetPairCurrency(ctx, creditor, c.Target, c.Currency); err != nil {
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.LedgerResetCurrency, c.Target, c.Currency)}, nil

	case parser.KindStatusFor:
		rows, err := l.store.ListPair(ctx, creditor, c.Target)
		if err != nil {
			return Reply{}, err
		}
		if len(rows) == 0 {
			return Reply{Text: l.t.T(i18n.LedgerStatusForEmpty, c.Target)}, nil
		}
		return Reply{Text: l.t.T(i18n.LedgerStatusFor, c.Target, joinAmounts(rows, l.t, time.Now().Unix()))}, nil

	case parser.KindStatusAll:
		owed, err := l.store.ListByCreditor(ctx, creditor)
		if err != nil {
			return Reply{}, err
		}
		owe, err := l.store.ListByDebtor(ctx, creditor)
		if err != nil {
			return Reply{}, err
		}
		if len(owed) == 0 && len(owe) == 0 {
			return Reply{Text: l.t.T(i18n.LedgerStatusAllEmpty)}, nil
		}
		now := time.Now().Unix()
		var b strings.Builder
		if len(owed) > 0 {
			b.WriteString(l.t.T(i18n.LedgerStatusAllOwedHeader))
			writeGroupedLines(&b, l.t, now, owed, func(d store.Debt) string { return d.Debtor })
		}
		if len(owe) > 0 {
			b.WriteString(l.t.T(i18n.LedgerStatusAllOweHeader))
			writeGroupedLines(&b, l.t, now, owe, func(d store.Debt) string { return d.Creditor })
		}
		return Reply{Text: b.String()}, nil

	case parser.KindPaySet:
		if err := l.store.SetPaymentMethod(ctx, creditor, c.PayMethod, c.PayValue); err != nil {
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.PaySaved, c.PayMethod)}, nil

	case parser.KindPayRemove:
		if err := l.store.RemovePaymentMethod(ctx, creditor, c.PayMethod); err != nil {
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.PayRemoved, c.PayMethod)}, nil

	case parser.KindPayClear:
		if err := l.store.ClearPaymentMethods(ctx, creditor); err != nil {
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.PayCleared)}, nil

	case parser.KindPayShowSelf:
		return l.renderPay(ctx, creditor, true)

	case parser.KindPayShowFor:
		return l.renderPay(ctx, c.Target, false)

	default:
		return Reply{}, errors.New("unknown command kind")
	}
}

func (l *Ledger) renderPay(ctx context.Context, userID string, isSelf bool) (Reply, error) {
	pms, err := l.store.ListPaymentMethods(ctx, userID)
	if err != nil {
		return Reply{}, err
	}
	if len(pms) == 0 {
		if isSelf {
			return Reply{Text: l.t.T(i18n.PayShowSelfEmpty)}, nil
		}
		return Reply{Text: l.t.T(i18n.PayShowForEmpty, userID)}, nil
	}
	var b strings.Builder
	if isSelf {
		b.WriteString(l.t.T(i18n.PayShowSelfHead))
	} else {
		b.WriteString(l.t.T(i18n.PayShowForHead, userID))
	}
	for _, pm := range pms {
		b.WriteString(l.t.T(i18n.PayLine, pm.Method, pm.Value))
	}
	return Reply{Text: b.String()}, nil
}

func writeGroupedLines(b *strings.Builder, t *i18n.Translator, now int64, rows []store.Debt, key func(store.Debt) string) {
	var lastKey string
	var group []store.Debt
	flush := func() {
		if len(group) == 0 {
			return
		}
		b.WriteString(t.T(i18n.LedgerStatusAllLine, lastKey, joinAmounts(group, t, now)))
	}
	for _, r := range rows {
		k := key(r)
		if k != lastKey {
			flush()
			lastKey = k
			group = group[:0]
		}
		group = append(group, r)
	}
	flush()
}

func joinAmounts(rows []store.Debt, t *i18n.Translator, now int64) string {
	parts := make([]string, len(rows))
	for i, r := range rows {
		parts[i] = fmt.Sprintf("%s %s (%s)", formatMinor(r.AmountMinor), r.Currency, t.Since(now, r.InsertedAt))
	}
	return strings.Join(parts, ", ")
}

func formatMinor(n int64) string {
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%d.%02d", n/100, n%100)
}
