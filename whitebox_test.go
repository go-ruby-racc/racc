package racc

import "testing"

// TestEvalactErrorStatus3 directly exercises evalact's error arm with
// raccErrStatus == 3 and a non-EOF lookahead (raccT != 0): MRI sets read_next and
// re-enters recovery. We seed a stmt-grammar state stack positioned so the recovery
// loop finds an error-token shift and returns a real action, rather than looping.
func TestEvalactErrorStatus3(t *testing.T) {
	p := &Parser{Tables: stmtTables(), Reduce: stmtReduce}
	p.initSysvars()
	// In the stmt tables, state 0 has an error-token shift to action 5
	// (action_table[pointer(0)+1] with a matching check). Put state 0 on top, mark
	// recovery already in progress (status 3) with a non-EOF lookahead so the
	// case-3 read-next arm runs before the recovery loop returns the error shift.
	p.state = []int{0}
	p.raccErrStatus = 3
	p.raccT = 2 // NUMBER, non-EOF
	p.raccVal = 99
	p.raccReadNext = false

	next, cont := p.evalact(-p.Tables.ReduceN) // the error action
	if !cont {
		t.Fatalf("evalact(error) should return a continue action")
	}
	// The recovery found state 0's error-token shift -> action 5.
	if next != 5 {
		t.Fatalf("recovery action = %d, want 5", next)
	}
	// The case-3 arm must have set read_next.
	if !p.raccReadNext {
		t.Fatalf("read_next should be set in the status-3 recovery arm")
	}
}

// TestDoReduceGotoDefault covers _racc_do_reduce's fallthrough to goto_default when
// the compressed goto cell's check does not match the produced nonterminal. We call
// doReduce in a state whose goto entry mismatches, forcing GotoDefault[k1].
func TestDoReduceGotoDefault(t *testing.T) {
	tb := &Tables{
		GotoTable:   []int{0},  // a cell
		GotoCheck:   []int{99}, // check that will never match k1
		GotoDefault: []int{42}, // the default goto for nt index 0
		GotoPointer: []int{0},  // pointer 0 -> i = 0 + state
		NtBase:      1,
		ReduceTable: []int{0, 0, 0, 1, 1, 7}, // rule act=-1: len1, reduce_to=1, method 7
	}
	p := &Parser{Tables: tb, Reduce: func(int, []any, any) any { return "x" }}
	// State has a base plus the to-be-popped state; after popping len=1, state[-1]
	// is 0 so i = pointer(0)+0 = 0 indexes the (mismatching) goto cell.
	p.state = []int{0, 0}
	p.vstack = []any{"a"} // one value to pop (len 1)

	got := p.doReduce(-1)
	// goto check (99) != k1 (reduce_to 1 - nt_base 1 = 0) -> use goto_default[0]=42.
	if got != 42 {
		t.Fatalf("doReduce -> %d, want goto_default 42", got)
	}
}

// startReduceYyacceptTables: state 0 reduces by default (no action_pointer), and the
// reduce action calls Yyaccept, so Yyparse's pre-token loop runs a continuation
// (covering its `act = next` arm).
func TestYyparsePreTokenContinuation(t *testing.T) {
	tb := &Tables{
		ActionDefault: []int{-1, 2}, // state0 reduce rule1; state1 accept (unused)
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
	}
	p := &Parser{Tables: tb}
	p.Reduce = func(int, []any, any) any { p.Yyaccept(); return nil }
	got, err := p.Yyparse(func(yield func(any, any)) {})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// Yyaccept returns vstack[0], which is nil at this point.
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}

// TestYyparseDrainContinuation covers the second drain loop's `act2 = next`
// continuation: inside the iterator, after a token, a reduce action calls Yyaccept
// while draining. We drive the calc grammar and Yyaccept on the first reduce.
func TestYyparseDrainContinuation(t *testing.T) {
	p := &Parser{Tables: calcTables()}
	p.Reduce = func(ruleIdx int, values []any, result any) any {
		if ruleIdx == 5 {
			p.Yyaccept()
		}
		return calcReduce(ruleIdx, values, result)
	}
	got, err := p.Yyparse(func(yield func(any, any)) {
		yield(Symbol("NUMBER"), 1)
		yield(Symbol("NUMBER"), 2) // forces a drain/reduce of the first NUMBER
		yield(nil, nil)
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	_ = got // value is whatever vstack[0] holds at yyaccept; the point is coverage
}
