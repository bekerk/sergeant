package ledger

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
			return Reply{Text: l.t.T(i18n.LedgerStatusForEmpty, c.Target), Ephemeral: true}, nil
		}
		return Reply{Text: l.t.T(i18n.LedgerStatusFor, c.Target, joinAmounts(rows)), Ephemeral: true}, nil

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
			return Reply{Text: l.t.T(i18n.LedgerStatusAllEmpty), Ephemeral: true}, nil
		}
		var b strings.Builder
		if len(owed) > 0 {
			b.WriteString(l.t.T(i18n.LedgerStatusAllOwedHeader))
			writeGroupedLines(&b, l.t, owed, func(d store.Debt) string { return d.Debtor })
		}
		if len(owe) > 0 {
			b.WriteString(l.t.T(i18n.LedgerStatusAllOweHeader))
			writeGroupedLines(&b, l.t, owe, func(d store.Debt) string { return d.Creditor })
		}
		return Reply{Text: b.String(), Ephemeral: true}, nil

	case parser.KindPaySet:
		if err := l.store.SetPaymentMethod(ctx, creditor, c.PayMethod, c.PayValue); err != nil {
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.PaySaved, c.PayMethod), Ephemeral: true}, nil

	case parser.KindPayRemove:
		if err := l.store.RemovePaymentMethod(ctx, creditor, c.PayMethod); err != nil {
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.PayRemoved, c.PayMethod), Ephemeral: true}, nil

	case parser.KindPayClear:
		if err := l.store.ClearPaymentMethods(ctx, creditor); err != nil {
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.PayCleared), Ephemeral: true}, nil

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
			return Reply{Text: l.t.T(i18n.PayShowSelfEmpty), Ephemeral: true}, nil
		}
		return Reply{Text: l.t.T(i18n.PayShowForEmpty, userID), Ephemeral: true}, nil
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
	return Reply{Text: b.String(), Ephemeral: true}, nil
}

func writeGroupedLines(b *strings.Builder, t *i18n.Translator, rows []store.Debt, key func(store.Debt) string) {
	var lastKey string
	var group []store.Debt
	flush := func() {
		if len(group) == 0 {
			return
		}
		b.WriteString(t.T(i18n.LedgerStatusAllLine, lastKey, joinAmounts(group)))
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

func joinAmounts(rows []store.Debt) string {
	parts := make([]string, len(rows))
	for i, r := range rows {
		parts[i] = fmt.Sprintf("%s %s", formatMinor(r.AmountMinor), r.Currency)
	}
	return strings.Join(parts, ", ")
}

func formatMinor(n int64) string {
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%d.%02d", n/100, n%100)
}
