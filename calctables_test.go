package racc

// calcTables is the racc-generated parse table for the tiny calculator grammar
//
//	target: exp
//	exp: exp '+' exp | exp '*' exp | '(' exp ')' | NUMBER
//
// It is the exact Racc_arg emitted by `racc` (racc 1.8.1, MRI 4.0.5) for that
// grammar, transcribed field-for-field. MRI's dump of Calc::Racc_arg was:
//
//	[0]  action_table   = [6, 7, 3, 12, 4, 3, 3, 4, 4, 3, 5, 4, 6, 7, 6, 7, 6, 7, 9]
//	[1]  action_check   = [8, 8, 0, 8, 0, 3, 6, 3, 6, 7, 1, 7, 2, 2, 10, 10, 11, 11, 5]
//	[2]  action_default = [-6, -6, -1, -6, -5, -6, -6, -6, -6, 13, -2, -3, -4]
//	[3]  action_pointer = [-2, 10, 10, 1, nil, 18, 2, 5, -2, nil, 12, 14, nil]
//	[4]  goto_table     = [2, 1, nil, 8, nil, nil, 10, 11]
//	[5]  goto_check     = [2, 1, nil, 2, nil, nil, 2, 2]
//	[6]  goto_default   = [nil, nil, nil]
//	[7]  goto_pointer   = [nil, 1, 0]
//	[8]  nt_base        = 7
//	[9]  reduce_table   = [0,0,:racc_error, 1,8,:_reduce_1, 3,9,:_reduce_2,
//	                       3,9,:_reduce_3, 3,9,:_reduce_4, 1,9,:_reduce_5]
//	[10] token_table    = {false=>0, error=>1, "+"=>2, "*"=>3, "("=>4, ")"=>5, NUMBER=>6}
//	[11] shift_n        = 13
//	[12] reduce_n       = 6
//	[13] use_result     = true
//
// nil cells become ptrNil (pointer tables) or nilCell (value tables). The reduce
// method ids map to rule indices: racc_error=0, _reduce_1..5 = 1..5.
func calcTables() *Tables {
	return &Tables{
		ActionTable:   []int{6, 7, 3, 12, 4, 3, 3, 4, 4, 3, 5, 4, 6, 7, 6, 7, 6, 7, 9},
		ActionCheck:   []int{8, 8, 0, 8, 0, 3, 6, 3, 6, 7, 1, 7, 2, 2, 10, 10, 11, 11, 5},
		ActionDefault: []int{-6, -6, -1, -6, -5, -6, -6, -6, -6, 13, -2, -3, -4},
		ActionPointer: []int{-2, 10, 10, 1, ptrNil, 18, 2, 5, -2, ptrNil, 12, 14, ptrNil},

		GotoTable:   []int{2, 1, nilCell, 8, nilCell, nilCell, 10, 11},
		GotoCheck:   []int{2, 1, nilCell, 2, nilCell, nilCell, 2, 2},
		GotoDefault: []int{nilCell, nilCell, nilCell},
		GotoPointer: []int{ptrNil, 1, 0},

		NtBase: 7,
		ReduceTable: []int{
			0, 0, 0, // racc_error
			1, 8, 1, // _reduce_1
			3, 9, 2, // _reduce_2
			3, 9, 3, // _reduce_3
			3, 9, 4, // _reduce_4
			1, 9, 5, // _reduce_5
		},
		TokenTable: map[any]int{
			nil:              0, // false (EOF) — but EOF is signalled by nil sym, see below
			Symbol("error"):  1,
			"+":              2,
			"*":              3,
			"(":              4,
			")":              5,
			Symbol("NUMBER"): 6,
		},
		ShiftN:    13,
		ReduceN:   6,
		UseResult: true,
		// MRI's Racc_token_to_s_table renders character-literal terminals as the
		// double-quoted form (e.g. `"+"`, three chars), so the default error
		// message reads `... ("+")`.
		TokenToS: []string{
			"$end",   // 0
			"error",  // 1
			`"+"`,    // 2
			`"*"`,    // 3
			`"("`,    // 4
			`")"`,    // 5
			"NUMBER", // 6
		},
	}
}

// calcReduce is the reduce-action seam for the calc grammar — the Go equivalent of
// the generated `_reduce_N` methods. ruleIdx is the racc method id from the reduce
// table. With UseResult the convention is to start from result (== values[0]).
func calcReduce(ruleIdx int, values []any, result any) any {
	switch ruleIdx {
	case 1: // target: exp        -> val[0]
		return values[0]
	case 2: // exp: exp '+' exp   -> val[0] + val[2]
		return values[0].(int) + values[2].(int)
	case 3: // exp: exp '*' exp   -> val[0] * val[2]
		return values[0].(int) * values[2].(int)
	case 4: // exp: '(' exp ')'   -> val[1]
		return values[1]
	case 5: // exp: NUMBER        -> val[0]
		return values[0]
	default: // ruleIdx 0 is racc_error, never reduced in a valid parse
		return result
	}
}

// num/op build a calc token stream for the lexer seam.
func num(n int) [2]any   { return [2]any{Symbol("NUMBER"), n} }
func op(s string) [2]any { return [2]any{s, s} }

var eofTok = [2]any{nil, nil}
