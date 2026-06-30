package racc

import (
	"reflect"
	"testing"
)

func newStmtParser(toks [][2]any) *Parser {
	q := append([][2]any(nil), toks...)
	p := &Parser{Tables: stmtTables(), Reduce: stmtReduce}
	p.NextToken = func() (any, any) {
		t := q[0]
		q = q[1:]
		return t[0], t[1]
	}
	return p
}

func TestStmtValid(t *testing.T) {
	// 1 ; 2 ;  -> [1, 2]
	p := newStmtParser([][2]any{
		num(1), op(";"), num(2), op(";"), eofTok,
	})
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []any{1, 2}) {
		t.Fatalf("result = %#v, want [1 2]", got)
	}
}

func TestStmtErrorRecovery(t *testing.T) {
	// 1 ; @ ;  where @ is a BOGUS token absent from the table. The grammar's
	// `error ';'` rule means the offending token maps to the error token (id 1),
	// which has a direct shift in this state — so MRI recovers to [1, :recovered]
	// WITHOUT ever calling on_error (verified against MRI: 0 on_error calls). The
	// engine must reproduce that exactly, including not invoking the seam.
	var errCalls int
	p := newStmtParser([][2]any{
		num(1), op(";"), {Symbol("BOGUS"), "@"}, op(";"), eofTok,
	})
	p.OnError = func(int, any, []any) error { errCalls++; return nil }
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if errCalls != 0 {
		t.Fatalf("on_error calls = %d, want 0 (direct error-token shift)", errCalls)
	}
	want := []any{1, Symbol("recovered")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result = %#v, want %#v", got, want)
	}
}

// TestStmtErrorRecoveryDefaultHandler drives the same recovery shape with the
// engine's default on_error. Because the error token shifts directly here, no
// error action fires and the default handler is never reached — the parse still
// recovers to [1, :recovered], matching MRI.
func TestStmtErrorRecoveryDefaultHandler(t *testing.T) {
	p := newStmtParser([][2]any{
		num(1), op(";"), {Symbol("BOGUS"), "@"}, op(";"), eofTok,
	})
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, []any{1, Symbol("recovered")}) {
		t.Fatalf("result = %#v, want [1 recovered]", got)
	}
}
