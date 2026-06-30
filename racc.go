// Package racc is a pure-Go (CGO=0) port of MRI's Racc::Parser runtime — the
// table-driven LALR(1) parse engine that every racc-generated Ruby parser drives
// via `include Racc::Parser`.
//
// This is the RUNTIME only, not the racc generator. A racc-generated parser file
// embeds a constant `Racc_arg` (the parse tables: action/goto/reduce tables, the
// token-value table, the shift/reduce counts, …) plus the generated reduce-action
// methods (`_reduce_N`) and a `next_token` lexer. At parse time, MRI's
// `_racc_do_parse_rb` / `_racc_yyparse_rb` interpret those tables, calling back
// into the lexer and the reduce actions. This package reproduces that interpreter
// byte-for-byte on the parse trajectory: given the same tables and the same token
// stream, it performs the same shift/reduce/accept/error sequence and returns the
// same result as MRI running its Ruby runtime core.
//
// The engine is pure compute. The host (e.g. rbgo) injects three SEAMS that, in a
// generated parser, are the user's Ruby code:
//
//   - NextToken: the lexer (`next_token` / the iterator yielded by yyparse). It
//     returns the next [tokenSymbol, value] pair; symbol == nil signals EOF, which
//     MRI spells `[false, false]`.
//   - Reduce: the reduce-action dispatch (`__send__(method_id, val, vstack[, result])`),
//     i.e. the generated `_reduce_N` callbacks. Given the rule index (the racc
//     `method_id`) and the slice of popped symbol values, it returns the value of
//     the produced nonterminal.
//   - OnError: the parse-error handler (`on_error`). It is called the first time an
//     error action fires; returning normally enters racc's error-recovery mode,
//     and returning an error aborts the parse with that error. The default handler
//     ([Parser.OnError] unset) raises a [ParseError], matching MRI.
//
// The Tables layout mirrors MRI's `Racc_arg` array exactly so a generated parser's
// tables drive this engine unchanged; see [Tables] for the index mapping.
package racc

import "fmt"

// ParseError is the error MRI's Racc::Parser raises (and rescues) on a parse
// error. It corresponds to Ruby's `Racc::ParseError < StandardError`.
type ParseError struct {
	// Tok is the internal token id of the token that caused the error
	// (MRI's ERROR_TOKEN_ID).
	Tok int
	// Val is the value of the offending token (MRI's ERROR_VALUE).
	Val any
	// Msg is the rendered message, matching MRI's
	// "parse error on value <val.inspect> (<token_to_str or '?'>)".
	Msg string
}

func (e *ParseError) Error() string { return e.Msg }

// Tables holds a racc-generated parser's parse tables. The field order and
// semantics mirror MRI's `Racc_arg` array element-for-element (the comment after
// each field is the `Racc_arg` index), so the constants emitted by `racc` map
// directly onto this struct:
//
//	Racc_arg = [
//	  action_table, action_check, action_default, action_pointer,   # 0..3
//	  goto_table,   goto_check,   goto_default,   goto_pointer,      # 4..7
//	  nt_base,      reduce_table, token_table,    shift_n,           # 8..11
//	  reduce_n,     use_result_var [, ...] ]                         # 12..13
//
// The action/goto tables use racc's compressed double-array layout: an entry at
// pointer+input is valid only when the parallel *_check array equals the current
// state (resp. nonterminal index), otherwise the *_default for that state is used.
type Tables struct {
	ActionTable   []int // [0] compressed action table (negative=reduce, positive=shift, shift_n=accept)
	ActionCheck   []int // [1] validity check parallel to ActionTable
	ActionDefault []int // [2] default action per state
	ActionPointer []int // [3] per-state base offset into ActionTable (nil ⇒ use default)

	GotoTable   []int // [4] compressed goto table
	GotoCheck   []int // [5] validity check parallel to GotoTable
	GotoDefault []int // [6] default goto per produced nonterminal
	GotoPointer []int // [7] per-state base offset into GotoTable (nil ⇒ use default)

	NtBase      int         // [8]  first nonterminal id; reduce_to-NtBase indexes the goto tables
	ReduceTable []int       // [9]  flat triples (len, reduce_to, method_id) indexed by act*-3
	TokenTable  map[any]int // [10] maps an external token symbol to its internal id (EOF symbol ⇒ 0)
	ShiftN      int         // [11] boundary: 0<act<ShiftN ⇒ shift; act==ShiftN ⇒ accept
	ReduceN     int         // [12] boundary: 0>act>-ReduceN ⇒ reduce; act==-ReduceN ⇒ error

	UseResult bool // [13] whether reduce actions take the extra `result` argument

	// TokenToS optionally maps an internal token id to its display string
	// (MRI's Racc_token_to_s_table), used by [Tables.TokenToStr] and in the
	// default parse-error message. It is not part of Racc_arg.
	TokenToS []string
}

