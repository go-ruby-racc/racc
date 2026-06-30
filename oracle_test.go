package racc

import (
	"bytes"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// This file is the differential-vs-MRI oracle. It drives MRI's *own* Racc::Parser
// runtime (the Ruby core, forced via Racc_No_Extensions) over the EXACT SAME parse
// tables this package's engine uses (the calc grammar's Racc_arg), and asserts that
// the Go engine reproduces MRI's parse trajectory (the shift/reduce sequence) and
// final result byte-for-byte. "Same tables -> same parse" is the load-bearing
// guarantee that lets racc-generated gem parsers run unchanged on this runtime.
//
// The oracle requires a real `ruby` (>= 4.0) and is skipped on Windows / when ruby
// is absent or older, exactly as the org's Windows-CI convention prescribes. The
// deterministic in-package tests already hold coverage at 100% without ruby.

// raccRubyArg is the Ruby literal for the calc grammar's Racc_arg, identical to the
// table transcribed in calcTables(). The two are kept in lock-step on purpose: the
// oracle feeds this to MRI and the Go engine consumes calcTables().
const raccRubyArg = `[
  [6, 7, 3, 12, 4, 3, 3, 4, 4, 3, 5, 4, 6, 7, 6, 7, 6, 7, 9],
  [8, 8, 0, 8, 0, 3, 6, 3, 6, 7, 1, 7, 2, 2, 10, 10, 11, 11, 5],
  [-6, -6, -1, -6, -5, -6, -6, -6, -6, 13, -2, -3, -4],
  [-2, 10, 10, 1, nil, 18, 2, 5, -2, nil, 12, 14, nil],
  [2, 1, nil, 8, nil, nil, 10, 11],
  [2, 1, nil, 2, nil, nil, 2, 2],
  [nil, nil, nil],
  [nil, 1, 0],
  7,
  [0, 0, :racc_error, 1, 8, :_reduce_1, 3, 9, :_reduce_2, 3, 9, :_reduce_3, 3, 9, :_reduce_4, 1, 9, :_reduce_5],
  {false => 0, :error => 1, "+" => 2, "*" => 3, "(" => 4, ")" => 5, :NUMBER => 6},
  13,
  6,
  true ]`

// rubyDriver builds a self-contained Ruby program that includes MRI's Racc::Parser,
// installs the calc Racc_arg above, parses the given token stream, and prints one
// line per reduction ("R <rule>") followed by "RESULT <int>" or "ERROR <message>".
// The token stream is encoded as Ruby array literal text.
func rubyDriver(tokenLiteral string) string {
	return `
module Racc; Racc_No_Extensions = true; end
require 'racc/parser.rb'
class CalcOracle < Racc::Parser
  Racc_arg = ` + raccRubyArg + `
  Racc_token_to_s_table = ["$end","error","\"+\"","\"*\"","\"(\"","\")\"","NUMBER","$start","target","exp"]
  Racc_debug_parser = false
  def initialize(toks); super(); @toks = toks.dup; @out = []; end
  def out; @out; end
  def next_token; @toks.shift; end
  def _reduce_1(val,_v,r); @out << "R 1"; val[0]; end
  def _reduce_2(val,_v,r); @out << "R 2"; val[0]+val[2]; end
  def _reduce_3(val,_v,r); @out << "R 3"; val[0]*val[2]; end
  def _reduce_4(val,_v,r); @out << "R 4"; val[1]; end
  def _reduce_5(val,_v,r); @out << "R 5"; val[0]; end
end
p = CalcOracle.new(` + tokenLiteral + `)
begin
  res = p.do_parse
  puts p.out
  puts "RESULT #{res}"
rescue Racc::ParseError => e
  puts p.out
  puts "ERROR #{e.message}"
end
`
}

// rubyAvailable reports whether a usable ruby (>= 4.0) is on PATH.
func rubyAvailable(t *testing.T) bool {
	t.Helper()
	if runtime.GOOS == "windows" {
		return false
	}
	out, err := exec.Command("ruby", "-e", "print RUBY_VERSION").Output()
	if err != nil {
		return false
	}
	ver := string(out)
	major := ver
	if dot := strings.IndexByte(ver, '.'); dot >= 0 {
		major = ver[:dot]
	}
	n, err := strconv.Atoi(major)
	if err != nil || n < 4 {
		return false
	}
	return true
}

// runOracle runs the Ruby driver for a token stream and returns its trajectory
// lines (reduces) and the final RESULT/ERROR line.
func runOracle(t *testing.T, tokenLiteral string) (reduces []int, result string) {
	t.Helper()
	cmd := exec.Command("ruby", "-e", rubyDriver(tokenLiteral))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ruby oracle failed: %v\nstderr: %s", err, stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "R "):
			n, _ := strconv.Atoi(strings.TrimPrefix(line, "R "))
			reduces = append(reduces, n)
		case strings.HasPrefix(line, "RESULT "), strings.HasPrefix(line, "ERROR "):
			result = line
		}
	}
	return reduces, result
}

