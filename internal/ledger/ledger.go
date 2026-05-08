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
		// Check all currencies for net debt with this person
		rows, err := l.store.ListPair(ctx, creditor, c.Target)
		if err != nil {
			return Reply{}, err
		}
		// Also check reverse direction for debt simplification
		reverseRows, err := l.store.ListPair(ctx, c.Target, creditor)
		if err != nil {
			return Reply{}, err
		}

		if len(rows) == 0 && len(reverseRows) == 0 {
			return Reply{Text: l.t.T(i18n.LedgerStatusForEmpty, c.Target)}, nil
		}

		// Build net amounts per currency
		netAmounts := make(map[string]int64)
		for _, r := range rows {
			netAmounts[r.Currency] += r.AmountMinor
		}
		for _, r := range reverseRows {
			netAmounts[r.Currency] -= r.AmountMinor
		}

		// Convert to display format
		var displayDebts []store.Debt
		for currency, amount := range netAmounts {
			if amount != 0 {
				displayDebts = append(displayDebts, store.Debt{
					Creditor:    creditor,
					Debtor:      c.Target,
					Currency:    currency,
					AmountMinor: amount,
				})
			}
		}

		if len(displayDebts) == 0 {
			return Reply{Text: l.t.T(i18n.LedgerStatusForEmpty, c.Target)}, nil
		}
		return Reply{Text: l.t.T(i18n.LedgerStatusFor, c.Target, joinNetDebtsForStatus(displayDebts))}, nil

	case parser.KindStatusAll:
		owed, owe, err := l.store.ListNormalized(ctx, creditor)
		if err != nil {
			return Reply{}, err
		}
		if len(owed) == 0 && len(owe) == 0 {
			return Reply{Text: l.t.T(i18n.LedgerStatusAllEmpty)}, nil
		}

		creditorPay := map[string]*store.PaymentMethod{}
		for _, r := range owe {
			if _, seen := creditorPay[r.OtherUser]; seen {
				continue
			}
			pm, ok, err := l.store.DefaultPaymentMethod(ctx, r.OtherUser)
			if err != nil {
				return Reply{}, err
			}
			if ok {
				creditorPay[r.OtherUser] = &pm
			} else {
				creditorPay[r.OtherUser] = nil
			}
		}

		now := time.Now().Unix()
		var b strings.Builder
		if len(owed) > 0 {
			b.WriteString(l.t.T(i18n.LedgerStatusAllOwedHeader))
			writeNetDebtLines(&b, l.t, now, owed, nil)
		}
		if len(owe) > 0 {
			if len(owed) > 0 {
				b.WriteString("\n")
			}
			b.WriteString(l.t.T(i18n.LedgerStatusAllOweHeader))
			writeNetDebtLines(&b, l.t, now, owe, creditorPay)
		}
		return Reply{Text: b.String()}, nil

	case parser.KindPaySet:
		if err := l.store.SetPaymentMethod(ctx, creditor, c.PayMethod, c.PayValue); err != nil {
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.PaySaved, c.PayMethod)}, nil

	case parser.KindPaySetDefault:
		if err := l.store.SetDefaultPaymentMethod(ctx, creditor, c.PayMethod); err != nil {
			if errors.Is(err, store.ErrUnknownMethod) {
				return Reply{Text: l.t.T(i18n.PayDefaultMissing, c.PayMethod)}, nil
			}
			return Reply{}, err
		}
		return Reply{Text: l.t.T(i18n.PayDefaultSet, c.PayMethod)}, nil

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

	b.WriteString("<@" + userID + "> :money_with_wings: ")

	for _, pm := range pms {
		id := i18n.PayLine
		if pm.IsDefault {
			id = i18n.PayLineDefault
		}
		b.WriteString(l.t.T(id, pm.Method, pm.Value))
	}
	return Reply{Text: b.String()}, nil
}

func writeNetDebtLines(b *strings.Builder, tr *i18n.Translator, now int64, rows []store.NetDebt, payByKey map[string]*store.PaymentMethod) {
	var lastKey string
	var group []store.NetDebt
	flush := func() {
		if len(group) == 0 {
			return
		}
		b.WriteString(tr.T(i18n.LedgerStatusAllLine, lastKey, joinNetAmounts(group, tr, payByKey[lastKey])))
	}
	for _, r := range rows {
		k := r.OtherUser
		if k != lastKey {
			flush()
			lastKey = k
			group = group[:0]
		}
		group = append(group, r)
	}
	flush()
}

func joinNetDebtsForStatus(rows []store.Debt) string {
	parts := make([]string, len(rows))
	for i, r := range rows {
		amount := formatMinor(r.AmountMinor)
		if r.AmountMinor < 0 {
			amount = formatMinor(-r.AmountMinor)
		}
		parts[i] = fmt.Sprintf("%s %s", amount, r.Currency)
	}
	return strings.Join(parts, ", ")
}

func joinNetAmounts(rows []store.NetDebt, tr *i18n.Translator, pay *store.PaymentMethod) string {
	parts := make([]string, len(rows))
	for i, r := range rows {
		amount := formatMinor(r.AmountMinor)
		if pay != nil {
			parts[i] = fmt.Sprintf("%s %s (`%s %s`)", amount, r.Currency, pay.Method, pay.Value)
		} else {
			parts[i] = fmt.Sprintf("%s %s", amount, r.Currency)
		}
	}
	return strings.Join(parts, ", ")
}

func formatMinor(n int64) string {
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%d.%02d", n/100, n%100)
}
