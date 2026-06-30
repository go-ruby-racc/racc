package racc

// stmtTables is the racc-generated parse table for a statement-list grammar with
// an explicit `error` recovery rule:
//
//	program: stmts
//	stmts: stmt | stmts stmt
//	stmt: NUMBER ';' | error ';'
//
// It is the exact Racc_arg emitted by racc 1.8.1 (MRI 4.0.5) for that grammar.
// MRI's Stmt::Racc_arg dump:
//
//	[0]  action_table   = [5, 4, 5, 4, 6, 8, 9, 10]
//	[1]  action_check   = [0, 0, 2, 2, 1, 4, 5, 6]
//	[2]  action_default = [-6, -6, -1, -2, -6, -6, -6, -3, -4, -5, 11]
//	[3]  action_pointer = [-1, 4, 1, nil, 2, 3, 7, nil, nil, nil, nil]
//	[4]  goto_table     = [3, 1, 7, 2]
//	[5]  goto_check     = [3, 1, 3, 2]
//	[6]  goto_default   = [nil, nil, nil, nil]
//	[7]  goto_pointer   = [nil, 1, 3, 0]
//	[8]  nt_base        = 4
//	[9]  reduce_table   = [0,0,racc_error, 1,5,_reduce_1, 1,6,_reduce_2,
//	                       2,6,_reduce_3, 2,7,_reduce_4, 2,7,_reduce_5]
//	[10] token_table    = {false=>0, error=>1, NUMBER=>2, ";"=>3}
//	[11] shift_n        = 11
//	[12] reduce_n       = 6
//	[13] use_result     = true
//
// This grammar's `error ';'` rule exercises the engine's error-recovery path (the
// `error` token shift after popping the stack).
func stmtTables() *Tables {
	return &Tables{
		ActionTable:   []int{5, 4, 5, 4, 6, 8, 9, 10},
		ActionCheck:   []int{0, 0, 2, 2, 1, 4, 5, 6},
		ActionDefault: []int{-6, -6, -1, -2, -6, -6, -6, -3, -4, -5, 11},
		ActionPointer: []int{-1, 4, 1, ptrNil, 2, 3, 7, ptrNil, ptrNil, ptrNil, ptrNil},

		GotoTable:   []int{3, 1, 7, 2},
		GotoCheck:   []int{3, 1, 3, 2},
		GotoDefault: []int{nilCell, nilCell, nilCell, nilCell},
		GotoPointer: []int{ptrNil, 1, 3, 0},

		NtBase: 4,
		ReduceTable: []int{
			0, 0, 0, // racc_error
			1, 5, 1, // _reduce_1  program: stmts
			1, 6, 2, // _reduce_2  stmts: stmt
			2, 6, 3, // _reduce_3  stmts: stmts stmt
			2, 7, 4, // _reduce_4  stmt: NUMBER ';'
			2, 7, 5, // _reduce_5  stmt: error ';'
		},
		TokenTable: map[any]int{
			nil:              0,
			Symbol("error"):  1,
			Symbol("NUMBER"): 2,
			";":              3,
		},
		ShiftN:    11,
		ReduceN:   6,
		UseResult: true,
		TokenToS: []string{
			"$end",   // 0
			"error",  // 1
			"NUMBER", // 2
			"';'",    // 3
		},
	}
}

func stmtReduce(ruleIdx int, values []any, result any) any {
	switch ruleIdx {
	case 1: // program: stmts
		return values[0]
	case 2: // stmts: stmt
		return []any{values[0]}
	case 3: // stmts: stmts stmt
		return append(values[0].([]any), values[1])
	case 4: // stmt: NUMBER ';'
		return values[0]
	case 5: // stmt: error ';'
		return Symbol("recovered")
	default:
		return result
	}
}
