package racc

import (
	"fmt"
	"strconv"
	"strings"
)

// rubyInspect renders a token value the way Ruby's Object#inspect would for the
// value kinds that appear in a racc default parse-error message ("parse error on
// value <val.inspect> (...)"). Only the common cases a lexer produces are handled
// precisely (string, symbol, integer, float, nil, true/false); anything else falls
// back to Go's %v, which is sufficient because custom OnError handlers bypass this
// path entirely. This exists solely to byte-match MRI's default message text.
func rubyInspect(v any) string {
	switch x := v.(type) {
	case nil:
		return "nil"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return rubyInspectString(x)
	case Symbol:
		return ":" + string(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		// Ruby prints e.g. 1.0 where Go's %v prints "1"; append ".0" when the
		// shortest representation has no decimal point or exponent. (Ruby and Go
		// can still differ on very large magnitudes, e.g. 1e10 vs 1e+10; float
		// tokens are rare and this only affects the default error-message text.)
		s := strconv.FormatFloat(x, 'g', -1, 64)
		if !strings.ContainsAny(s, ".eEnN") { // no decimal point / exponent / Inf/NaN
			s += ".0"
		}
		return s
	default:
		return fmt.Sprintf("%v", v)
	}
}

// rubyInspectString reproduces Ruby's String#inspect for the ASCII-printable cases
// (double-quoted, backslash-escaping the quote and backslash). It is intentionally
// minimal — token strings in grammars are short literals like "+" or "(".
func rubyInspectString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Symbol is the Go representation of a Ruby Symbol token (e.g. :NUMBER). A racc
// token table keys terminals by their grammar symbol; for named tokens MRI uses a
// Ruby Symbol and for character literals a String. Use Symbol so the two are
// distinct map keys in Tables.TokenTable, matching MRI where :foo and "foo" differ.
type Symbol string