// nilPointer reports whether ActionPointer[state] is the racc "no entry" marker.
// In MRI the pointer table stores nil for states with no shift/goto entries; here
// the sentinel is encoded by ActionPointerNil. A state index out of range is also
// treated as having no pointer.
func (t *Tables) actionPointer(state int) (int, bool) {
	if state < 0 || state >= len(t.ActionPointer) {
		return 0, false
	}
	v := t.ActionPointer[state]
	if v == ptrNil {
		return 0, false
	}
	return v, true
}

func (t *Tables) gotoPointer(k1 int) (int, bool) {
	if k1 < 0 || k1 >= len(t.GotoPointer) {
		return 0, false
	}
	v := t.GotoPointer[k1]
	if v == ptrNil {
		return 0, false
	}
	return v, true
}

// ptrNil is the sentinel value stored in ActionPointer / GotoPointer for entries
// that are `nil` in MRI's tables. MRI distinguishes "no pointer" (nil) from a real
// offset of 0; Go int slices cannot hold nil, so generated tables encode the nil
// slots with this sentinel. It is deliberately a large negative number that can
// never be a legitimate racc table offset.
const ptrNil = -1 << 30

// tableAt returns table[i] together with whether i is a valid in-range index, so
// callers can reproduce MRI's `act = table[i]` short-circuit (where an out-of-range
// or "false-y" entry causes a fallthrough to the default action). MRI also treats a
// stored value of nil/false as absent; generated action/goto tables use nilCell for
// those holes.
func tableAt(table []int, i int) (int, bool) {
	if i < 0 || i >= len(table) {
		return 0, false
	}
	v := table[i]
	if v == nilCell {
		return 0, false
	}
	return v, true
}

// nilCell marks an empty slot in ActionTable / GotoTable (MRI stores nil there).
// Like ptrNil it is an impossible real value so it can never collide with a genuine
// state number or action code.
const nilCell = -1 << 30

// TokenToStr returns the display string for an internal token id, mirroring MRI's
// Racc::Parser#token_to_str (returns "" when there is no mapping, where MRI returns
// nil). It is exposed because OnError handlers commonly want it.
func (t *Tables) TokenToStr(tok int) string {
	if tok < 0 || tok >= len(t.TokenToS) {
		return ""
	}
	return t.TokenToS[tok]
}

// Parser is the table-driven LALR(1) parse engine. Construct it with a [Tables]
// and the host seams, then call [Parser.DoParse] or [Parser.Yyparse].
//
// A zero Parser is not usable; Tables and NextToken (for DoParse) / the iterator
// (for Yyparse) plus Reduce must be set.
type Parser struct {
	// Tables are the racc-generated parse tables driving the automaton.
	Tables *Tables

	// NextToken is the lexer seam, used by DoParse. It returns the next token's
	// internal-or-external symbol and value. A nil symbol means EOF (MRI's
	// `[false, false]`). The returned symbol is looked up in Tables.TokenTable;
	// an unknown non-nil symbol maps to the error token (internal id 1).
	NextToken func() (sym any, val any)

	// Reduce is the reduce-action dispatch seam (the generated `_reduce_N`). It is
	// called with the racc rule's method_id and the slice of popped symbol values
	// (MRI's `val`), and returns the value of the produced nonterminal. When
	// Tables.UseResult is true the action conventionally starts from values[0]
	// (MRI passes `tmp_v[0]` as the initial `result`); that initial value is also
	// provided here as result for parity with the generated `def _reduce_N(val, _values, result)`.
	Reduce func(methodID int, values []any, result any) any

	// OnError is the parse-error seam (MRI's `on_error`). It is called the first
	// time an error action fires, with the offending token id/value and a snapshot
	// of the value stack (callers must not mutate it). Returning nil enters racc's
	// error-recovery mode; returning a non-nil error aborts the parse with that
	// error. When nil, the engine raises a ParseError, matching MRI's default.
	OnError func(tok int, val any, valueStack []any) error

	// state, vstack, tstack mirror MRI's @racc_state / @racc_vstack / @racc_tstack.
	state  []int
	vstack []any

	raccT         int  // @racc_t : internal id of the current lookahead token
	raccVal       any  // @racc_val : value of the current lookahead token
	raccReadNext  bool // @racc_read_next : whether to pull a fresh token
	raccUserError bool // @racc_user_yyerror : a reduce action invoked Yyerror
	raccErrStatus int  // @racc_error_status : error-recovery countdown
	tStarted      bool // tracks whether @racc_t has been assigned (MRI starts it nil)
}

