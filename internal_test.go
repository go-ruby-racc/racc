package racc

import (
	"errors"
	"testing"
)

// --- table-access helper branches -------------------------------------------

func TestActionPointerBranches(t *testing.T) {
	tb := &Tables{ActionPointer: []int{5, ptrNil}}
	if v, ok := tb.actionPointer(0); !ok || v != 5 {
		t.Fatalf("actionPointer(0) = %d,%v want 5,true", v, ok)
	}
	if _, ok := tb.actionPointer(1); ok {
		t.Fatalf("actionPointer(1) (ptrNil) should be !ok")
	}
	if _, ok := tb.actionPointer(-1); ok {
		t.Fatalf("actionPointer(-1) (out of range) should be !ok")
	}
	if _, ok := tb.actionPointer(99); ok {
		t.Fatalf("actionPointer(99) (out of range) should be !ok")
	}
}

func TestGotoPointerBranches(t *testing.T) {
	tb := &Tables{GotoPointer: []int{7, ptrNil}}
	if v, ok := tb.gotoPointer(0); !ok || v != 7 {
		t.Fatalf("gotoPointer(0) = %d,%v want 7,true", v, ok)
	}
	if _, ok := tb.gotoPointer(1); ok {
		t.Fatalf("gotoPointer(1) (ptrNil) should be !ok")
	}
	if _, ok := tb.gotoPointer(-1); ok {
		t.Fatalf("gotoPointer(-1) should be !ok")
	}
	if _, ok := tb.gotoPointer(5); ok {
		t.Fatalf("gotoPointer(5) (out of range) should be !ok")
	}
}

func TestTableAtBranches(t *testing.T) {
	tab := []int{3, nilCell}
	if v, ok := tableAt(tab, 0); !ok || v != 3 {
		t.Fatalf("tableAt(0) = %d,%v want 3,true", v, ok)
	}
	if _, ok := tableAt(tab, 1); ok {
		t.Fatalf("tableAt(nilCell) should be !ok")
	}
	if _, ok := tableAt(tab, -1); ok {
		t.Fatalf("tableAt(-1) should be !ok")
	}
	if _, ok := tableAt(tab, 9); ok {
		t.Fatalf("tableAt(out of range) should be !ok")
	}
}

// --- doReduce: empty production with use_result -----------------------------

// TestReduceEmptyProductionUseResult covers _racc_do_reduce's `result = tmp_v[0]`
// guard when the rule has length 0: result must stay nil (no panic on tmp_v[0]).
// We synthesize a single-rule grammar `s: /* empty */` whose start state reduces by
// default and then accepts.
func TestReduceEmptyProductionUseResult(t *testing.T) {
	// States: 0 (start). Default action of state 0 = reduce rule 1 (act -1).
	// reduce_table[3..6] = {len:0, reduce_to: nt_base, method_id:1}. After reducing,
	// goto sends us to state 1 whose default action is accept (shift_n).
	tb := &Tables{
		ActionTable:   []int{},
		ActionCheck:   []int{},
		ActionDefault: []int{-1, 2}, // state0: reduce rule1; state1: accept
		ActionPointer: []int{ptrNil, ptrNil},
		GotoTable:     []int{1},
		GotoCheck:     []int{0},
		GotoDefault:   []int{nilCell},
		GotoPointer:   []int{0},
		NtBase:        1,
		ReduceTable: []int{
			0, 0, 0, // racc_error
			0, 1, 1, // rule1: s: (empty) -> nt 1, method 1
		},
		TokenTable: map[any]int{nil: 0},
		ShiftN:     2,
		ReduceN:    2,
		UseResult:  true,
	}
	var gotResult any = "sentinel"
	gotLen := -1
	p := &Parser{Tables: tb}
	p.NextToken = func() (any, any) { return nil, nil }
	p.Reduce = func(methodID int, values []any, result any) any {
		gotResult = result
		gotLen = len(values)
		return "produced"
	}
	res, err := p.DoParse()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if gotLen != 0 {
		t.Fatalf("values len = %d, want 0", gotLen)
	}
	if gotResult != nil {
		t.Fatalf("result for empty production = %v, want nil", gotResult)
	}
	// vstack[0] at accept is the produced value.
	if res != "produced" {
		t.Fatalf("result = %v, want produced", res)
	}
}

// --- callOnError default message with no token string -----------------------

func TestCallOnErrorNoTokenString(t *testing.T) {
	// Tables without a TokenToS entry for the error token -> message uses "?".
	p := &Parser{Tables: &Tables{}}
	p.raccT = 2
	p.raccVal = "x"
	err := p.callOnError()
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *ParseError", err)
	}
	want := `parse error on value "x" (?)`
	if pe.Msg != want {
		t.Fatalf("msg = %q, want %q", pe.Msg, want)
	}
}

// --- Yyparse: default-reduce before first token, and early iterator stop -----

// yyparseStartReduceTables: state 0 has no action_pointer, so Yyparse's pre-token
// loop reduces by default (covering that loop) before the first token is pulled.
func yyparseStartReduceTables() *Tables {
	return &Tables{
		ActionDefault: []int{-1, 2}, // state0 reduce rule1; state1 accept
		ActionPointer: []int{ptrNil, ptrNil},
		GotoTable:     []int{1},
		GotoCheck:     []int{0},
		GotoDefault:   []int{nilCell},
		GotoPointer:   []int{0},
		NtBase:        1,
		ReduceTable:   []int{0, 0, 0, 0, 1, 1},
		TokenTable:    map[any]int{nil: 0},
		ShiftN:        2,
		ReduceN:       2,
		UseResult:     false,
	}
}

func TestYyparsePreTokenReduce(t *testing.T) {
	p := &Parser{Tables: yyparseStartReduceTables()}
	p.Reduce = func(int, []any, any) any { return "v" }
	// The parse accepts during the pre-token loop, before the iterator runs.
	called := false
	got, err := p.Yyparse(func(yield func(any, any)) { called = true })
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != "v" {
		t.Fatalf("got = %v, want v", got)
	}
	if called {
		t.Fatalf("iterator should not run when parse completes pre-token")
	}
}

// TestYyparseIteratorStopsEarly covers Yyparse's fallthrough `return nil, nil`:
// the iterator returns without the parse reaching accept or error.
func TestYyparseIteratorStopsEarly(t *testing.T) {
	p := &Parser{Tables: calcTables(), Reduce: calcReduce}
	got, err := p.Yyparse(func(yield func(any, any)) {
		// yield a single NUMBER then stop — the automaton shifts it and waits for
		// more input that never comes; the iterator simply returns.
		yield(Symbol("NUMBER"), 5)
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil (iterator exhausted mid-parse)", got)
	}
}
