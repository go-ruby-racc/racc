package racc

import (
	"errors"
	"reflect"
	"testing"
)

// TestYyaccept: a reduce action that calls Yyaccept exits the parser early,
// returning the bottom of the value stack (MRI's Symbol_Value_Stack[0]). We trigger
// it on the first NUMBER reduction (rule 5) of "1 + 2"; at that point vstack[0] is 1.
func TestYyaccept(t *testing.T) {
	q := [][2]any{num(1), op("+"), num(2), eofTok}
	i := 0
	p := &Parser{Tables: calcTables()}
	p.NextToken = func() (any, any) { tk := q[i]; i++; return tk[0], tk[1] }
	p.Reduce = func(ruleIdx int, values []any, result any) any {
		if ruleIdx == 5 {
			p.Yyaccept()
		}
		return calcReduce(ruleIdx, values, result)
	}
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// At the point rule 5 reduces, the NUMBER value has been popped into the
	// reduce args and not yet pushed back, so the value stack is empty and
	// vstack[0] is nil — matching MRI's yyaccept here (verified == nil).
	if got != nil {
		t.Fatalf("Yyaccept result = %v, want nil", got)
	}
}

// TestYyerror: a reduce action calling Yyerror enters error-recovery mode without
// invoking on_error (MRI throws :racc_jump,1 which sets @racc_user_yyerror). On the
// calc grammar (no error rule) this exhausts recovery and ends at nil with no error.
func TestYyerror(t *testing.T) {
	q := [][2]any{num(1), op("+"), num(2), eofTok}
	i := 0
	var onErr int
	p := &Parser{Tables: calcTables()}
	p.NextToken = func() (any, any) { tk := q[i]; i++; return tk[0], tk[1] }
	p.OnError = func(int, any, []any) error { onErr++; return nil }
	first := true
	p.Reduce = func(ruleIdx int, values []any, result any) any {
		if ruleIdx == 5 && first {
			first = false
			p.Yyerror()
		}
		return calcReduce(ruleIdx, values, result)
	}
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("Yyerror result = %v, want nil", got)
	}
	// Yyerror suppresses on_error for the user-triggered error.
	if onErr != 0 {
		t.Fatalf("on_error calls = %d, want 0 (user yyerror)", onErr)
	}
}

// TestYyerrok exercises the Yyerrok seam (reset error status) directly.
func TestYyerrok(t *testing.T) {
	p := &Parser{Tables: calcTables()}
	p.raccErrStatus = 3
	p.Yyerrok()
	if p.raccErrStatus != 0 {
		t.Fatalf("after Yyerrok status = %d, want 0", p.raccErrStatus)
	}
}

// TestReduceUnknownJumpCode: doReduceCatch must surface a real panic that is not a
// jumpCode. A Reduce that panics with a non-jumpCode value should propagate.
func TestReduceForeignPanic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic to propagate")
		}
		if s, ok := r.(string); !ok || s != "boom" {
			t.Fatalf("recovered %v, want \"boom\"", r)
		}
	}()
	q := [][2]any{num(1), eofTok}
	i := 0
	p := &Parser{Tables: calcTables()}
	p.NextToken = func() (any, any) { tk := q[i]; i++; return tk[0], tk[1] }
	p.Reduce = func(int, []any, any) any { panic("boom") }
	_, _ = p.DoParse()
}

// TestRecoverTopForeignPanic: recoverTop re-raises any panic that is not a control
// signal. Drive a foreign panic from NextToken (outside the reduce catch).
func TestRecoverTopForeignPanic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil || r.(string) != "lex-boom" {
			t.Fatalf("recovered %v, want lex-boom", r)
		}
	}()
	p := &Parser{Tables: calcTables(), Reduce: calcReduce}
	p.NextToken = func() (any, any) { panic("lex-boom") }
	_, _ = p.DoParse()
}

// TestUnknownActionBug: an action code outside every band is a "[Racc Bug]". We
// build a degenerate Tables whose default action lands in the impossible gap
// (act == 0, which is neither shift (>0) nor reduce (<0) nor accept nor error).
func TestUnknownActionBug(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected [Racc Bug] panic")
		}
	}()
	tb := &Tables{
		ActionDefault: []int{0}, // state 0 default = 0 -> impossible band
		ActionPointer: []int{ptrNil},
		ShiftN:        10,
		ReduceN:       5,
	}
	p := &Parser{Tables: tb, Reduce: func(int, []any, any) any { return nil }}
	p.NextToken = func() (any, any) { return nil, nil }
	_, _ = p.DoParse()
}

// TestUnknownJumpCodeBug: an out-of-band jump code is a "[Racc Bug]". We panic a
// jumpCode(99) from a reduce action; evalact's switch hits the default arm.
func TestUnknownJumpCodeBug(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected [Racc Bug] panic")
		}
	}()
	q := [][2]any{num(1), eofTok}
	i := 0
	p := &Parser{Tables: calcTables()}
	p.NextToken = func() (any, any) { tk := q[i]; i++; return tk[0], tk[1] }
	p.Reduce = func(int, []any, any) any { panic(jumpCode(99)) }
	_, _ = p.DoParse()
}

// TestAcceptEmptyStack covers the accept arm when the value stack is empty
// (vstack[0] would be nil). A grammar that accepts the empty input returns nil.
// We synthesize a 1-state table whose default action is accept (shift_n).
func TestAcceptEmptyStack(t *testing.T) {
	tb := &Tables{
		ActionDefault: []int{5}, // accept
		ActionPointer: []int{ptrNil},
		ShiftN:        5,
		ReduceN:       3,
	}
	p := &Parser{Tables: tb, Reduce: func(int, []any, any) any { return nil }}
	p.NextToken = func() (any, any) { return nil, nil }
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

// TestGotoDefault covers _racc_do_reduce's fallthrough to goto_default when the
// compressed goto entry's check does not match. The stmt grammar already drives
// goto_default during normal reduction; assert the valid parse threads it.
func TestGotoDefaultThreaded(t *testing.T) {
	// stmts: stmts stmt uses goto_default for the `stmts` nonterminal after the
	// second statement. A 3-statement parse forces repeated goto_default lookups.
	p := newStmtParser([][2]any{
		num(1), op(";"), num(2), op(";"), num(3), op(";"), eofTok,
	})
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(got, []any{1, 2, 3}) {
		t.Fatalf("got = %#v, want [1 2 3]", got)
	}
}

// TestYyparseUnknownToken drives the yyparse path with an out-of-table token to
// cover its lookup-miss arm and error termination.
func TestYyparseUnknownToken(t *testing.T) {
	p := &Parser{Tables: calcTables(), Reduce: calcReduce}
	_, err := p.Yyparse(func(yield func(any, any)) {
		yield(Symbol("BOGUS"), 0)
		yield(nil, nil)
	})
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *ParseError", err)
	}
}
