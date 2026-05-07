package ledger

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sergeant/internal/i18n"
	"sergeant/internal/parser"
	"sergeant/internal/store"
)

func newLedger(t *testing.T) *Ledger { return newLedgerLocale(t, "en") }

func newLedgerLocale(t *testing.T, locale string) *Ledger {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return New(s, "PLN", i18n.New(locale))
}

func add(t *testing.T, l *Ledger, creditor, target string, sign int, minor int64, ccy string) Reply {
	t.Helper()
	r, err := l.Apply(context.Background(), creditor, parser.Command{
		Kind: parser.KindAdd, Target: target, Sign: sign, Minor: minor, Currency: ccy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLedger(t *testing.T) {
	t.Run("add accumulates and uses default currency", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "")
		r := add(t, l, "A", "B", 1, 500, "")
		if !strings.Contains(r.Text, "25.00 PLN") {
			t.Fatalf("got %q", r.Text)
		}
	})

	t.Run("subtract clamps at zero", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 1000, "PLN")
		r := add(t, l, "A", "B", -1, 9999, "PLN")
		if !strings.Contains(r.Text, "Tab cleared") {
			t.Fatalf("got %q", r.Text)
		}
	})

	t.Run("self-target rejected", func(t *testing.T) {
		l := newLedger(t)
		_, err := l.Apply(context.Background(), "A", parser.Command{
			Kind: parser.KindAdd, Target: "A", Sign: 1, Minor: 100, Currency: "PLN",
		})
		if !errors.Is(err, ErrSelfTarget) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("reset variants", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 100, "PLN")
		add(t, l, "A", "B", 1, 100, "EUR")

		_, err := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindReset, Target: "B", Currency: "EUR"})
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := l.store.ListPair(context.Background(), "A", "B")
		if len(rows) != 1 || rows[0].Currency != "PLN" {
			t.Fatalf("after reset EUR: %v", rows)
		}

		_, err = l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindReset, Target: "B"})
		if err != nil {
			t.Fatal(err)
		}
		rows, _ = l.store.ListPair(context.Background(), "A", "B")
		if len(rows) != 0 {
			t.Fatalf("after reset all: %v", rows)
		}
	})

	t.Run("status-for empty and populated", func(t *testing.T) {
		l := newLedger(t)
		empty, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusFor, Target: "B"})
		if empty.Ephemeral || !strings.Contains(empty.Text, "owes you nothing") {
			t.Fatalf("got %+v", empty)
		}
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "A", "B", 1, 500, "EUR")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusFor, Target: "B"})
		if !strings.Contains(r.Text, "20.00 PLN") || !strings.Contains(r.Text, "5.00 EUR") {
			t.Fatalf("got %q", r.Text)
		}
	})

	t.Run("status output includes since-phrase per amount", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "A", "B", 1, 500, "EUR")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusFor, Target: "B"})

		for _, want := range []string{"20.00 PLN (just now)", "5.00 EUR (just now)"} {
			if !strings.Contains(r.Text, want) {
				t.Errorf("status text %q missing %q", r.Text, want)
			}
		}
	})

	t.Run("status output includes since-phrase in polish", func(t *testing.T) {
		l := newLedgerLocale(t, "pl")
		add(t, l, "A", "B", 1, 2000, "PLN")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})

		if !strings.Contains(r.Text, "20.00 PLN (przed chwilą)") {
			t.Errorf("polish status text missing since-phrase: %q", r.Text)
		}
	})

	t.Run("polish locale", func(t *testing.T) {
		l := newLedgerLocale(t, "pl")
		r := add(t, l, "A", "B", 1, 2000, "PLN")
		if !strings.Contains(r.Text, "jest tobie winny 20.00 PLN") {
			t.Fatalf("polish add: %q", r.Text)
		}
		// status-all populated
		all, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		if !strings.Contains(all.Text, "Mają u Ciebie dług") || !strings.Contains(all.Text, "<@B>") {
			t.Fatalf("polish status-all: %q", all.Text)
		}
		// reset clears, status-all empty changes phrasing too
		_, _ = l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindReset, Target: "B"})
		empty, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		if !strings.Contains(empty.Text, "Nikt nie ma u Ciebie długu") {
			t.Fatalf("polish status-all empty: %q", empty.Text)
		}
	})

	t.Run("pay set / show self / show for / remove / clear", func(t *testing.T) {
		l := newLedger(t)
		ctx := context.Background()

		// Empty self.
		empty, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		if empty.Ephemeral || !strings.Contains(empty.Text, "haven't added") {
			t.Fatalf("empty self: %+v", empty)
		}

		// Set two methods.
		if _, err := l.Apply(ctx, "A", parser.Command{
			Kind: parser.KindPaySet, PayMethod: "bank", PayValue: "PL61 1090",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := l.Apply(ctx, "A", parser.Command{
			Kind: parser.KindPaySet, PayMethod: "blik", PayValue: "555 555 555",
		}); err != nil {
			t.Fatal(err)
		}

		// Show self lists both.
		mine, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		for _, want := range []string{"bank", "PL61 1090", "blik", "555 555 555"} {
			if !strings.Contains(mine.Text, want) {
				t.Errorf("show self missing %q in %q", want, mine.Text)
			}
		}

		// Another user can read A's methods.
		view, _ := l.Apply(ctx, "B", parser.Command{Kind: parser.KindPayShowFor, Target: "A"})
		if !strings.Contains(view.Text, "<@A>") || !strings.Contains(view.Text, "PL61 1090") {
			t.Fatalf("show for: %q", view.Text)
		}

		// Remove one.
		if _, err := l.Apply(ctx, "A", parser.Command{
			Kind: parser.KindPayRemove, PayMethod: "blik",
		}); err != nil {
			t.Fatal(err)
		}
		mine, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		if strings.Contains(mine.Text, "blik") {
			t.Fatalf("blik should be gone: %q", mine.Text)
		}

		// Clear wipes A but doesn't affect B.
		_, _ = l.Apply(ctx, "B", parser.Command{
			Kind: parser.KindPaySet, PayMethod: "revolut", PayValue: "@kamil",
		})
		if _, err := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayClear}); err != nil {
			t.Fatal(err)
		}
		empty, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		if !strings.Contains(empty.Text, "haven't added") {
			t.Fatalf("expected empty after clear: %q", empty.Text)
		}
		bView, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowFor, Target: "B"})
		if !strings.Contains(bView.Text, "@kamil") {
			t.Fatalf("B's revolut should still be there: %q", bView.Text)
		}
	})

	t.Run("pay set-default flags exactly one method", func(t *testing.T) {
		l := newLedger(t)
		ctx := context.Background()

		// Saving methods first.
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySet, PayMethod: "bank", PayValue: "PL61"})
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySet, PayMethod: "blik", PayValue: "555"})

		// Marking blik as default succeeds and is reflected in show-self.
		ok, err := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySetDefault, PayMethod: "blik"})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(ok.Text, "Set `blik`") {
			t.Fatalf("set-default reply: %q", ok.Text)
		}
		mine, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		if !strings.Contains(mine.Text, "blik` - 555 (default)") {
			t.Fatalf("expected blik to render as default: %q", mine.Text)
		}
		if strings.Contains(mine.Text, "bank` - PL61 (default)") {
			t.Fatalf("bank should not be marked default: %q", mine.Text)
		}

		// Trying to default an unsaved method gives a friendly hint, not an error.
		miss, err := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySetDefault, PayMethod: "wise"})
		if err != nil {
			t.Fatalf("missing method should not error: %v", err)
		}
		if !strings.Contains(miss.Text, "wise") {
			t.Fatalf("missing-method reply: %q", miss.Text)
		}
	})

	t.Run("status-all groups by debtor and isolates per creditor", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "A", "B", 1, 500, "EUR")
		add(t, l, "A", "C", 1, 100, "PLN")
		// B's view should show only what A booked (against B), under "You owe".
		bView, _ := l.Apply(context.Background(), "B", parser.Command{Kind: parser.KindStatusAll})
		if !strings.Contains(bView.Text, "You owe:") || !strings.Contains(bView.Text, "<@A>") {
			t.Fatalf("B view should list A as creditor: %q", bView.Text)
		}
		if strings.Contains(bView.Text, "Owed to you:") {
			t.Fatalf("B view should not have an owed-to-you section: %q", bView.Text)
		}
		aView, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		if aView.Ephemeral {
			t.Fatal("status-all should not be ephemeral")
		}
		if !strings.Contains(aView.Text, "Owed to you:") {
			t.Fatalf("missing owed header in %q", aView.Text)
		}
		if !strings.Contains(aView.Text, "<@B>") || !strings.Contains(aView.Text, "<@C>") {
			t.Fatalf("missing debtors in %q", aView.Text)
		}
		// B's row groups both currencies on one line.
		if !strings.Contains(aView.Text, "20.00 PLN") || !strings.Contains(aView.Text, "5.00 EUR") {
			t.Fatalf("got %q", aView.Text)
		}
		// A doesn't owe anyone, so no "You owe" section.
		if strings.Contains(aView.Text, "You owe:") {
			t.Fatalf("A view should not have a you-owe section: %q", aView.Text)
		}
	})

	t.Run("status-all shows both sections when both directions exist", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN") // B owes A
		add(t, l, "C", "A", 1, 1500, "EUR") // A owes C
		add(t, l, "D", "A", 1, 700, "PLN")  // A owes D
		aView, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		for _, want := range []string{
			"Owed to you:", "<@B>", "20.00 PLN",
			"You owe:", "<@C>", "15.00 EUR", "<@D>", "7.00 PLN",
		} {
			if !strings.Contains(aView.Text, want) {
				t.Errorf("missing %q in %q", want, aView.Text)
			}
		}
	})

	t.Run("status-all you-owe lines include creditor's default payment method", func(t *testing.T) {
		l := newLedger(t)
		ctx := context.Background()

		add(t, l, "C", "A", 1, 3600, "PLN") // A owes C 36.00 PLN
		add(t, l, "D", "A", 1, 1500, "EUR") // A owes D 15.00 EUR (D has no default)

		_, _ = l.Apply(ctx, "C", parser.Command{Kind: parser.KindPaySet, PayMethod: "blik", PayValue: "555 555 555"})
		_, _ = l.Apply(ctx, "C", parser.Command{Kind: parser.KindPaySetDefault, PayMethod: "blik"})

		aView, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindStatusAll})
		want := "You owe:\n- <@C> - 36.00 PLN (`blik 555 555 555`) (just now)\n- <@D> - 15.00 EUR (just now)"
		if aView.Text != want {
			t.Errorf("status-all text\n  got:  %q\n  want: %q", aView.Text, want)
		}
	})

	t.Run("status-all owed-to-you lines never include payment method", func(t *testing.T) {
		l := newLedger(t)
		ctx := context.Background()

		add(t, l, "A", "B", 1, 2000, "PLN") // B owes A

		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySet, PayMethod: "blik", PayValue: "111"})
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySetDefault, PayMethod: "blik"})
		_, _ = l.Apply(ctx, "B", parser.Command{Kind: parser.KindPaySet, PayMethod: "bank", PayValue: "PL61"})
		_, _ = l.Apply(ctx, "B", parser.Command{Kind: parser.KindPaySetDefault, PayMethod: "bank"})

		aView, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindStatusAll})
		want := "Owed to you:\n- <@B> - 20.00 PLN (just now)"
		if aView.Text != want {
			t.Errorf("status-all text\n  got:  %q\n  want: %q", aView.Text, want)
		}
	})

	t.Run("status-all empty when no rows on either side", func(t *testing.T) {
		l := newLedger(t)
		r, _ := l.Apply(context.Background(), "Z", parser.Command{Kind: parser.KindStatusAll})
		if !strings.Contains(r.Text, "Nobody owes you") {
			t.Fatalf("got %q", r.Text)
		}
	})
}
