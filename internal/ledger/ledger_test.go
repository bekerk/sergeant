package ledger

import (
	"context"
	"errors"
	"path/filepath"
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
		if want := "<@B> now owes you 25.00 PLN."; r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("subtract clamps at zero", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 1000, "PLN")
		r := add(t, l, "A", "B", -1, 9999, "PLN")
		// With debt simplification, subtracting more than exists creates reverse debt
		// 10.00 - 99.99 = -89.99, so A now owes B 89.99 PLN
		if want := "You now owe <@B> 89.99 PLN."; r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
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
		if empty.Ephemeral {
			t.Fatalf("empty status-for should not be ephemeral: %+v", empty)
		}
		if want := "<@B> owes you nothing."; empty.Text != want {
			t.Fatalf("got %q, want %q", empty.Text, want)
		}
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "A", "B", 1, 500, "EUR")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusFor, Target: "B"})
		if want := "<@B> owes you 5.00 EUR, 20.00 PLN."; r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("status output includes amounts only", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "A", "B", 1, 500, "EUR")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusFor, Target: "B"})
		if want := "<@B> owes you 5.00 EUR, 20.00 PLN."; r.Text != want {
			t.Errorf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("status output in polish without timestamps", func(t *testing.T) {
		l := newLedgerLocale(t, "pl")
		add(t, l, "A", "B", 1, 2000, "PLN")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		if want := "Mają u Ciebie dług:\n- <@B> - 20.00 PLN"; r.Text != want {
			t.Errorf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("polish locale", func(t *testing.T) {
		l := newLedgerLocale(t, "pl")
		r := add(t, l, "A", "B", 1, 2000, "PLN")
		if want := "<@B> jest tobie winny 20.00 PLN."; r.Text != want {
			t.Fatalf("polish add: got %q, want %q", r.Text, want)
		}
		// status-all populated
		all, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		if want := "Mają u Ciebie dług:\n- <@B> - 20.00 PLN"; all.Text != want {
			t.Fatalf("polish status-all: got %q, want %q", all.Text, want)
		}
		// reset clears, status-all empty changes phrasing too
		_, _ = l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindReset, Target: "B"})
		empty, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		if want := "Nikt nie ma u Ciebie długu i Ty też nie."; empty.Text != want {
			t.Fatalf("polish status-all empty: got %q, want %q", empty.Text, want)
		}
	})

	t.Run("pay set / show self / show for / remove / clear", func(t *testing.T) {
		l := newLedger(t)
		ctx := context.Background()

		emptySelf := "You haven't added any payment methods yet. Try `@sergeant pay set bank PL61 ...` or `@sergeant pay set blik 555 555 555`."

		// Empty self.
		empty, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		if empty.Ephemeral {
			t.Fatalf("empty self should not be ephemeral: %+v", empty)
		}
		if empty.Text != emptySelf {
			t.Fatalf("empty self: got %q, want %q", empty.Text, emptySelf)
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
		wantBoth := "<@A> :money_with_wings: \n- `bank` - PL61 1090\n- `blik` - 555 555 555"
		if mine.Text != wantBoth {
			t.Errorf("show self: got %q, want %q", mine.Text, wantBoth)
		}

		// Another user can read A's methods.
		view, _ := l.Apply(ctx, "B", parser.Command{Kind: parser.KindPayShowFor, Target: "A"})
		if view.Text != wantBoth {
			t.Fatalf("show for: got %q, want %q", view.Text, wantBoth)
		}

		// Remove one.
		if _, err := l.Apply(ctx, "A", parser.Command{
			Kind: parser.KindPayRemove, PayMethod: "blik",
		}); err != nil {
			t.Fatal(err)
		}
		mine, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		wantBank := "<@A> :money_with_wings: \n- `bank` - PL61 1090"
		if mine.Text != wantBank {
			t.Fatalf("after remove: got %q, want %q", mine.Text, wantBank)
		}

		// Clear wipes A but doesn't affect B.
		_, _ = l.Apply(ctx, "B", parser.Command{
			Kind: parser.KindPaySet, PayMethod: "revolut", PayValue: "@kamil",
		})
		if _, err := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayClear}); err != nil {
			t.Fatal(err)
		}
		empty, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		if empty.Text != emptySelf {
			t.Fatalf("after clear: got %q, want %q", empty.Text, emptySelf)
		}
		bView, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowFor, Target: "B"})
		wantB := "<@B> :money_with_wings: \n- `revolut` - @kamil"
		if bView.Text != wantB {
			t.Fatalf("B's view: got %q, want %q", bView.Text, wantB)
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
		if want := "Set `blik` as your default payment method."; ok.Text != want {
			t.Fatalf("set-default reply: got %q, want %q", ok.Text, want)
		}
		mine, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPayShowSelf})
		wantShow := "<@A> :money_with_wings: \n- `blik` - 555 (default)\n- `bank` - PL61"
		if mine.Text != wantShow {
			t.Fatalf("show self: got %q, want %q", mine.Text, wantShow)
		}

		// Trying to default an unsaved method gives a friendly hint, not an error.
		miss, err := l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySetDefault, PayMethod: "wise"})
		if err != nil {
			t.Fatalf("missing method should not error: %v", err)
		}
		if want := "You haven't saved `wise` yet — add it first with `pay set wise ...`."; miss.Text != want {
			t.Fatalf("missing-method reply: got %q, want %q", miss.Text, want)
		}
	})

	t.Run("status-all groups by debtor and isolates per creditor", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "A", "B", 1, 500, "EUR")
		add(t, l, "A", "C", 1, 100, "PLN")
		// B's view should show only what A booked (against B), under "You owe".
		bView, _ := l.Apply(context.Background(), "B", parser.Command{Kind: parser.KindStatusAll})
		wantB := "You owe:\n- <@A> - 5.00 EUR, 20.00 PLN"
		if bView.Text != wantB {
			t.Fatalf("B view: got %q, want %q", bView.Text, wantB)
		}
		aView, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		if aView.Ephemeral {
			t.Fatal("status-all should not be ephemeral")
		}
		wantA := "Owed to you:\n- <@B> - 5.00 EUR, 20.00 PLN\n- <@C> - 1.00 PLN"
		if aView.Text != wantA {
			t.Fatalf("A view: got %q, want %q", aView.Text, wantA)
		}
	})

	t.Run("status-all shows both sections when both directions exist", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN") // B owes A
		add(t, l, "C", "A", 1, 1500, "EUR") // A owes C
		add(t, l, "D", "A", 1, 700, "PLN")  // A owes D
		aView, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		want := "Owed to you:\n- <@B> - 20.00 PLN\nYou owe:\n- <@C> - 15.00 EUR\n- <@D> - 7.00 PLN"
		if aView.Text != want {
			t.Errorf("got %q, want %q", aView.Text, want)
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
		want := "You owe:\n- <@C> - 36.00 PLN (`blik 555 555 555`)\n- <@D> - 15.00 EUR"
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
		want := "Owed to you:\n- <@B> - 20.00 PLN"
		if aView.Text != want {
			t.Errorf("status-all text\n  got:  %q\n  want: %q", aView.Text, want)
		}
	})

	t.Run("status-all empty when no rows on either side", func(t *testing.T) {
		l := newLedger(t)
		r, _ := l.Apply(context.Background(), "Z", parser.Command{Kind: parser.KindStatusAll})
		if want := "Nobody owes you and you owe nothing."; r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("net debt simplification - bidirectional debts cancel out", func(t *testing.T) {
		l := newLedger(t)
		// Adam is creditor: Kamil owes Adam 15 PLN
		add(t, l, "Adam", "Kamil", 1, 1500, "PLN")
		// Kamil is creditor: Adam owes Kamil 30 PLN
		add(t, l, "Kamil", "Adam", 1, 3000, "PLN")

		// From Adam's view: net = 15 - 30 = -15, so Adam owes Kamil 15 PLN
		adamView, _ := l.Apply(context.Background(), "Adam", parser.Command{Kind: parser.KindStatusAll})
		wantAdam := "You owe:\n- <@Kamil> - 15.00 PLN"
		if adamView.Text != wantAdam {
			t.Fatalf("Adam view: got %q, want %q", adamView.Text, wantAdam)
		}

		// From Kamil's view: net = 30 - 15 = 15, so Kamil is owed 15 PLN
		kamilView, _ := l.Apply(context.Background(), "Kamil", parser.Command{Kind: parser.KindStatusAll})
		wantKamil := "Owed to you:\n- <@Adam> - 15.00 PLN"
		if kamilView.Text != wantKamil {
			t.Fatalf("Kamil view: got %q, want %q", kamilView.Text, wantKamil)
		}
	})

	t.Run("net debt simplification - exact cancel shows empty", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "B", "A", 1, 2000, "PLN")

		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusAll})
		if want := "Nobody owes you and you owe nothing."; r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("reset clears both directions", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "B", "A", 1, 1000, "PLN")

		// Before reset: net = 10 PLN owed to A
		r1, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusFor, Target: "B"})
		if want := "<@B> owes you 10.00 PLN."; r1.Text != want {
			t.Fatalf("before reset: got %q, want %q", r1.Text, want)
		}

		// Reset from A's side should clear both directions
		_, err := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindReset, Target: "B"})
		if err != nil {
			t.Fatal(err)
		}

		// After reset: both directions cleared
		r2, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindStatusFor, Target: "B"})
		if want := "<@B> owes you nothing."; r2.Text != want {
			t.Fatalf("after reset: got %q, want %q", r2.Text, want)
		}

		// B's view should also be empty
		r3, _ := l.Apply(context.Background(), "B", parser.Command{Kind: parser.KindStatusFor, Target: "A"})
		if want := "<@A> owes you nothing."; r3.Text != want {
			t.Fatalf("B view after reset: got %q, want %q", r3.Text, want)
		}
	})

	t.Run("summary empty", func(t *testing.T) {
		l := newLedger(t)
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindSummary})
		if want := "Nobody owes you anything."; r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("summary with debts but no payment methods", func(t *testing.T) {
		l := newLedger(t)
		add(t, l, "A", "B", 1, 2000, "PLN")
		add(t, l, "A", "C", 1, 3470, "PLN")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindSummary})
		want := "Summary\n- <@B> - 20.00 PLN\n- <@C> - 34.70 PLN"
		if r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("summary with debts and payment methods", func(t *testing.T) {
		l := newLedger(t)
		ctx := context.Background()
		add(t, l, "A", "B", 1, 2300, "PLN")
		add(t, l, "A", "C", 1, 3400, "PLN")
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySet, PayMethod: "bank", PayValue: "PL61 1090 1234"})
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySet, PayMethod: "blik", PayValue: "555 555 555"})
		r, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindSummary})
		want := "Summary\n- <@B> - 23.00 PLN\n- <@C> - 34.00 PLN\n\n:money_with_wings:\n- `bank` - PL61 1090 1234\n- `blik` - 555 555 555"
		if r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("summary with payments including default", func(t *testing.T) {
		l := newLedger(t)
		ctx := context.Background()
		add(t, l, "A", "B", 1, 2300, "PLN")
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySet, PayMethod: "bank", PayValue: "PL61 1090 1234"})
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySet, PayMethod: "blik", PayValue: "555 555 555"})
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySetDefault, PayMethod: "blik"})
		r, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindSummary})
		want := "Summary\n- <@B> - 23.00 PLN\n\n:money_with_wings:\n- `blik` - 555 555 555 (default)\n- `bank` - PL61 1090 1234"
		if r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("summary polish locale", func(t *testing.T) {
		l := newLedgerLocale(t, "pl")
		add(t, l, "A", "B", 1, 2300, "PLN")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindSummary})
		want := "Podsumowanie:\n- <@B> - 23.00 PLN"
		if r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("summary polish empty", func(t *testing.T) {
		l := newLedgerLocale(t, "pl")
		r, _ := l.Apply(context.Background(), "A", parser.Command{Kind: parser.KindSummary})
		if want := "Nikt nie ma u ciebie długu."; r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})

	t.Run("summary polish with pay methods", func(t *testing.T) {
		l := newLedgerLocale(t, "pl")
		ctx := context.Background()
		add(t, l, "A", "B", 1, 2300, "PLN")
		_, _ = l.Apply(ctx, "A", parser.Command{Kind: parser.KindPaySet, PayMethod: "blik", PayValue: "555 555 555"})
		r, _ := l.Apply(ctx, "A", parser.Command{Kind: parser.KindSummary})
		want := "Podsumowanie\n- <@B> - 23.00 PLN\n\n:money_with_wings:\n- `blik` - 555 555 555"
		if r.Text != want {
			t.Fatalf("got %q, want %q", r.Text, want)
		}
	})
}
