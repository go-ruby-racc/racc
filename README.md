<p align="center"><img src="https://raw.githubusercontent.com/go-ruby-racc/brand/main/social/go-ruby-racc-racc.png" alt="go-ruby-racc/racc" width="720"></p>

# racc — go-ruby-racc

[![Docs](https://img.shields.io/badge/docs-mkdocs--material-DC2626)](https://go-ruby-racc.github.io/docs/)
[![License](https://img.shields.io/badge/license-BSD--3--Clause-blue)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.26.4%2B-00ADD8)](https://go.dev/dl/)
[![Coverage](https://img.shields.io/badge/coverage-100%25-1a7f37)](#tests--coverage)

**A pure-Go (no cgo) reimplementation of the runtime at the heart of Ruby's
[Racc](https://github.com/ruby/racc)** — the table-driven LALR(1) parse engine
(`Racc::Parser`) that every racc-generated parser drives via `include
Racc::Parser`. It is a faithful port of MRI 4.0.5's Ruby runtime core
(`lib/racc/parser.rb`: `_racc_do_parse_rb` / `_racc_yyparse_rb` / `_racc_evalact`
/ `_racc_do_reduce`), so that — given the **same parse tables** — it walks the
**same shift/reduce/accept/error trajectory** and returns the **same result** as
MRI, **without any Ruby runtime**.

This is the **runtime only**, not the `racc` generator tool. A racc-generated
parser file embeds a `Racc_arg` table constant plus generated `_reduce_N` action
methods and a `next_token` lexer; at parse time MRI's runtime interprets those
tables. This package reproduces that interpreter so racc-generated gem parsers can
run on [go-embedded-ruby](https://github.com/go-embedded-ruby/ruby). It is a
**standalone, reusable** module — a sibling of
[go-ruby-marshal](https://github.com/go-ruby-marshal/marshal),
[go-ruby-regexp](https://github.com/go-ruby-regexp/regexp),
[go-ruby-erb](https://github.com/go-ruby-erb/erb), and
[go-ruby-pstore](https://github.com/go-ruby-pstore/pstore).

> **What it is — and isn't.** The parse automaton — read the action table for the
> current state and lookahead, shift / reduce / accept / recover, drive the goto
> tables, run the error-recovery state machine — is fully deterministic and needs
> **no interpreter**, so it lives here as pure Go over a `Tables` value (the racc
> `Racc_arg`). The grammar-specific halves are the **seams** the host injects: the
> lexer (`next_token`), the reduce actions (`_reduce_N`), and the error handler
> (`on_error`). rbgo wires these to the user's Ruby parser; the tests wire small Go
> closures, so the whole suite is deterministic and Ruby-free.

## How a generated parser's tables drive the engine

A `racc`-generated parser carries a `Racc_arg` array. Its 14 elements map
field-for-field onto [`Tables`](racc.go):

| `Racc_arg` index | `Tables` field | role |
| --- | --- | --- |
| 0–3 | `ActionTable` / `ActionCheck` / `ActionDefault` / `ActionPointer` | compressed action table (shift / reduce / accept / error per state × lookahead) |
| 4–7 | `GotoTable` / `GotoCheck` / `GotoDefault` / `GotoPointer` | compressed goto table (state after a reduction) |
| 8 | `NtBase` | first nonterminal id (`reduce_to − NtBase` indexes the goto tables) |
| 9 | `ReduceTable` | flat `(len, reduce_to, method_id)` triples per rule |
| 10 | `TokenTable` | external token symbol → internal id (EOF ⇒ 0) |
| 11–12 | `ShiftN` / `ReduceN` | the action-code band boundaries |
| 13 | `UseResult` | whether reduce actions take the extra `result` argument |

`nil` cells in the pointer / value tables are encoded with the package sentinels
`ptrNil` / `nilCell`. The engine consumes this `Tables` value exactly as MRI's
runtime consumes `Racc_arg`, so transcribing a generated parser's constants (or
emitting them from a code generator) is all that is needed to run that grammar.

## The seams

```go
type Parser struct {
	Tables    *Tables
	NextToken func() (sym any, val any)                       // the lexer (next_token); nil sym == EOF
	Reduce    func(methodID int, values []any, result any) any // the _reduce_N dispatch
	OnError   func(tok int, val any, valueStack []any) error   // on_error; nil enters recovery
}

func (p *Parser) DoParse() (result any, err error)                              // do_parse
func (p *Parser) Yyparse(iter func(yield func(sym, val any))) (result any, err error) // yyparse(recv, mid)
func (p *Parser) Yyerror()  // throw :racc_jump, 1  — from a Reduce action
func (p *Parser) Yyaccept() // throw :racc_jump, 2  — from a Reduce action
func (p *Parser) Yyerrok()  // leave error-recovery mode
```

- **`NextToken`** is the lexer seam used by `DoParse` (MRI's `next_token`). It
  returns `[sym, val]`; a nil `sym` is EOF (MRI's `[false, false]`). An unknown
  non-nil symbol maps to the error token (internal id 1).
- **`Reduce`** is the reduce-action seam (MRI's `__send__(method_id, val, …)`,
  i.e. the generated `_reduce_N`). It receives the rule's `method_id` and the
  popped symbol values and returns the produced nonterminal's value.
- **`OnError`** is the parse-error seam (MRI's `on_error`). It fires the first time
  an error action is taken; returning `nil` enters racc's error-recovery mode,
  returning an error aborts the parse with it. When unset, the engine raises a
  `*ParseError` with MRI's exact message — `parse error on value <inspect> (<tok>)`.

`Yyparse` swaps the pull lexer for an iterator seam: the `iter` closure is called
once and must `yield(sym, val)` per token, mirroring `yyparse(recv, mid)` where
`recv.mid` yields the tokens.

## Usage

```go
p := &racc.Parser{Tables: calcTables} // calcTables transcribes the grammar's Racc_arg
p.NextToken = lexer.Next               // the host lexer
p.Reduce = func(methodID int, val []any, result any) any {
	switch methodID { // the generated _reduce_N bodies
	case 2: // exp: exp '+' exp
		return val[0].(int) + val[2].(int)
	case 5: // exp: NUMBER
		return val[0]
	// …
	}
	return result
}
result, err := p.DoParse()
```

## MRI parse-trajectory parity

The headline guarantee is **same tables ⇒ same parse**. The differential oracle
([`oracle_test.go`](oracle_test.go)) installs the calc grammar's `Racc_arg` into
MRI's *own* `Racc::Parser` (the Ruby core, forced via `Racc_No_Extensions`) and
into this engine, runs the identical token streams through both, and asserts the
reduce trajectory and the final result/error match byte-for-byte — including
operator-grouping decisions driven by racc's conflict resolution and the exact
default error-message text. A second grammar with an `error` rule exercises the
error-recovery path; building it caught a real divergence (MRI renders a character
terminal's `token_to_str` as the double-quoted `"+"`, not `'+'`).

## Tests & coverage

Deterministic, ruby-free tests drive the engine over fixed parse tables (calc and a
statement grammar with error recovery) and hold coverage at **100%** on their own,
so the qemu cross-arch and Windows lanes pass the gate. The MRI oracle runs on the
Linux/macOS lanes where `ruby` (≥ 4.0) is installed and skips itself on Windows /
when ruby is absent.

```sh
COVERPKG=$(go list ./... | paste -sd, -)
go test -race -coverpkg="$COVERPKG" -coverprofile=cover.out ./...
go tool cover -func=cover.out | tail -1   # 100.0%
```

CGO-free, `gofmt` + `go vet` clean, and green across the six 64-bit Go targets
(amd64, arm64, riscv64, loong64, ppc64le, s390x) and three OSes (Linux, macOS,
Windows).

## What rbgo binds

rbgo (go-embedded-ruby) binds the three seams to the user's Ruby parser object: the
generated `next_token` method (the user's lexer) → `NextToken`; the generated
`_reduce_N` methods dispatched by `method_id` → `Reduce`; and the parser's
`on_error` (default or overridden) → `OnError`. The `Racc_arg` constant emitted by
`racc` into the parser becomes a `Tables`, and `do_parse` / `yyparse` map to
`DoParse` / `Yyparse`. That makes racc-generated gem parsers run on the pure-Go,
CGO-free runtime with no Ruby interpreter underneath.

## License

BSD-3-Clause — see [LICENSE](LICENSE). Copyright the go-ruby-racc/racc authors.
