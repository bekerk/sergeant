package store

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

func openTemp(t *testing.T) (*SQLite, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestSQLiteRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	// Add accumulates.
	if got, _ := s.AddDelta(ctx, "A", "B", "PLN", 2000); got != 2000 {
		t.Fatalf("first add: got %d", got)
	}
	if got, _ := s.AddDelta(ctx, "A", "B", "PLN", 500); got != 2500 {
		t.Fatalf("second add: got %d", got)
	}

	// Negative delta clamps to zero and deletes.
	if got, _ := s.AddDelta(ctx, "A", "B", "PLN", -10000); got != 0 {
		t.Fatalf("clamp: got %d", got)
	}
	if rows, _ := s.ListPair(ctx, "A", "B"); len(rows) != 0 {
		t.Fatalf("expected empty after clamp, got %v", rows)
	}

	// Multi-currency, multi-debtor; only A's view is returned.
	for _, op := range []struct {
		c, d, ccy string
		amt       int64
	}{
		{"A", "B", "PLN", 2000},
		{"A", "B", "EUR", 500},
		{"A", "C", "PLN", 100},
		{"X", "B", "PLN", 9999}, // different creditor; must be invisible to A
	} {
		if _, err := s.AddDelta(ctx, op.c, op.d, op.ccy, op.amt); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListByCreditor(ctx, "A")
	if err != nil {
		t.Fatal(err)
	}
	want := []Debt{
		{Creditor: "A", Debtor: "B", Currency: "EUR", AmountMinor: 500},
		{Creditor: "A", Debtor: "B", Currency: "PLN", AmountMinor: 2000},
		{Creditor: "A", Debtor: "C", Currency: "PLN", AmountMinor: 100},
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range got {
		if got[i].InsertedAt == 0 {
			t.Errorf("row %d: InsertedAt was not set", i)
		}
		got[i].InsertedAt = 0 // ignore for the structural comparison below
		if got[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, got[i], want[i])
		}
	}

	// ListByDebtor returns rows where the user is the debtor.
	bDebt, err := s.ListByDebtor(ctx, "B")
	if err != nil {
		t.Fatal(err)
	}
	wantBDebt := []Debt{
		{Creditor: "A", Debtor: "B", Currency: "EUR", AmountMinor: 500},
		{Creditor: "A", Debtor: "B", Currency: "PLN", AmountMinor: 2000},
		{Creditor: "X", Debtor: "B", Currency: "PLN", AmountMinor: 9999},
	}
	if len(bDebt) != len(wantBDebt) {
		t.Fatalf("ListByDebtor B: got %v, want %v", bDebt, wantBDebt)
	}
	for i := range bDebt {
		bDebt[i].InsertedAt = 0
		if bDebt[i] != wantBDebt[i] {
			t.Errorf("debtor row %d: got %+v, want %+v", i, bDebt[i], wantBDebt[i])
		}
	}

	// Reset one currency, then everything.
	if err := s.ResetPairCurrency(ctx, "A", "B", "EUR"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListPair(ctx, "A", "B"); len(rows) != 1 || rows[0].Currency != "PLN" {
		t.Fatalf("after reset EUR: %v", rows)
	}
	if err := s.ResetPair(ctx, "A", "B"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListPair(ctx, "A", "B"); len(rows) != 0 {
		t.Fatalf("after reset all: %v", rows)
	}
}

func TestSQLitePaymentMethods(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	// Empty starts empty.
	if pms, _ := s.ListPaymentMethods(ctx, "U1"); len(pms) != 0 {
		t.Fatalf("expected empty, got %v", pms)
	}

	// Set, then list.
	if err := s.SetPaymentMethod(ctx, "U1", "bank", "PL61 1090 0000"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaymentMethod(ctx, "U1", "blik", "555 555 555"); err != nil {
		t.Fatal(err)
	}
	pms, err := s.ListPaymentMethods(ctx, "U1")
	if err != nil {
		t.Fatal(err)
	}
	want := []PaymentMethod{
		{UserID: "U1", Method: "bank", Value: "PL61 1090 0000"},
		{UserID: "U1", Method: "blik", Value: "555 555 555"},
	}
	if len(pms) != len(want) {
		t.Fatalf("got %v, want %v", pms, want)
	}
	for i := range pms {
		if pms[i].InsertedAt == 0 {
			t.Errorf("row %d: InsertedAt was not set", i)
		}
		pms[i].InsertedAt = 0
		if pms[i] != want[i] {
			t.Errorf("row %d: got %+v, want %+v", i, pms[i], want[i])
		}
	}

	// Update overwrites.
	if err := s.SetPaymentMethod(ctx, "U1", "bank", "PL99 9999 9999"); err != nil {
		t.Fatal(err)
	}
	pms, _ = s.ListPaymentMethods(ctx, "U1")
	if pms[0].Value != "PL99 9999 9999" {
		t.Fatalf("update did not overwrite, got %v", pms[0])
	}

	// Remove one.
	if err := s.RemovePaymentMethod(ctx, "U1", "blik"); err != nil {
		t.Fatal(err)
	}
	pms, _ = s.ListPaymentMethods(ctx, "U1")
	if len(pms) != 1 || pms[0].Method != "bank" {
		t.Fatalf("after remove blik: %v", pms)
	}

	// Clear wipes everything for that user.
	_ = s.SetPaymentMethod(ctx, "U2", "revolut", "@kamil")
	if err := s.ClearPaymentMethods(ctx, "U1"); err != nil {
		t.Fatal(err)
	}
	if pms, _ := s.ListPaymentMethods(ctx, "U1"); len(pms) != 0 {
		t.Fatalf("U1 not cleared: %v", pms)
	}
	if pms, _ := s.ListPaymentMethods(ctx, "U2"); len(pms) != 1 {
		t.Fatalf("U2 should be untouched: %v", pms)
	}
}

func TestSQLitePersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "persist.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddDelta(context.Background(), "A", "B", "PLN", 1234); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	rows, _ := s2.ListPair(context.Background(), "A", "B")
	if len(rows) != 1 || rows[0].AmountMinor != 1234 {
		t.Fatalf("got %v", rows)
	}
}

func TestAddDeltaConcurrent(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	const goroutines = 50
	const perGoroutine = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				if _, err := s.AddDelta(ctx, "A", "B", "PLN", 1); err != nil {
					t.Errorf("AddDelta: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	rows, err := s.ListPair(ctx, "A", "B")
	if err != nil {
		t.Fatal(err)
	}
	want := int64(goroutines * perGoroutine)
	if len(rows) != 1 || rows[0].AmountMinor != want {
		t.Fatalf("final balance = %v, want single row of %d", rows, want)
	}
}

func TestAddDeltaConcurrentMixedSigns(t *testing.T) {
	s, _ := openTemp(t)
	ctx := context.Background()

	if _, err := s.AddDelta(ctx, "A", "B", "PLN", 100000); err != nil {
		t.Fatal(err)
	}

	const goroutines = 40
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		delta := int64(1)
		if i%2 == 1 {
			delta = -1
		}
		go func(d int64) {
			defer wg.Done()
			if _, err := s.AddDelta(ctx, "A", "B", "PLN", d); err != nil {
				t.Errorf("AddDelta: %v", err)
			}
		}(delta)
	}
	wg.Wait()

	rows, _ := s.ListPair(ctx, "A", "B")
	if len(rows) != 1 || rows[0].AmountMinor != 100000 {
		t.Fatalf("final balance = %v, want 100000", rows)
	}
}