// initSysvars mirrors Racc::Parser#_racc_init_sysvars.
func (p *Parser) initSysvars() {
	p.state = []int{0}
	p.vstack = nil
	p.raccT = 0
	p.tStarted = false // MRI: @racc_t starts nil, distinct from 0 (EOF)
	p.raccVal = nil
	p.raccReadNext = true
	p.raccUserError = false
	p.raccErrStatus = 0
}

// jumpCode carries the result of MRI's `throw :racc_jump, code` out of a reduce
// action: 1 == Yyerror, 2 == Yyaccept. It is propagated via panic and recovered
// around the reduce call, exactly where MRI's `catch(:racc_jump)` sits.
type jumpCode int

// endParse carries MRI's `throw :racc_end_parse, value`: the engine unwinds to the
// top-level catch and returns value. It is propagated via panic and recovered in
// DoParse / Yyparse.
type endParse struct {
	val any
}

// abortErr carries an error returned by OnError out to the top-level entry point.
type abortErr struct {
	err error
}

// DoParse runs the parser using the NextToken lexer seam. It is the Go counterpart
// of Racc::Parser#do_parse → _racc_do_parse_rb. It returns the accepted result
// (MRI's Symbol_Value_Stack[0]) or a *ParseError (or the error returned by a custom
// OnError) on failure.
func (p *Parser) DoParse() (result any, err error) {
	defer p.recoverTop(&result, &err)
	p.initSysvars()
	t := p.Tables

	for {
		var act int
		if i, ok := t.actionPointer(p.state[len(p.state)-1]); ok {
			if p.raccReadNext {
				if !(p.tStarted && p.raccT == 0) { // not EOF
					tok, val := p.NextToken()
					p.raccVal = val
					if tok == nil {
						p.raccT = 0 // EOF
					} else if id, found := t.TokenTable[tok]; found {
						p.raccT = id
					} else {
						p.raccT = 1 // error token
					}
					p.tStarted = true
					p.raccReadNext = false
				}
			}
			i += p.raccT
			var got int
			var ok2 bool
			if i >= 0 {
				if got, ok2 = tableAt(t.ActionTable, i); ok2 {
					if at, _ := tableAt(t.ActionCheck, i); at == p.state[len(p.state)-1] {
						act = got
						ok2 = true
					} else {
						ok2 = false
					}
				}
			}
			if !ok2 {
				act = t.ActionDefault[p.state[len(p.state)-1]]
			}
		} else {
			act = t.ActionDefault[p.state[len(p.state)-1]]
		}
		for {
			next, cont := p.evalact(act)
			if !cont {
				break
			}
			act = next
		}
	}
}