// goRun drives this package's engine over the same stream and returns the same
// shape (reduce rule sequence + a RESULT/ERROR line) as the oracle.
func goRun(toks [][2]any) (reduces []int, result string) {
	q := append([][2]any(nil), toks...)
	i := 0
	p := &Parser{Tables: calcTables()}
	p.NextToken = func() (any, any) { tk := q[i]; i++; return tk[0], tk[1] }
	p.Reduce = func(ruleIdx int, values []any, res any) any {
		reduces = append(reduces, ruleIdx)
		return calcReduce(ruleIdx, values, res)
	}
	got, err := p.DoParse()
	if err != nil {
		if pe, ok := err.(*ParseError); ok {
			return reduces, "ERROR " + pe.Msg
		}
		return reduces, "ERROR " + err.Error()
	}
	return reduces, "RESULT " + intToStr(got)
}

func intToStr(v any) string {
	if n, ok := v.(int); ok {
		return strconv.Itoa(n)
	}
	return ""
}

// oracleCase pairs a Go token stream with the Ruby literal encoding the same stream.
type oracleCase struct {
	name     string
	toks     [][2]any
	rubyToks string
}

func oracleCases() []oracleCase {
	return []oracleCase{
		{
			name:     "1+2*3",
			toks:     [][2]any{num(1), op("+"), num(2), op("*"), num(3), eofTok},
			rubyToks: `[[:NUMBER,1],["+","+"],[:NUMBER,2],["*","*"],[:NUMBER,3],[false,false]]`,
		},
		{
			name:     "paren_1plus2_times3",
			toks:     [][2]any{op("("), num(1), op("+"), num(2), op(")"), op("*"), num(3), eofTok},
			rubyToks: `[["(","("],[:NUMBER,1],["+","+"],[:NUMBER,2],[")",")"],["*","*"],[:NUMBER,3],[false,false]]`,
		},
		{
			name:     "single_number",
			toks:     [][2]any{num(42), eofTok},
			rubyToks: `[[:NUMBER,42],[false,false]]`,
		},
		{
			name:     "error_double_plus",
			toks:     [][2]any{num(1), op("+"), op("+"), num(2), eofTok},
			rubyToks: `[[:NUMBER,1],["+","+"],["+","+"],[:NUMBER,2],[false,false]]`,
		},
		{
			name:     "right_grouping_2times5plus1",
			toks:     [][2]any{num(2), op("*"), num(5), op("+"), num(1), eofTok},
			rubyToks: `[[:NUMBER,2],["*","*"],[:NUMBER,5],["+","+"],[:NUMBER,1],[false,false]]`,
		},
	}
}

// TestMRIOracleParity is the headline proof: for each token stream, MRI's Ruby Racc
// runtime and this Go engine, driven by the identical calc Racc_arg, must produce
// the same reduce trajectory and the same final result/error.
func TestMRIOracleParity(t *testing.T) {
	if !rubyAvailable(t) {
		t.Skip("ruby >= 4.0 not available; deterministic tests cover the engine")
	}
	for _, tc := range oracleCases() {
		t.Run(tc.name, func(t *testing.T) {
			wantReduces, wantResult := runOracle(t, tc.rubyToks)
			gotReduces, gotResult := goRun(tc.toks)
			if !reflect.DeepEqual(gotReduces, wantReduces) {
				t.Fatalf("reduce trajectory: go=%v mri=%v", gotReduces, wantReduces)
			}
			if gotResult != wantResult {
				t.Fatalf("result: go=%q mri=%q", gotResult, wantResult)
			}
		})
	}
}
