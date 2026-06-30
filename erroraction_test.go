package racc

import (
	"reflect"
	"testing"
)

// TestStmtErrorActionRecovery drives a stream that triggers a *real* error action
// (unlike TestStmtErrorRecovery, where the error token shifts directly). On
// `NUMBER NUMBER ;` the second NUMBER is unexpected, so the engine calls on_error
// once, enters recovery, pops to a state that shifts the `error` token, then shifts
// ';' to complete the `error ';'` rule -> [:recovered]. Verified against MRI
// (on_error fires once, result [:recovered]).
func TestStmtErrorActionRecovery(t *testing.T) {
	var errs int
	p := newStmtParser([][2]any{
		num(1), num(2), op(";"), eofTok,
	})
	p.OnError = func(int, any, []any) error { errs++; return nil }
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if errs != 1 {
		t.Fatalf("on_error calls = %d, want 1", errs)
	}
	if !reflect.DeepEqual(got, []any{Symbol("recovered")}) {
		t.Fatalf("got = %#v, want [recovered]", got)
	}
}

// TestStmtErrorActionThenContinue extends recovery: after recovering from the bad
// `NUMBER NUMBER ;`, a further valid `NUMBER ;` is parsed. This exercises shifting
// real tokens while the error-recovery countdown is active (errStatus>0), i.e. the
// shift-arm decrement and the post-recovery continuation loops. MRI yields
// [:recovered, 3].
func TestStmtErrorActionThenContinue(t *testing.T) {
	var errs int
	p := newStmtParser([][2]any{
		num(1), num(2), op(";"), num(3), op(";"), eofTok,
	})
	p.OnError = func(int, any, []any) error { errs++; return nil }
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if errs != 1 {
		t.Fatalf("on_error calls = %d, want 1", errs)
	}
	want := []any{Symbol("recovered"), 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got = %#v, want %#v", got, want)
	}
}

// TestStmtDoubleError drives `NUMBER NUMBER NUMBER ;`: after the first unexpected
// NUMBER fires on_error and enters recovery (status 3), the next NUMBER errors again
// while status==3 and is silently discarded (the case-3 read-next arm) rather than
// re-reporting. MRI: on_error fires once, result [:recovered].
func TestStmtDoubleError(t *testing.T) {
	var errs int
	p := newStmtParser([][2]any{
		num(1), num(2), num(9), op(";"), eofTok,
	})
	p.OnError = func(int, any, []any) error { errs++; return nil }
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if errs != 1 {
		t.Fatalf("on_error calls = %d, want 1", errs)
	}
	if !reflect.DeepEqual(got, []any{Symbol("recovered")}) {
		t.Fatalf("got = %#v, want [recovered]", got)
	}
}

// TestStmtErrorRecoveryYyparse runs the error-action recovery through the Yyparse
// (iterator) entry point, exercising its post-error continuation loops. MRI's
// yyparse over `NUMBER NUMBER ;` yields [:recovered].
func TestStmtErrorRecoveryYyparse(t *testing.T) {
	toks := [][2]any{num(1), num(2), op(";"), eofTok}
	var errs int
	p := &Parser{Tables: stmtTables(), Reduce: stmtReduce}
	p.OnError = func(int, any, []any) error { errs++; return nil }
	got, err := p.Yyparse(func(yield func(any, any)) {
		for _, tk := range toks {
			yield(tk[0], tk[1])
		}
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if errs != 1 {
		t.Fatalf("on_error calls = %d, want 1", errs)
	}
	if !reflect.DeepEqual(got, []any{Symbol("recovered")}) {
		t.Fatalf("got = %#v, want [recovered]", got)
	}
}

// TestStmtLeadingError covers the error action when the very first token is bad
// (`;` with an empty stack): on_error fires, recovery pops to the start which has
// no error shift, exhausting to nil. MRI: result nil, on_error 1.
func TestStmtLeadingError(t *testing.T) {
	var errs int
	p := newStmtParser([][2]any{
		op(";"), eofTok,
	})
	p.OnError = func(int, any, []any) error { errs++; return nil }
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if errs != 1 {
		t.Fatalf("on_error calls = %d, want 1", errs)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil", got)
	}
}