// Yyparse runs the parser using a pull-style iterator seam instead of NextToken,
// mirroring Racc::Parser#yyparse → _racc_yyparse_rb. The yield function passed to
// iter must be called once per token with (sym, val) (nil sym == EOF), exactly as
// MRI's RECEIVER#METHOD_ID yields [tok, val]. It returns the accepted result or an
// error, like DoParse.
//
// In a generated parser, `yyparse(recv, mid)` drives the automaton from tokens that
// recv.mid yields; here the caller supplies that iterator directly as iter.
func (p *Parser) Yyparse(iter func(yield func(sym any, val any))) (result any, err error) {
	defer p.recoverTop(&result, &err)
	p.initSysvars()
	t := p.Tables

	// Consume any reductions possible before the first token is needed (MRI's
	// `until i = action_pointer[state]` loop).
	var i int
	for {
		if v, ok := t.actionPointer(p.state[len(p.state)-1]); ok {
			i = v
			break
		}
		act := t.ActionDefault[p.state[len(p.state)-1]]
		for {
			next, cont := p.evalact(act)
			if !cont {
				break
			}
			act = next
		}
	}

	iter(func(tok any, val any) {
		if tok == nil {
			p.raccT = 0
		} else if id, found := t.TokenTable[tok]; found {
			p.raccT = id
		} else {
			p.raccT = 1
		}
		p.tStarted = true
		p.raccVal = val
		p.raccReadNext = false

		i += p.raccT
		act := p.lookupAction(i)
		for {
			next, cont := p.evalact(act)
			if !cont {
				break
			}
			act = next
		}

		// Drain reductions until another real token is required.
		for {
			pv, ok := t.actionPointer(p.state[len(p.state)-1])
			if ok && p.raccReadNext && p.raccT != 0 {
				i = pv
				break
			}
			var act2 int
			if !ok {
				act2 = t.ActionDefault[p.state[len(p.state)-1]]
			} else {
				ii := pv + p.raccT
				act2 = p.lookupAction(ii)
			}
			for {
				next, cont := p.evalact(act2)
				if !cont {
					break
				}
				act2 = next
			}
		}
	})

	// MRI: if the iterator returns without the parse having ended, the result is
	// nil (the catch block falls through). Reaching here means no accept/error
	// throw fired, so return nil with no error.
	return nil, nil
}

// lookupAction reproduces MRI's "validated action table read, else default" used
// in the yyparse paths: act = action_table[i] if i>=0 and the check matches the
// current state, otherwise action_default[state].
func (p *Parser) lookupAction(i int) int {
	t := p.Tables
	cur := p.state[len(p.state)-1]
	if i >= 0 {
		if got, ok := tableAt(t.ActionTable, i); ok {
			if at, _ := tableAt(t.ActionCheck, i); at == cur {
				return got
			}
		}
	}
	return t.ActionDefault[cur]
}

// recoverTop turns the panic-propagated control-flow signals (endParse / abortErr)
// into ordinary returns from DoParse / Yyparse. Any other panic is re-raised.
func (p *Parser) recoverTop(result *any, err *error) {
	if r := recover(); r != nil {
		switch v := r.(type) {
		case endParse:
			*result = v.val
			*err = nil
		case abortErr:
			*result = nil
			*err = v.err
		default:
			panic(r)
		}
	}
}

// evalact is the core action dispatcher, a port of Racc::Parser#_racc_evalact. It
// returns (nextAction, true) when the caller must loop with the returned action
// (MRI's `while act = _racc_evalact(...)`), or (_, false) when the inner loop ends
// (MRI's _racc_evalact returning nil).
func (p *Parser) evalact(act int) (int, bool) {
	t := p.Tables

	switch {
	case act > 0 && act < t.ShiftN:
		// shift
		if p.raccErrStatus > 0 {
			if p.raccT > 1 { // not error token or EOF
				p.raccErrStatus--
			}
		}
		p.vstack = append(p.vstack, p.raccVal)
		p.state = append(p.state, act)
		p.raccReadNext = true

	case act < 0 && act > -t.ReduceN:
		// reduce
		code := p.doReduceCatch(act)
		if code != 0 {
			switch code {
			case 1: // yyerror
				p.raccUserError = true
				return -t.ReduceN, true
			case 2: // yyaccept
				return t.ShiftN, true
			default:
				panic("[Racc Bug] unknown jump code")
			}
		}

	case act == t.ShiftN:
		// accept
		var top any
		if len(p.vstack) > 0 {
			top = p.vstack[0]
		}
		panic(endParse{val: top})

	case act == -t.ReduceN:
		// error
		switch p.raccErrStatus {
		case 0:
			if !p.raccUserError {
				if e := p.callOnError(); e != nil {
					panic(abortErr{err: e})
				}
			}
		case 3:
			if p.raccT == 0 { // is $
				panic(endParse{val: nil})
			}
			p.raccReadNext = true
		}
		p.raccUserError = false
		p.raccErrStatus = 3
		for {
			if i, ok := t.actionPointer(p.state[len(p.state)-1]); ok {
				i++ // error token
				if i >= 0 {
					if a, ok2 := tableAt(t.ActionTable, i); ok2 {
						if at, _ := tableAt(t.ActionCheck, i); at == p.state[len(p.state)-1] {
							return a, true
						}
					}
				}
			}
			if len(p.state) <= 1 {
				panic(endParse{val: nil})
			}
			p.state = p.state[:len(p.state)-1]
			p.vstack = p.vstack[:len(p.vstack)-1]
		}

	default:
		panic(fmt.Sprintf("[Racc Bug] unknown action %d", act))
	}

	return 0, false
}

