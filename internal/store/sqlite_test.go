package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
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

func TestSQLiteDefaultPaymentMethod(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	if err := s.SetDefaultPaymentMethod(ctx, "U1", "bank"); !errors.Is(err, ErrUnknownMethod) {
		t.Fatalf("expected ErrUnknownMethod, got %v", err)
	}

	if err := s.SetPaymentMethod(ctx, "U1", "bank", "PL61"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaymentMethod(ctx, "U1", "blik", "555"); err != nil {
		t.Fatal(err)
	}

	pms, _ := s.ListPaymentMethods(ctx, "U1")
	for _, pm := range pms {
		if pm.IsDefault {
			t.Fatalf("expected no defaults, got %+v", pm)
		}
	}

	if err := s.SetDefaultPaymentMethod(ctx, "U1", "blik"); err != nil {
		t.Fatal(err)
	}
	pms, _ = s.ListPaymentMethods(ctx, "U1")
	if len(pms) != 2 || pms[0].Method != "blik" || !pms[0].IsDefault || pms[1].IsDefault {
		t.Fatalf("after set-default blik: %+v", pms)
	}

	if err := s.SetDefaultPaymentMethod(ctx, "U1", "bank"); err != nil {
		t.Fatal(err)
	}
	pms, _ = s.ListPaymentMethods(ctx, "U1")
	if len(pms) != 2 || pms[0].Method != "bank" || !pms[0].IsDefault || pms[1].IsDefault {
		t.Fatalf("after set-default bank: %+v", pms)
	}

	if err := s.SetPaymentMethod(ctx, "U1", "bank", "PL99"); err != nil {
		t.Fatal(err)
	}
	pms, _ = s.ListPaymentMethods(ctx, "U1")
	if pms[0].Method != "bank" || pms[0].Value != "PL99" || !pms[0].IsDefault {
		t.Fatalf("update should preserve default: %+v", pms)
	}

	if err := s.RemovePaymentMethod(ctx, "U1", "bank"); err != nil {
		t.Fatal(err)
	}
	pms, _ = s.ListPaymentMethods(ctx, "U1")
	if len(pms) != 1 || pms[0].IsDefault {
		t.Fatalf("removing default should leave no default: %+v", pms)
	}
}

func TestSQLiteDefaultPaymentMethodLookup(t *testing.T) {
	ctx := context.Background()
	s, _ := openTemp(t)

	if _, ok, err := s.DefaultPaymentMethod(ctx, "U1"); err != nil || ok {
		t.Fatalf("expected (false, nil) for missing user, got ok=%v err=%v", ok, err)
	}

	if err := s.SetPaymentMethod(ctx, "U1", "bank", "PL61"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaymentMethod(ctx, "U1", "blik", "555"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.DefaultPaymentMethod(ctx, "U1"); err != nil || ok {
		t.Fatalf("no default set: ok=%v err=%v", ok, err)
	}

	if err := s.SetDefaultPaymentMethod(ctx, "U1", "blik"); err != nil {
		t.Fatal(err)
	}
	pm, ok, err := s.DefaultPaymentMethod(ctx, "U1")
	if err != nil || !ok {
		t.Fatalf("expected blik as default, got ok=%v err=%v", ok, err)
	}
	if pm.Method != "blik" || pm.Value != "555" || !pm.IsDefault {
		t.Fatalf("unexpected default row: %+v", pm)
	}
}

func TestSQLiteMigrateFromV0(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	rawDB, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(bootstrap); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(
		`INSERT INTO payment_methods(user_id, method, value, updated_at)
		 VALUES ('U1', 'bank', 'PL61', unixepoch())`); err != nil {
		t.Fatal(err)
	}
	_ = rawDB.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatalf("open after legacy seed: %v", err)
	}
	defer func() { _ = s.Close() }()

	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != len(migrations) {
		t.Fatalf("user_version = %d, want %d", version, len(migrations))
	}

	pms, err := s.ListPaymentMethods(ctx, "U1")
	if err != nil {
		t.Fatal(err)
	}
	if len(pms) != 1 || pms[0].Value != "PL61" || pms[0].IsDefault {
		t.Fatalf("after migrate: %+v", pms)
	}
	if err := s.SetDefaultPaymentMethod(ctx, "U1", "bank"); err != nil {
		t.Fatalf("set-default after migrate: %v", err)
	}
}

func TestSQLiteMigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "current.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPaymentMethod(ctx, "U1", "bank", "PL61"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDefaultPaymentMethod(ctx, "U1", "bank"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = s2.Close() }()

	var version int
	if err := s2.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != len(migrations) {
		t.Fatalf("user_version = %d, want %d", version, len(migrations))
	}
	pms, _ := s2.ListPaymentMethods(ctx, "U1")
	if len(pms) != 1 || !pms[0].IsDefault {
		t.Fatalf("default flag lost across reopen: %+v", pms)
	}
}

func TestSQLiteMigrateRejectsFutureSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.db")
	rawDB, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(bootstrap); err != nil {
		t.Fatal(err)
	}
	if _, err := rawDB.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(migrations)+5)); err != nil {
		t.Fatal(err)
	}
	_ = rawDB.Close()

	if _, err := Open(path); err == nil {
		t.Fatal("expected Open to reject a future schema version")
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
