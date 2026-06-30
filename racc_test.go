package racc

import (
	"errors"
	"reflect"
	"testing"
)

// trace records the parse trajectory (the reduce sequence) so it can be compared
// against MRI's trajectory for the same tables and token stream.
type trace struct {
	reduces [][2]any // each entry: {ruleIdx, copy-of-values}
}

func newCalcParser(toks [][2]any, tr *trace) *Parser {
	q := append([][2]any(nil), toks...)
	p := &Parser{Tables: calcTables()}
	p.NextToken = func() (any, any) {
		t := q[0]
		q = q[1:]
		return t[0], t[1]
	}
	p.Reduce = func(ruleIdx int, values []any, result any) any {
		if tr != nil {
			vc := append([]any(nil), values...)
			tr.reduces = append(tr.reduces, [2]any{ruleIdx, vc})
		}
		return calcReduce(ruleIdx, values, result)
	}
	return p
}

func TestDoParseSimple(t *testing.T) {
	tr := &trace{}
	// 1 + 2 * 3 ; '*' binds tighter (racc resolves the s/r conflict that way).
	p := newCalcParser([][2]any{num(1), op("+"), num(2), op("*"), num(3), eofTok}, tr)
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 7 {
		t.Fatalf("result = %v, want 7", got)
	}
	// MRI trajectory: reduce 5,5,5 then 3 (2*3), then 2 (1+6), then 1.
	wantRules := []int{5, 5, 5, 3, 2, 1}
	if len(tr.reduces) != len(wantRules) {
		t.Fatalf("reduce count = %d, want %d (%v)", len(tr.reduces), len(wantRules), tr.reduces)
	}
	for i, r := range tr.reduces {
		if r[0].(int) != wantRules[i] {
			t.Fatalf("reduce[%d] rule = %v, want %d", i, r[0], wantRules[i])
		}
	}
	// Spot-check the values passed to the multiply and add reductions.
	if v := tr.reduces[3][1].([]any); !reflect.DeepEqual(v, []any{2, "*", 3}) {
		t.Fatalf("reduce 3 values = %v, want [2 * 3]", v)
	}
	if v := tr.reduces[4][1].([]any); !reflect.DeepEqual(v, []any{1, "+", 6}) {
		t.Fatalf("reduce 2 values = %v, want [1 + 6]", v)
	}
}

func TestDoParseParens(t *testing.T) {
	// (1 + 2) * 3 = 9
	p := newCalcParser([][2]any{
		op("("), num(1), op("+"), num(2), op(")"), op("*"), num(3), eofTok,
	}, nil)
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 9 {
		t.Fatalf("result = %v, want 9", got)
	}
}

func TestDoParseSingleNumber(t *testing.T) {
	p := newCalcParser([][2]any{num(42), eofTok}, nil)
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Fatalf("result = %v, want 42", got)
	}
}

func TestDoParseError(t *testing.T) {
	// 1 + + 2 -> parse error on the second '+'
	p := newCalcParser([][2]any{num(1), op("+"), op("+"), num(2), eofTok}, nil)
	_, err := p.DoParse()
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v (%T), want *ParseError", err, err)
	}
	// MRI: parse error on value "+" ("+")  (the token-to-s of '+' is the
	// double-quoted literal in Racc_token_to_s_table).
	want := `parse error on value "+" ("+")`
	if pe.Msg != want {
		t.Fatalf("message = %q, want %q", pe.Msg, want)
	}
	if pe.Tok != 2 { // internal id of '+'
		t.Fatalf("Tok = %d, want 2", pe.Tok)
	}
	if pe.Val != "+" {
		t.Fatalf("Val = %v, want +", pe.Val)
	}
	if pe.Error() != want {
		t.Fatalf("Error() = %q, want %q", pe.Error(), want)
	}
}