// doReduceCatch wraps doReduce with the recover that corresponds to MRI's
// `catch(:racc_jump)` around the reduce. It returns the jump code (1=yyerror,
// 2=yyaccept) or 0 when the reduce completed normally.
func (p *Parser) doReduceCatch(act int) (code int) {
	defer func() {
		if r := recover(); r != nil {
			if jc, ok := r.(jumpCode); ok {
				code = int(jc)
				return
			}
			panic(r)
		}
	}()
	p.state = append(p.state, p.doReduce(act))
	return 0
}

// doReduce performs one reduction, a port of Racc::Parser#_racc_do_reduce. It pops
// `len` symbols, invokes the Reduce seam for the rule's method_id, pushes the
// produced value, and returns the goto-determined next state.
func (p *Parser) doReduce(act int) int {
	t := p.Tables
	i := act * -3
	length := t.ReduceTable[i]
	reduceTo := t.ReduceTable[i+1]
	methodID := t.ReduceTable[i+2]

	// tmp_v = vstack[-len, len]; vstack[-len, len] = []
	n := len(p.vstack)
	tmpV := make([]any, length)
	copy(tmpV, p.vstack[n-length:n])
	p.vstack = p.vstack[:n-length]

	// state[-len, len] = []
	p.state = p.state[:len(p.state)-length]

	var produced any
	if t.UseResult {
		var result any
		if length > 0 {
			result = tmpV[0]
		}
		produced = p.Reduce(methodID, tmpV, result)
	} else {
		produced = p.Reduce(methodID, tmpV, nil)
	}
	p.vstack = append(p.vstack, produced)

	k1 := reduceTo - t.NtBase
	if i, ok := t.gotoPointer(k1); ok {
		i += p.state[len(p.state)-1]
		if i >= 0 {
			if curstate, ok2 := tableAt(t.GotoTable, i); ok2 {
				if gc, _ := tableAt(t.GotoCheck, i); gc == k1 {
					return curstate
				}
			}
		}
	}
	return t.GotoDefault[k1]
}

// callOnError dispatches to the OnError seam, or raises the default ParseError when
// no handler is set, mirroring Racc::Parser#on_error.
func (p *Parser) callOnError() error {
	if p.OnError != nil {
		snap := make([]any, len(p.vstack))
		copy(snap, p.vstack)
		return p.OnError(p.raccT, p.raccVal, snap)
	}
	tokStr := p.Tables.TokenToStr(p.raccT)
	if tokStr == "" {
		tokStr = "?"
	}
	return &ParseError{
		Tok: p.raccT,
		Val: p.raccVal,
		Msg: fmt.Sprintf("parse error on value %s (%s)", rubyInspect(p.raccVal), tokStr),
	}
}

// Yyerror enters error-recovery mode from inside a reduce action, like MRI's
// Racc::Parser#yyerror (`throw :racc_jump, 1`). Call it only from within a Reduce
// callback; it unwinds to the reduce dispatcher.
func (p *Parser) Yyerror() { panic(jumpCode(1)) }

// Yyaccept exits the parser from inside a reduce action, like MRI's
// Racc::Parser#yyaccept (`throw :racc_jump, 2`). Call it only from within a Reduce
// callback.
func (p *Parser) Yyaccept() { panic(jumpCode(2)) }

// Yyerrok leaves error-recovery mode, like MRI's Racc::Parser#yyerrok. Call it from
// within a Reduce/OnError seam.
func (p *Parser) Yyerrok() { p.raccErrStatus = 0 }