func TestOnErrorSeam(t *testing.T) {
	var capturedTok int
	var capturedVal any
	var capturedStack []any
	sentinel := errors.New("custom abort")

	p := newCalcParser([][2]any{num(1), op("+"), op("+"), num(2), eofTok}, nil)
	p.OnError = func(tok int, val any, vstack []any) error {
		capturedTok = tok
		capturedVal = val
		capturedStack = append([]any(nil), vstack...)
		return sentinel
	}
	_, err := p.DoParse()
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if capturedTok != 2 {
		t.Fatalf("OnError tok = %d, want 2", capturedTok)
	}
	if capturedVal != "+" {
		t.Fatalf("OnError val = %v, want +", capturedVal)
	}
	// At the point of error the value stack holds [1, "+"] (1 shifted+reduced to exp,
	// then '+' shifted) — the snapshot must be non-empty and must not alias internal state.
	if len(capturedStack) == 0 {
		t.Fatalf("OnError stack snapshot empty")
	}
}

func TestOnErrorRecovers(t *testing.T) {
	// An OnError that returns nil enters racc error-recovery mode. The calc grammar
	// has no `error` rule, so recovery pops the stack and ultimately fails to nil,
	// but exercising it covers the recovery loop. We assert the parse ends without
	// a *ParseError and without panicking.
	p := newCalcParser([][2]any{num(1), op("+"), op("+"), num(2), eofTok}, nil)
	p.OnError = func(int, any, []any) error { return nil }
	got, err := p.DoParse()
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got != nil {
		t.Fatalf("got = %v, want nil (recovery exhausted)", got)
	}
}

func TestYyparseSimple(t *testing.T) {
	toks := [][2]any{num(2), op("*"), num(5), op("+"), num(1), eofTok}
	p := &Parser{Tables: calcTables(), Reduce: calcReduce}
	got, err := p.Yyparse(func(yield func(any, any)) {
		for _, tk := range toks {
			yield(tk[0], tk[1])
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 2 * 5 + 1: with racc's default conflict resolution this grammar groups as
	// 2 * (5 + 1) = 12 (verified against MRI's yyparse over the same tables).
	if got != 12 {
		t.Fatalf("result = %v, want 12", got)
	}
}

func TestYyparseParens(t *testing.T) {
	toks := [][2]any{op("("), num(3), op("+"), num(4), op(")"), eofTok}
	p := &Parser{Tables: calcTables(), Reduce: calcReduce}
	got, err := p.Yyparse(func(yield func(any, any)) {
		for _, tk := range toks {
			yield(tk[0], tk[1])
		}
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 7 {
		t.Fatalf("result = %v, want 7", got)
	}
}

func TestYyparseError(t *testing.T) {
	toks := [][2]any{num(1), op("*"), op("*"), eofTok}
	p := &Parser{Tables: calcTables(), Reduce: calcReduce}
	_, err := p.Yyparse(func(yield func(any, any)) {
		for _, tk := range toks {
			yield(tk[0], tk[1])
		}
	})
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v (%T), want *ParseError", err, err)
	}
}

func TestUnknownTokenMapsToErrorToken(t *testing.T) {
	// A token symbol absent from TokenTable maps to the error token (id 1), which
	// the calc grammar cannot shift -> parse error. This covers the lookup-miss arm.
	p := &Parser{Tables: calcTables(), Reduce: calcReduce}
	q := [][2]any{{Symbol("BOGUS"), 0}, eofTok}
	i := 0
	p.NextToken = func() (any, any) { t := q[i]; i++; return t[0], t[1] }
	_, err := p.DoParse()
	var pe *ParseError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want *ParseError", err)
	}
	if pe.Tok != 1 {
		t.Fatalf("Tok = %d, want 1 (error token)", pe.Tok)
	}
}

func TestTokenToStr(t *testing.T) {
	tb := calcTables()
	if s := tb.TokenToStr(6); s != "NUMBER" {
		t.Fatalf("TokenToStr(6) = %q, want NUMBER", s)
	}
	if s := tb.TokenToStr(-1); s != "" {
		t.Fatalf("TokenToStr(-1) = %q, want empty", s)
	}
	if s := tb.TokenToStr(99); s != "" {
		t.Fatalf("TokenToStr(99) = %q, want empty", s)
	}
}
