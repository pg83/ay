package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MakeFile is the parsed representation of a ya.make file.
type MakeFile struct {
	Path  string // absolute path of the .ya.make file (or relative if caller passed relative)
	Stmts []Stmt // top-level statements in source order
}

// Stmt is the sealed-interface marker for ya.make statements.
type Stmt interface {
	stmtMarker() // unexported, just for sealed-interface discipline
}

// ModuleStmt represents bare module declarations like PROGRAM(), LIBRARY().
type ModuleStmt struct {
	Name string   // e.g. "PROGRAM", "LIBRARY"
	Args []string // usually empty for our subset
	Line int      // 1-based source line of the macro
}

// PeerdirStmt represents PEERDIR(p1 p2 ...).
type PeerdirStmt struct {
	Paths []string
	Line  int
}

// SrcsStmt represents SRCS(s1 s2 ...).
type SrcsStmt struct {
	Sources []string
	Line    int
}

// SetStmt represents SET(NAME value) or SET(NAME "value").
type SetStmt struct {
	Name  string
	Value string
	Line  int
}

// EndStmt represents END().
type EndStmt struct {
	Line int
}

// UnknownStmt is the catch-all for macros we recognize textually but do not
// model semantically yet. Keeps the parser tolerant.
type UnknownStmt struct {
	Name string
	Args []string
	Line int
}

// IfStmt represents `IF (cond) ... ELSE ... ENDIF` (and ELSEIF as
// nested IfStmt in the Else branch). The body slices are the parsed
// Stmts of each branch in source order; an IF without ELSE has nil/
// empty Else. PR-13 only parses the construct; PR-20 wires the
// evaluator (`macros.go:EvalCond`) into Gen so unreachable branches
// are dropped before rule emission.
type IfStmt struct {
	Cond Expr
	Then []Stmt
	Else []Stmt
	Line int
}

// IncludeStmt is the type for an `INCLUDE(path)` directive. NOTE:
// `Parse`/`ParseFile` NEVER emit this in the resulting `Stmts` slice —
// includes are inline-expanded at parse time so downstream walkers see
// a flat list of top-level Stmts. The type stays defined for symmetry
// with the rest of the M2 ADT and so future PRs (e.g. PR-20 reporting
// "where did this Stmt come from") can re-introduce a marker without
// breaking the public type set. See `expandInclude` in this file for
// the resolution rule (path is relative to the directory of the
// currently-parsed file).
type IncludeStmt struct {
	Path string
	Line int
}

// JoinSrcsStmt represents `JOIN_SRCS(name srcs...)`. OutputName is the
// first arg; Sources keeps the remaining args in declaration order
// (NOT sorted — JOIN_SRCS preserves source order in the generated
// translation unit, which the reference graph relies on for byte-exact
// cmd_args).
type JoinSrcsStmt struct {
	OutputName string
	Sources    []string
	Line       int
}

// AddInclStmt represents `ADDINCL(paths...)` where individual paths
// may be prefixed with GLOBAL. GlobalPaths holds paths that were
// immediately preceded by the GLOBAL keyword and will be propagated
// to consumers via PEERDIR. OwnPaths holds the remaining paths that
// are local to the declaring module only.
//
// In ymake syntax, GLOBAL is a per-path modifier, not a
// statement-level modifier. For example:
//
//	ADDINCL(
//	    GLOBAL contrib/libs/cxxsupp/libcxx/include  # → GlobalPaths
//	    contrib/libs/cxxsupp/libcxx/src             # → OwnPaths
//	)
//
// PR-31 D13: previously this struct carried a statement-level
// Modifier and a flat Paths list; the per-path split was missing,
// causing the bare (non-GLOBAL) path to leak into the GLOBAL set
// and propagate spuriously to consumers.
type AddInclStmt struct {
	GlobalPaths []string
	OwnPaths    []string
	Line        int
}

// CFlagsStmt represents `CFLAGS([GLOBAL] flags...)` with per-path
// GLOBAL semantics (PR-32 D04, mirror of AddInclStmt's PR-31 D13
// shape). A flag token immediately following the literal `GLOBAL`
// keyword goes to GlobalFlags; all other flag tokens go to OwnFlags.
// `CFLAGS(GLOBAL -DBAR)` puts -DBAR in GlobalFlags; `CFLAGS(-DBAZ)`
// puts -DBAZ in OwnFlags. GlobalFlags propagates to consumers via
// PEERDIR; OwnFlags applies only to the declaring module.
type CFlagsStmt struct {
	GlobalFlags []string
	OwnFlags    []string
	Line        int
}

// CXXFlagsStmt represents `CXXFLAGS([GLOBAL] flags...)`. Identical
// per-path GLOBAL semantics to CFlagsStmt; the distinction is the
// language axis: CXXFLAGS apply only to C++ sources (.cpp/.cc/.cxx),
// while CFLAGS apply to both C and C++ sources. PR-32 D05.
type CXXFlagsStmt struct {
	GlobalFlags []string
	OwnFlags    []string
	Line        int
}

// CONLYFlagsStmt represents `CONLYFLAGS([GLOBAL] flags...)`. C-only
// counterpart to CXXFlagsStmt: applies only to C / .S sources.
// PR-32 D06.
type CONLYFlagsStmt struct {
	GlobalFlags []string
	OwnFlags    []string
	Line        int
}

// LDFlagsStmt represents `LDFLAGS(flags...)`. No modifier — LDFLAGS in
// the upstream macro vocabulary is always "global to the linked
// module" and never carries a per-arg GLOBAL-ness toggle.
type LDFlagsStmt struct {
	Flags []string
	Line  int
}

// SrcDirStmt represents `SRCDIR(dir)` — single-arg.
type SrcDirStmt struct {
	Dir  string
	Line int
}

// GlobalSrcsStmt represents `GLOBAL_SRCS(srcs...)`.
type GlobalSrcsStmt struct {
	Sources []string
	Line    int
}

// GenerateEnumSerializationStmt represents a GENERATE_ENUM_SERIALIZATION(*) macro.
// Variant captures which of the three macro forms was used:
//   - "plain"       → GENERATE_ENUM_SERIALIZATION
//   - "with_header" → GENERATE_ENUM_SERIALIZATION_WITH_HEADER
//   - "noutf"       → GENERATE_ENUM_SERIALIZATION_NOUTF
//
// Header is the single argument: the header path relative to the
// module directory (e.g. "stats_enums.h" or "config/config.h").
type GenerateEnumSerializationStmt struct {
	Header  string
	Variant string // "plain" | "with_header" | "noutf"
	Line    int
}

func (*ModuleStmt) stmtMarker()                   {}
func (*PeerdirStmt) stmtMarker()                  {}
func (*SrcsStmt) stmtMarker()                     {}
func (*SetStmt) stmtMarker()                      {}
func (*EndStmt) stmtMarker()                      {}
func (*UnknownStmt) stmtMarker()                  {}
func (*IfStmt) stmtMarker()                       {}
func (*IncludeStmt) stmtMarker()                  {}
func (*JoinSrcsStmt) stmtMarker()                 {}
func (*AddInclStmt) stmtMarker()                  {}
func (*CFlagsStmt) stmtMarker()                   {}
func (*CXXFlagsStmt) stmtMarker()                 {}
func (*CONLYFlagsStmt) stmtMarker()               {}
func (*LDFlagsStmt) stmtMarker()                  {}
func (*SrcDirStmt) stmtMarker()                   {}
func (*GlobalSrcsStmt) stmtMarker()               {}
func (*GenerateEnumSerializationStmt) stmtMarker() {}

// Expr is the sealed-interface marker for IF-predicate AST nodes. The
// evaluator lives in `macros.go:EvalCond`. PR-27 widened the ADT from
// four bool-only constructors (ident/NOT/AND/OR) to eight by adding
// the value-position leaves (string/int literals) plus the two
// comparison operators (`==`, `<`) that the libcxx / libcxxrt /
// libunwind / libc_compat ya.makes use. The grammar stays small —
// only what the closure actually needs.
type Expr interface {
	exprMarker()
}

// ExprIdent is a leaf identifier — typically a bound-var name like
// `OS_LINUX` or `CLANG`. The evaluator throws on unknown idents
// (D27) rather than silently defaulting to false.
type ExprIdent struct {
	Name string
}

// ExprNot is logical negation: `NOT X`.
type ExprNot struct {
	Of Expr
}

// ExprAnd is short-circuiting conjunction: `A AND B`.
type ExprAnd struct {
	Left, Right Expr
}

// ExprOr is short-circuiting disjunction: `A OR B`.
type ExprOr struct {
	Left, Right Expr
}

// ExprString is a string literal — `"libcxxrt"` in
// `IF (CXX_RT == "libcxxrt")`. Only legal as an operand of ExprEq;
// using it as a top-level cond throws at evaluation time (a bare
// string has no boolean meaning).
type ExprString struct {
	Value string
}

// ExprInt is an integer literal — `28` in `IF (ANDROID_API < 28)`.
// Same value-position constraint as ExprString. Integer literals are
// unsigned (digits only); negative integers are unsupported by the
// current lexer — closures requiring `IF (X < -N)` would need a lexer
// extension.
type ExprInt struct {
	Value int
}

// ExprEq is equality comparison: `Left == Right`. Both operands must
// resolve (via env or literal) to the same dynamic type — `string ==
// string` or `int == int`. Mixed types throw at evaluation time.
type ExprEq struct {
	Left, Right Expr
}

// ExprLt is numeric less-than: `Left < Right`. Both operands must
// resolve to int; non-int operands throw.
type ExprLt struct {
	Left, Right Expr
}

func (*ExprIdent) exprMarker()  {}
func (*ExprNot) exprMarker()    {}
func (*ExprAnd) exprMarker()    {}
func (*ExprOr) exprMarker()     {}
func (*ExprString) exprMarker() {}
func (*ExprInt) exprMarker()    {}
func (*ExprEq) exprMarker()     {}
func (*ExprLt) exprMarker()     {}

// ParseError describes a syntactic problem with a ya.make file.
type ParseError struct {
	File    string
	Line    int // 1-based
	Col     int // 1-based
	Message string
}

func (e *ParseError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Message)
	}

	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Message)
}

// ParseFile reads path and parses it as a ya.make file. Returns a typed
// *ParseError when the file is syntactically invalid (callers can
// errors.As on it for line/col reporting). I/O errors from os.ReadFile
// are surfaced as plain errors (the underlying *fs.PathError from the
// stdlib).
//
// ParseFile is a Try boundary: internally it uses Throw2 for the
// os.ReadFile call (no caller discriminates on its error shape beyond
// "did it fail"), then delegates to Parse which is itself a boundary
// for the typed *ParseError contract.
func ParseFile(path string) (mf *MakeFile, err error) {
	exc := Try(func() {
		data := Throw2(os.ReadFile(path))

		abs, absErr := filepath.Abs(path)

		if absErr != nil {
			// Fall back to caller-provided path if Abs fails for any reason.
			abs = path
		}

		mf = Throw2(Parse(abs, data))
	})

	if exc != nil {
		err = exc.AsError()
		mf = nil
	}

	return mf, err
}

// ----------------------------------------------------------------------
// Lexer
// ----------------------------------------------------------------------

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokString // quoted string (without surrounding quotes)
	tokWord   // bare path / identifier-like atom that's not a macro IDENT (e.g. "main.cpp", "library/cpp/archive")
	tokLParen
	tokRParen
	tokInt   // unsigned integer literal (PR-27: IF (ANDROID_API < 28))
	tokEq    // `==` operator (PR-27: IF (CXX_RT == "libcxxrt"))
	tokLt    // `<` operator (PR-27: IF (ANDROID_API < 28))
	tokNotEq // `!=` operator (PR-27: IF (OS_SDK != "ubuntu-20"))
)

type token struct {
	kind tokKind
	val  string // textual value (for ident/string/word)
	line int    // 1-based line of the token's first character
	col  int    // 1-based column of the token's first character
}

type lexer struct {
	name string
	src  []byte
	pos  int
	line int
	col  int
	// prevByte is the most recently consumed source byte, used by skipTrivia
	// to decide whether a '#' is at a trivia boundary (and thus starts a
	// comment) or is mid-word (and is a literal byte). Initialized to 0 to
	// represent "start of file"; treated identically to whitespace for the
	// boundary check.
	prevByte byte
}

func newLexer(name string, src []byte) *lexer {
	return &lexer{
		name:     name,
		src:      src,
		pos:      0,
		line:     1,
		col:      1,
		prevByte: 0,
	}
}

// throwParse raises a *ParseError-bearing exception. Callers that need
// the typed value back (Parse, ParseFile) wrap their entry point in Try
// and use errors.As to recover the *ParseError.
func (l *lexer) throwParse(line, col int, format string, args ...any) {
	pe := &ParseError{
		File:    l.name,
		Line:    line,
		Col:     col,
		Message: fmt.Sprintf(format, args...),
	}

	New(pe).throw()
}

// advance consumes the byte at l.pos and updates line/col. It treats '\r',
// '\n', and the two-byte sequence "\r\n" as a single line terminator that
// bumps line by exactly 1 and resets col to 1. Returns the byte at the
// original l.pos (the leading byte of any CRLF pair).
func (l *lexer) advance() byte {
	b := l.src[l.pos]
	l.pos++

	switch {
	case b == '\n':
		l.line++
		l.col = 1
		l.prevByte = b
	case b == '\r':
		// Lone CR is a newline. CRLF is also one newline: consume the
		// trailing '\n' here without bumping line/col a second time.
		l.line++
		l.col = 1

		if l.pos < len(l.src) && l.src[l.pos] == '\n' {
			l.pos++
			l.prevByte = '\n'
		} else {
			l.prevByte = b
		}
	default:
		l.col++
		l.prevByte = b
	}

	return b
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}

func isIdentStart(b byte) bool {
	return (b >= 'A' && b <= 'Z') || b == '_'
}

func isIdentCont(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// isWordByte tells whether b can appear inside a bare-word arg (path,
// filename, lowercase identifier, version literal, etc.). The set is
// deliberately conservative: alphanumerics plus the punctuation that
// shows up in real ya.make atoms — paths, filenames, version literals,
// flag-style tokens. A character outside this set at the start of a word
// is a lex error (see readToken's default arm), which is exactly what the
// brief asks for ("token that's neither IDENT, paren, string, nor
// whitespace at top level").
//
// Notes on inclusions:
//   - '{' and '}' are kept to support ${VAR} interpolation syntax that
//     appears in real ya.make values; downstream emitters expect to see
//     them as part of the bare word.
//   - '#' is a word byte ONLY when it appears mid-word; skipTrivia (and
//     the comment-boundary check there) handles the "is this a comment?"
//     decision before readWord ever sees a leading '#'. A leading '#' at
//     a trivia boundary is consumed by skipTrivia before the lexer
//     classifies a token.
//
// Notes on exclusions (from the over-permissive earlier set):
//   - Quote-like and shell-metacharacter bytes (' ` ; | & ^ < > [ ]) are
//     dropped because they would silently swallow real syntax errors.
func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	}

	switch b {
	case '_', '-', '.', '/', '+', ':', '=', '*', '?', '$', '%', '~', ',', '!', '{', '}', '#',
		// M3: backslash appears in -DFOO=\"bar\" compiler-flag tokens
		// (e.g. contrib/libs/openssl/crypto/ya.make.inc:112).
		'\\':
		return true
	}

	return false
}

// skipTrivia advances past whitespace and comments. Comments run from '#'
// to end-of-line (the newline itself is whitespace and is also consumed).
//
// A '#' starts a comment only when it appears at a trivia boundary — that
// is, when the byte immediately preceding it is whitespace, the start of
// file, or '('. Anywhere else, '#' is a regular word byte (so e.g. the
// path "a/b#x" remains one word). This avoids the bug where a mid-word
// '#' would silently swallow the rest of the macro call up to EOL,
// including the closing ')'.
func (l *lexer) skipTrivia() {
	for l.pos < len(l.src) {
		b := l.src[l.pos]

		switch {
		case isWhitespace(b):
			l.advance()
		case b == '#' && commentBoundary(l.prevByte):
			// Comment to end-of-line. Stop just before any line
			// terminator so the next iteration's whitespace branch
			// consumes it (and updates line/col uniformly).
			for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.advance()
			}
		default:
			return
		}
	}
}

// commentBoundary reports whether prev (the most recently consumed byte,
// or 0 at start-of-file) is a trivia boundary that allows a following
// '#' to start a comment. Whitespace, SOF, and '(' qualify; anything
// else means the '#' is mid-word and must be treated as a literal.
func commentBoundary(prev byte) bool {
	if prev == 0 {
		return true
	}

	if prev == '(' {
		return true
	}

	return isWhitespace(prev)
}

// next returns the next token. On EOF it returns a tokEOF token. On a
// malformed token (e.g. unterminated string) it throws a
// *ParseError-bearing exception that Parse/ParseFile recover via Try.
func (l *lexer) next() token {
	return l.readToken()
}

func (l *lexer) readToken() token {
	l.skipTrivia()

	if l.pos >= len(l.src) {
		return token{kind: tokEOF, line: l.line, col: l.col}
	}

	startLine, startCol := l.line, l.col
	b := l.src[l.pos]

	switch {
	case b == '(':
		l.advance()

		return token{kind: tokLParen, line: startLine, col: startCol}
	case b == ')':
		l.advance()

		return token{kind: tokRParen, line: startLine, col: startCol}
	case b == '"':
		return l.readString(startLine, startCol)
	case b == '<':
		// `<` is the only single-character relational operator we
		// model. `<=` and `>=` are not in the closure subset; if a
		// future ya.make needs them, extend here and in parseCmp.
		l.advance()

		return token{kind: tokLt, line: startLine, col: startCol}
	case b == '=' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '=':
		// `==` only — bare `=` at a token boundary falls through to
		// readWord (so flag-style values like "-D_X=1" still lex as a
		// single word; bare `=` cannot start a token in any real
		// ya.make we observe).
		l.advance() // consume first '='
		l.advance() // consume second '='

		return token{kind: tokEq, line: startLine, col: startCol}
	case b == '!' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '=':
		// `!=` only — bare `!` at a token boundary falls through to
		// readWord (e.g. CFLAGS values like "!suppressed"; not common
		// in real ya.makes but kept lenient).
		l.advance() // consume '!'
		l.advance() // consume '='

		return token{kind: tokNotEq, line: startLine, col: startCol}
	case b >= '0' && b <= '9':
		return l.readNumberOrWord(startLine, startCol)
	case isIdentStart(b):
		return l.readIdentOrWord(startLine, startCol)
	case isWordByte(b):
		return l.readWord(startLine, startCol)
	default:
		// Consume the offending byte so callers can keep advancing past
		// malformed input if they want to (we always throw below).
		l.advance()
		l.throwParse(startLine, startCol, "unexpected character %q", b)

		// Unreachable: throwParse panics. The bare return keeps the
		// compiler happy without polluting the function's normal return
		// paths.
		return token{}
	}
}

// readString reads a double-quoted string. The opening quote is at l.pos.
// The returned token's val is the inner text (no surrounding quotes).
//
// String body is raw — no escape processing. \X is two literal bytes.
// \" is therefore NOT an escape; the bare " closes the string. (This
// behavior is pinned by TestStringHasNoEscapeProcessing in the test
// suite — do not "improve" it without updating the test.)
//
// A literal newline (LF or CR) inside a string is rejected as
// "unterminated string", pinned at the opening quote's line/col. ya.make
// strings are intentionally single-line; the user-meaningful failure
// when a newline appears mid-string is "you forgot the closing quote",
// not "strings can't span lines."
func (l *lexer) readString(startLine, startCol int) token {
	l.advance() // consume opening "

	var buf []byte

	for {
		if l.pos >= len(l.src) {
			l.throwParse(startLine, startCol, "unterminated string")
		}

		b := l.src[l.pos]

		if b == '"' {
			l.advance() // consume closing "

			return token{kind: tokString, val: string(buf), line: startLine, col: startCol}
		}

		if b == '\n' || b == '\r' {
			l.throwParse(startLine, startCol, "unterminated string")
		}

		if b == '\\' && l.pos+1 < len(l.src) {
			// Treat backslash + next byte as two literal bytes (no escapes).
			// (See TestStringHasNoEscapeProcessing.)
			buf = append(buf, l.advance())
			buf = append(buf, l.advance())

			continue
		}

		buf = append(buf, l.advance())
	}
}

// readIdentOrWord reads a token whose first byte is an identifier-start
// character. If the entire run is identifier-class bytes, it is a tokIdent.
// If it contains anything else (e.g. a dot or slash because the source had
// "Main.cpp" — though our identifiers are upper-case so this is unlikely),
// the whole run is reclassified as a tokWord. This keeps the lexer simple
// and matches how ya.make actually distributes uppercase macro names vs.
// lowercase paths.
func (l *lexer) readIdentOrWord(startLine, startCol int) token {
	var buf []byte
	pureIdent := true

	for l.pos < len(l.src) {
		b := l.src[l.pos]

		if b == '\\' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
			// M3: `\"` inside an ident/word token — consume as two literal
			// bytes (same as readWord). The `"` must not start a string.
			pureIdent = false
			buf = append(buf, l.advance())
			buf = append(buf, l.advance())

			continue
		}

		if isIdentCont(b) {
			buf = append(buf, l.advance())

			continue
		}

		if isWordByte(b) {
			pureIdent = false
			buf = append(buf, l.advance())

			continue
		}

		break
	}

	val := string(buf)
	kind := tokIdent

	if !pureIdent {
		kind = tokWord
	}

	return token{kind: kind, val: val, line: startLine, col: startCol}
}

// readWord reads a bare-word token (path, lowercase identifier, etc.).
func (l *lexer) readWord(startLine, startCol int) token {
	var buf []byte

	for l.pos < len(l.src) {
		b := l.src[l.pos]

		if b == '\\' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
			// M3: `\"` inside a bare-word token (e.g. -DFOO=\"bar\")
			// is consumed as two literal bytes so the whole compiler flag
			// remains one token. The embedded `"` must not start a string.
			buf = append(buf, l.advance())
			buf = append(buf, l.advance())

			continue
		}

		if !isWordByte(b) {
			break
		}

		buf = append(buf, l.advance())
	}

	return token{kind: tokWord, val: string(buf), line: startLine, col: startCol}
}

// readNumberOrWord reads a token that begins with a digit. A pure
// digit run that ends at a non-word boundary becomes a tokInt
// (`28` → tokInt(28)). Anything else — digits followed by another
// word byte, or a non-digit punctuation continuation — degrades to
// a tokWord so version literals like `2025-06-20` keep their existing
// shape and the rest of the parser sees them as bare words. The
// non-degraded path is the one PR-27 added support for; the
// degraded path is the pre-PR-27 behaviour preserved verbatim.
func (l *lexer) readNumberOrWord(startLine, startCol int) token {
	start := l.pos

	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.advance()
	}

	if l.pos < len(l.src) && isWordByte(l.src[l.pos]) {
		// Continuation byte is a non-digit word byte — the whole run
		// is a tokWord (e.g. version literal "2025-06-20" or a
		// path component starting with a digit).
		for l.pos < len(l.src) && isWordByte(l.src[l.pos]) {
			l.advance()
		}

		return token{kind: tokWord, val: string(l.src[start:l.pos]), line: startLine, col: startCol}
	}

	return token{kind: tokInt, val: string(l.src[start:l.pos]), line: startLine, col: startCol}
}

// ----------------------------------------------------------------------
// Parser
// ----------------------------------------------------------------------

// Parse parses src as a ya.make file. name is used in error messages.
// Returns a typed *ParseError on syntactic problems (callers can
// errors.As on it). Internally the lexer/parser raise via throw; Parse
// wraps the entry point in Try and converts the recovered exception
// back into the typed error so the public contract stays
// (T, error)-shaped — domain signal that drives a branch (CLI error
// formatting, future macro evaluator) lives at this boundary.
func Parse(name string, src []byte) (mf *MakeFile, err error) {
	exc := Try(func() {
		mf = parseInternal(name, src)
	})

	if exc != nil {
		// Unwrap the exception's underlying error. For lexer/parser
		// errors that's a *ParseError; we surface it verbatim so
		// callers' errors.As(err, &pe) keeps working.
		err = exc.AsError()
		mf = nil
	}

	return mf, err
}

func parseInternal(name string, src []byte) *MakeFile {
	return parseInternalWithStack(name, src, nil)
}

func parseInternalWithStack(name string, src []byte, stack []string) *MakeFile {
	p := &parser{lex: newLexer(name, src), name: name, includeStack: stack}
	mf := &MakeFile{Path: name}
	mf.Stmts, _ = p.parseStmts(termTopLevel)

	return mf
}

// stmtTerminator names the boundary that ends a Stmt sequence. The
// caller of `parseStmts` passes one of these to say "I want to read
// Stmts until you hit X". `termTopLevel` is the file-level terminator
// (only `tokEOF` ends the sequence). The `termIfBody*` set is used by
// `parseIf` to stop at `ELSE`, `ELSEIF`, or `ENDIF`.
type stmtTerminator int

const (
	termTopLevel stmtTerminator = iota
	termIfBody
)

// parseStmts collects Stmts until it sees the terminator (EOF for
// termTopLevel, or one of ELSE/ELSEIF/ENDIF for termIfBody). For
// termIfBody it returns the terminator macro's name token via
// `endTok` so `parseIf` can decide whether the next thing is an Else
// branch, an ElseIf chain, or the ENDIF closer. INCLUDE is
// transparently expanded inline (the IncludeStmt itself is dropped
// from the result).
func (p *parser) parseStmts(term stmtTerminator) (stmts []Stmt, endTok token) {
	for {
		tok := p.lex.next()

		if tok.kind == tokEOF {
			if term != termTopLevel {
				p.lex.throwParse(tok.line, tok.col, "unexpected end of file inside IF block (missing ENDIF)")
			}

			return stmts, tok
		}

		if tok.kind != tokIdent && !(tok.kind == tokWord && isIdentShapedName(tok.val)) {
			p.lex.throwParse(tok.line, tok.col, "expected macro name, got %s", describeToken(tok))
		}

		// Inside an IF body, the keywords ELSE/ELSEIF/ENDIF are not
		// macros — they are block boundaries. Detect them BEFORE we
		// consume the `(...)` so parseIf can parse ELSEIF's condition
		// arguments itself.
		if term == termIfBody && (tok.val == "ELSE" || tok.val == "ELSEIF" || tok.val == "ENDIF") {
			return stmts, tok
		}

		stmts = p.parseMacroInto(stmts, tok)
	}
}

// parseMacroInto consumes `(args...)` for the macro whose name token
// is `nameTok` and appends the resulting Stmts to `into`. Most macros
// produce exactly one Stmt; INCLUDE expands inline (zero-or-more
// Stmts from the included file) and IF reads its own block bodies via
// parseStmts(termIfBody) so the caller's loop sees the IF as one Stmt.
func (p *parser) parseMacroInto(into []Stmt, nameTok token) []Stmt {
	switch nameTok.val {
	case "IF":
		return append(into, p.parseIf(nameTok))
	case "INCLUDE":
		return p.expandInclude(into, nameTok)
	}

	args := p.parseMacroArgs(nameTok)
	stmt := p.buildStmt(nameTok, args)

	return append(into, stmt)
}

// parseMacroArgs reads `( args... )`. The leading `(` and the trailing
// `)` are both consumed; the returned slice contains the bare-string
// args (idents, words, strings) in source order.
func (p *parser) parseMacroArgs(nameTok token) []string {
	lp := p.lex.next()

	if lp.kind != tokLParen {
		p.lex.throwParse(lp.line, lp.col, "expected '(' after macro name %q, got %s", nameTok.val, describeToken(lp))
	}

	var args []string

	for {
		tok := p.lex.next()

		switch tok.kind {
		case tokRParen:
			return args
		case tokEOF:
			p.lex.throwParse(nameTok.line, nameTok.col, "unterminated macro call %q (missing ')')", nameTok.val)
		case tokIdent, tokWord, tokString, tokInt:
			args = append(args, tok.val)
		case tokLParen:
			p.lex.throwParse(tok.line, tok.col, "unexpected '(' inside macro call %q", nameTok.val)
		default:
			p.lex.throwParse(tok.line, tok.col, "unexpected %s inside macro call %q", describeToken(tok), nameTok.val)
		}
	}
}

// isIdentShapedName reports whether s could plausibly be a macro
// identifier — non-empty, starting with letter or '_', containing only
// letters, digits, and '_'. Used to decide whether a tokWord at top
// level should be accepted as an UnknownStmt macro name.
func isIdentShapedName(s string) bool {
	if s == "" {
		return false
	}

	for i := 0; i < len(s); i++ {
		b := s[i]
		isLetter := (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
		isDigit := b >= '0' && b <= '9'

		if i == 0 {
			if !isLetter && b != '_' {
				return false
			}

			continue
		}

		if !isLetter && !isDigit && b != '_' {
			return false
		}
	}

	return true
}

type parser struct {
	lex          *lexer
	name         string
	includeStack []string // absolute paths of files being parsed, outermost first; used for cycle detection
}

// buildStmt routes recognized macro names to typed Stmt; everything else
// becomes UnknownStmt. IF/INCLUDE are handled out-of-band by
// parseMacroInto and never reach this function.
func (p *parser) buildStmt(nameTok token, args []string) Stmt {
	switch nameTok.val {
	case "PROGRAM", "LIBRARY",
		// M3 multimodule types: parsed as ModuleStmt with canonical name so
		// genModule can route them. PR-M3-A treats them as LIBRARY-shaped stubs
		// (header-only when they have no compilable C/C++ sources, emitted as
		// a normal LIBRARY when they do). PR-M3-B..E introduce real emitters.
		"PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
		"PY3_PROGRAM_BIN", "PY2_PROGRAM", "PY3_PROGRAM",
		"PROTO_LIBRARY",
		"DLL", "SO_PROGRAM",
		"PACKAGE", "UNION", "RESOURCES_LIBRARY":
		return &ModuleStmt{Name: nameTok.val, Args: args, Line: nameTok.line}
	case "PEERDIR":
		return &PeerdirStmt{Paths: args, Line: nameTok.line}
	case "SRCS":
		return &SrcsStmt{Sources: args, Line: nameTok.line}
	case "SET":
		if len(args) != 2 {
			p.lex.throwParse(nameTok.line, nameTok.col, "SET expects exactly 2 arguments (name and value), got %d", len(args))
		}

		return &SetStmt{Name: args[0], Value: args[1], Line: nameTok.line}
	case "END":
		return &EndStmt{Line: nameTok.line}
	case "JOIN_SRCS":
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "JOIN_SRCS expects at least one argument (the output name)")
		}

		// Defensive copy: the caller's args slice is reused across
		// branches; a sub-slice into it would alias.
		sources := append([]string(nil), args[1:]...)

		return &JoinSrcsStmt{OutputName: args[0], Sources: sources, Line: nameTok.line}
	case "ADDINCL":
		globalPaths, ownPaths := splitAddInclPaths(args)

		return &AddInclStmt{GlobalPaths: globalPaths, OwnPaths: ownPaths, Line: nameTok.line}
	case "CFLAGS":
		globalFlags, ownFlags := splitFlagsByGlobal(args)

		return &CFlagsStmt{GlobalFlags: globalFlags, OwnFlags: ownFlags, Line: nameTok.line}
	case "CXXFLAGS":
		globalFlags, ownFlags := splitFlagsByGlobal(args)

		return &CXXFlagsStmt{GlobalFlags: globalFlags, OwnFlags: ownFlags, Line: nameTok.line}
	case "CONLYFLAGS":
		globalFlags, ownFlags := splitFlagsByGlobal(args)

		return &CONLYFlagsStmt{GlobalFlags: globalFlags, OwnFlags: ownFlags, Line: nameTok.line}
	case "LDFLAGS":
		return &LDFlagsStmt{Flags: append([]string(nil), args...), Line: nameTok.line}
	case "SRCDIR":
		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "SRCDIR expects exactly 1 argument, got %d", len(args))
		}

		return &SrcDirStmt{Dir: args[0], Line: nameTok.line}
	case "GLOBAL_SRCS":
		return &GlobalSrcsStmt{Sources: append([]string(nil), args...), Line: nameTok.line}
	case "GENERATE_ENUM_SERIALIZATION":
		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "GENERATE_ENUM_SERIALIZATION expects exactly 1 argument (header path), got %d", len(args))
		}

		return &GenerateEnumSerializationStmt{Header: args[0], Variant: "plain", Line: nameTok.line}
	case "GENERATE_ENUM_SERIALIZATION_WITH_HEADER":
		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "GENERATE_ENUM_SERIALIZATION_WITH_HEADER expects exactly 1 argument (header path), got %d", len(args))
		}

		return &GenerateEnumSerializationStmt{Header: args[0], Variant: "with_header", Line: nameTok.line}
	case "GENERATE_ENUM_SERIALIZATION_NOUTF":
		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "GENERATE_ENUM_SERIALIZATION_NOUTF expects exactly 1 argument (header path), got %d", len(args))
		}

		return &GenerateEnumSerializationStmt{Header: args[0], Variant: "noutf", Line: nameTok.line}
	default:
		return &UnknownStmt{Name: nameTok.val, Args: args, Line: nameTok.line}
	}
}

// splitGlobalModifier extracts a leading "GLOBAL" pseudo-arg from an
// arg list (used by CFLAGS). Returns ("GLOBAL", rest) when the first
// arg is exactly "GLOBAL"; otherwise ("", args) — the uppercase match
// is deliberate, mirroring the upstream macro syntax where a lowercase
// "global" is a regular token, not a modifier.
func splitGlobalModifier(args []string) (string, []string) {
	if len(args) > 0 && args[0] == "GLOBAL" {
		return "GLOBAL", append([]string(nil), args[1:]...)
	}

	return "", append([]string(nil), args...)
}

// splitFlagsByGlobal separates CFLAGS / CXXFLAGS / CONLYFLAGS args
// into a global and own slice using per-path GLOBAL semantics
// (PR-32 D04). A flag token immediately following the literal
// `GLOBAL` keyword goes to globalFlags; all other tokens go to
// ownFlags. Mirrors `splitAddInclPaths`. Empirical M2 closure
// verifies all GLOBAL CFLAGS / CXXFLAGS callsites take the
// single-arg `(GLOBAL -DX)` shape, so the per-path vs statement-
// level distinction has no observable difference today; the
// per-path shape future-proofs against later mixed-token call
// sites.
func splitFlagsByGlobal(args []string) (globalFlags, ownFlags []string) {
	for i := 0; i < len(args); i++ {
		if args[i] == "GLOBAL" {
			i++

			if i < len(args) {
				globalFlags = append(globalFlags, args[i])
			}
		} else {
			ownFlags = append(ownFlags, args[i])
		}
	}

	return globalFlags, ownFlags
}

// splitAddInclPaths separates ADDINCL args into global and own path
// lists. In ymake syntax, GLOBAL is a per-path modifier: a path token
// immediately following the literal "GLOBAL" is added to the
// propagated (global) set; all other path tokens go to the module-own
// set. For example, given:
//
//	ADDINCL(
//	    GLOBAL contrib/libs/cxxsupp/libcxx/include
//	    contrib/libs/cxxsupp/libcxx/src
//	)
//
// args = ["GLOBAL", "contrib/libs/cxxsupp/libcxx/include",
// "contrib/libs/cxxsupp/libcxx/src"] and the return is
// (["contrib/libs/cxxsupp/libcxx/include"],
// ["contrib/libs/cxxsupp/libcxx/src"]).
//
// PR-31 D13: replaces the old statement-level splitGlobalModifier for
// ADDINCL, which incorrectly treated all paths after a leading GLOBAL
// as global, leaking module-own paths to consumers.
func splitAddInclPaths(args []string) (globalPaths, ownPaths []string) {
	for i := 0; i < len(args); i++ {
		if args[i] == "GLOBAL" {
			i++

			if i < len(args) {
				globalPaths = append(globalPaths, args[i])
			}
		} else {
			ownPaths = append(ownPaths, args[i])
		}
	}

	return globalPaths, ownPaths
}

// parseIf is invoked with the `IF` name token already consumed. It
// reads the condition args inside `(...)`, parses them into an Expr,
// then collects the THEN body until ELSE/ELSEIF/ENDIF, recursing into
// nested IfStmts for ELSEIF and reading the ELSE body until ENDIF.
//
// The semantics of ELSEIF are exactly "an IF inside the parent's
// Else", so the nested IfStmt holds the elseif's condition and body;
// chained ELSEIFs become right-leaning nested IfStmts, just like the
// `else if` chain in C.
func (p *parser) parseIf(ifTok token) *IfStmt {
	condToks := p.readCondTokens(ifTok)

	if len(condToks) == 0 {
		p.lex.throwParse(ifTok.line, ifTok.col, "IF requires a condition expression")
	}

	cond := parseCondExpr(p, ifTok, condToks)

	thenBody, endTok := p.parseStmts(termIfBody)
	node := &IfStmt{Cond: cond, Then: thenBody, Line: ifTok.line}

	switch endTok.val {
	case "ENDIF":
		p.consumeEmptyMacroArgs(endTok)

		return node
	case "ELSE":
		p.consumeEmptyMacroArgs(endTok)

		elseBody, endIf := p.parseStmts(termIfBody)

		if endIf.val != "ENDIF" {
			p.lex.throwParse(endIf.line, endIf.col, "expected ENDIF after ELSE block, got %s", endIf.val)
		}

		p.consumeEmptyMacroArgs(endIf)
		node.Else = elseBody

		return node
	case "ELSEIF":
		// ELSEIF (cond) ... = an IF nested in the parent's Else.
		// Recurse via parseIf — that handler reads the nested
		// condition args, the nested THEN body, and any further
		// ELSE/ELSEIF/ENDIF chain.
		nested := p.parseIf(endTok)
		node.Else = []Stmt{nested}

		return node
	}

	// Unreachable: parseStmts(termIfBody) only returns one of the
	// three terminator names above. A defensive throw makes the
	// invariant explicit.
	p.lex.throwParse(endTok.line, endTok.col, "internal: unexpected IF terminator %q", endTok.val)

	return nil
}

// readCondTokens reads the IF's `(...)` args, allowing arbitrary
// inner-paren grouping (the cond grammar supports it for precedence
// override). Returns the token sequence WITHOUT the outer `(`/`)`
// pair; inner parens are preserved as tokLParen/tokRParen so
// parseCondExpr can use them.
func (p *parser) readCondTokens(ifTok token) []token {
	lp := p.lex.next()

	if lp.kind != tokLParen {
		p.lex.throwParse(lp.line, lp.col, "expected '(' after IF, got %s", describeToken(lp))
	}

	var (
		out   []token
		depth = 1
	)

	for {
		tok := p.lex.next()

		switch tok.kind {
		case tokEOF:
			p.lex.throwParse(ifTok.line, ifTok.col, "unterminated IF condition (missing ')')")
		case tokLParen:
			depth++
			out = append(out, tok)
		case tokRParen:
			depth--

			if depth == 0 {
				return out
			}

			out = append(out, tok)
		case tokIdent, tokWord, tokString, tokInt, tokEq, tokLt, tokNotEq:
			out = append(out, tok)
		}
	}
}

// consumeEmptyMacroArgs reads `()` after one of the IF block keywords
// (ELSE/ELSEIF/ENDIF). ELSE/ENDIF accept only the empty arg list;
// ELSEIF's condition is parsed by `parseIf` recursively. The caller
// passes the keyword token in for line/col error reporting.
func (p *parser) consumeEmptyMacroArgs(kwTok token) {
	args := p.parseMacroArgs(kwTok)

	if len(args) != 0 {
		p.lex.throwParse(kwTok.line, kwTok.col, "%s does not take arguments, got %d", kwTok.val, len(args))
	}
}

// condParser is the cursor state for the IF-cond recursive-descent
// parser below. It walks the token slice produced by readCondTokens.
type condParser struct {
	toks   []token
	pos    int
	parent *parser // for throwParse line/col reporting on the lexer
	ifTok  token   // the IF keyword's token, used as fallback location
}

// parseCondExpr parses the IF's condition tokens into an Expr ADT.
// Precedence (lowest → highest): OR, AND, NOT, comparator (`==` /
// `<`), atom. Comparators bind tighter than NOT so `NOT X == Y`
// parses as `NOT (X == Y)`, which matches the libcxxrt usage
// `IF (SANITIZER_TYPE == undefined OR FUZZING)` (the OR's RHS is
// just FUZZING, the LHS is the whole comparison). Comparators are
// non-associative: `A == B == C` is a syntax error, not a chain.
// Parentheses override precedence. AND/OR are left-associative;
// NOT is right-associative. Throws *ParseError on syntactic
// problems.
func parseCondExpr(parent *parser, ifTok token, toks []token) Expr {
	cp := &condParser{toks: toks, parent: parent, ifTok: ifTok}
	expr := cp.parseOr()

	if cp.pos != len(cp.toks) {
		t := cp.toks[cp.pos]
		parent.lex.throwParse(t.line, t.col, "unexpected %s in IF condition", describeToken(t))
	}

	return expr
}

func (c *condParser) peek() (token, bool) {
	if c.pos >= len(c.toks) {
		return token{}, false
	}

	return c.toks[c.pos], true
}

func (c *condParser) consume() token {
	t := c.toks[c.pos]
	c.pos++

	return t
}

func (c *condParser) parseOr() Expr {
	left := c.parseAnd()

	for {
		t, ok := c.peek()

		if !ok || !(t.kind == tokIdent && t.val == "OR") {
			return left
		}

		c.consume()
		right := c.parseAnd()
		left = &ExprOr{Left: left, Right: right}
	}
}

func (c *condParser) parseAnd() Expr {
	left := c.parseNot()

	for {
		t, ok := c.peek()

		if !ok || !(t.kind == tokIdent && t.val == "AND") {
			return left
		}

		c.consume()
		right := c.parseNot()
		left = &ExprAnd{Left: left, Right: right}
	}
}

func (c *condParser) parseNot() Expr {
	t, ok := c.peek()

	if ok && t.kind == tokIdent && t.val == "NOT" {
		c.consume()

		return &ExprNot{Of: c.parseNot()}
	}

	return c.parseCmp()
}

// parseCmp recognises a single comparator `X op Y` between two atoms,
// where op is `==` or `<`. Non-associative: a second comparator after
// the first throws (so `A == B == C` is a clear syntax error rather
// than silently associating left or right). When no comparator
// follows the leading atom, parseCmp returns the atom as-is.
func (c *condParser) parseCmp() Expr {
	left := c.parseAtom()

	t, ok := c.peek()

	if !ok {
		return left
	}

	switch t.kind {
	case tokEq:
		c.consume()
		right := c.parseAtom()
		c.rejectChainedCmp(t)

		return &ExprEq{Left: left, Right: right}
	case tokLt:
		c.consume()
		right := c.parseAtom()
		c.rejectChainedCmp(t)

		return &ExprLt{Left: left, Right: right}
	case tokNotEq:
		// `X != Y` desugars to `NOT (X == Y)` so the evaluator only
		// needs the ExprEq path; the negation is structural and the
		// short-circuit semantics are unaffected.
		c.consume()
		right := c.parseAtom()
		c.rejectChainedCmp(t)

		return &ExprNot{Of: &ExprEq{Left: left, Right: right}}
	}

	return left
}

// rejectChainedCmp throws when the token directly after a comparator's
// RHS is itself another comparator — `A == B == C` and similar. Pinned
// at the chain operator's location so the user sees exactly which
// `==`/`<` was the second one.
func (c *condParser) rejectChainedCmp(prev token) {
	t, ok := c.peek()

	if !ok {
		return
	}

	if t.kind == tokEq || t.kind == tokLt || t.kind == tokNotEq {
		c.parent.lex.throwParse(t.line, t.col, "chained comparison %s after %s is not supported", describeToken(t), describeToken(prev))
	}
}

func (c *condParser) parseAtom() Expr {
	t, ok := c.peek()

	if !ok {
		c.parent.lex.throwParse(c.ifTok.line, c.ifTok.col, "unexpected end of IF condition")
	}

	if t.kind == tokLParen {
		c.consume()
		expr := c.parseOr()
		closer, hasCloser := c.peek()

		if !hasCloser || closer.kind != tokRParen {
			c.parent.lex.throwParse(t.line, t.col, "missing ')' in IF condition")
		}

		c.consume()

		return expr
	}

	if t.kind == tokString {
		c.consume()

		return &ExprString{Value: t.val}
	}

	if t.kind == tokInt {
		c.consume()

		// Lexer guarantees t.val is non-empty and digits-only;
		// strconv-style parsing inline would pull in a stdlib import
		// for one call site. The hand-rolled parse keeps the
		// parser's import set tight.
		n := 0
		for i := 0; i < len(t.val); i++ {
			n = n*10 + int(t.val[i]-'0')
		}

		return &ExprInt{Value: n}
	}

	if t.kind == tokIdent || (t.kind == tokWord && isIdentShapedName(t.val)) {
		// AND/OR/NOT as bare atoms are an error — they're operators.
		// A user typing `IF (AND)` should get a clear diagnostic, not
		// silently bind an identifier called AND.
		if t.val == "AND" || t.val == "OR" || t.val == "NOT" {
			c.parent.lex.throwParse(t.line, t.col, "operator %q used as identifier in IF condition", t.val)
		}

		c.consume()

		return &ExprIdent{Name: t.val}
	}

	c.parent.lex.throwParse(t.line, t.col, "unexpected %s in IF condition", describeToken(t))

	return nil // unreachable
}

// expandInclude parses `INCLUDE(path)` and inlines the included
// file's top-level Stmts into `into`. The IncludeStmt type stays
// defined for symmetry with the rest of the M2 ADT, but
// Parse/ParseFile NEVER emit it — downstream walkers see a flat list.
//
// Path resolution: relative to `filepath.Dir(p.name)`. When the
// caller used `Parse(name, src)` with a non-path label (e.g.
// `"test.input"`), `filepath.Dir` returns `.`, so an INCLUDE in such
// a stream resolves against the process CWD. The include test path
// uses `t.TempDir()` so this surfaces as a real file lookup, which
// is the documented contract.
//
// Cycle detection: expandInclude maintains p.includeStack, a slice of
// absolute paths forming the current include chain from the outermost
// file to the immediately-enclosing one. Before recursing, it checks
// whether the target path already appears in the stack; if so, it
// throws a *ParseError pinned at the INCLUDE site with message
// "INCLUDE cycle: <chain> -> <target>". The stack is propagated to
// the child parser so cycles spanning more than one hop are also
// caught.
func (p *parser) expandInclude(into []Stmt, nameTok token) []Stmt {
	args := p.parseMacroArgs(nameTok)

	if len(args) != 1 {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE expects exactly 1 argument (the path), got %d", len(args))
	}

	rel := args[0]

	// Resolve ${ARCADIA_ROOT}/... INCLUDE paths by replacing the variable
	// with the source-root directory (the parent of the top-level ya.make's
	// directory chain). For any ya.make whose absolute path is
	// <sourceRoot>/some/path/ya.make the source root is found by walking
	// up until the path begins with the known prefix. We approximate it
	// as filepath.Dir(p.name) for N levels up until a canonical marker
	// file exists — but a simpler heuristic is to strip exactly as many
	// components as the module path has, using the fact that p.name is
	// always <sourceRoot>/<modulePath>/ya.make. For M3 purposes we just
	// resolve ${ARCADIA_ROOT} by stripping "/ya.make" suffix and enough
	// parent directories to reach the source root. The portable approach:
	// the source root equals the path that, when joined with the module's
	// relative include path, produces a file that exists on disk. Simplest
	// safe implementation: replace ${ARCADIA_ROOT} with the root computed
	// from p.name (three levels: strip <file>, <dir>, … until a
	// recognizable marker is found). For the M3 closure, all
	// ARCADIA_ROOT-rooted INCLUDEs live at paths that are child-of the
	// same directory as p.name minus the module subpath. We derive
	// sourceRoot by detecting the ${ARCADIA_ROOT} prefix and computing
	// the root via the parser's stored name.
	if strings.HasPrefix(rel, "${ARCADIA_ROOT}/") {
		// Derive sourceRoot from p.name: p.name = <sourceRoot>/<modulePath>/ya.make.
		// Walk up from p.name's directory until we find the root that, when
		// joined with the remainder of rel, names an existing file. As a
		// conservative heuristic, walk up from dir(p.name) a bounded number
		// of steps and try each.
		suffix := strings.TrimPrefix(rel, "${ARCADIA_ROOT}/")
		dir := filepath.Dir(p.name)

		for i := 0; i < 20; i++ {
			candidate := filepath.Join(dir, suffix)

			if _, err := os.Stat(candidate); err == nil {
				rel = candidate

				break
			}

			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}

			dir = parent
		}
		// If no candidate was found, rel still has ${ARCADIA_ROOT}/ prefix
		// and the os.ReadFile below will produce a clear parse error.
	}

	dir := filepath.Dir(p.name)
	full := rel

	if !filepath.IsAbs(rel) {
		full = filepath.Join(dir, rel)
	}

	// Normalise to an absolute path for reliable cycle detection
	// across symlinks and "." components.
	absTarget, absErr := filepath.Abs(full)
	if absErr != nil {
		absTarget = full
	}

	// Build the full chain: the current parser's own file plus its
	// inherited stack. p.name is the absolute path of the file being
	// parsed right now (set by parseInternalWithStack); it is the last
	// element of the chain leading into this INCLUDE site.
	chain := append(p.includeStack, p.name)

	// Check for a cycle: does absTarget already appear anywhere in chain?
	for _, visited := range chain {
		if visited == absTarget {
			// Format the cycle chain for the error message.
			chainStr := ""
			for i, v := range chain {
				if i > 0 {
					chainStr += " -> "
				}
				chainStr += v
			}
			chainStr += " -> " + absTarget
			p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE cycle: %s", chainStr)
		}
	}

	data, ioErr := os.ReadFile(absTarget)
	if ioErr != nil {
		// Silently skip optional includes that do not exist on disk.
		// This handles conditional branches such as
		// `IF (USE_SYSTEM_OPENSSL) INCLUDE(system_openssl.ya.inc)`
		// where the included file is absent in the open-source tree.
		// Because our parser expands INCLUDE in both branches of an IF
		// before we evaluate the condition, a missing file in a dead
		// branch must not be a fatal error.
		if os.IsNotExist(ioErr) {
			return into
		}

		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE %q: %v", rel, ioErr)
	}

	// Recurse with the updated chain propagated into the child parser so
	// transitive cycles (a→b→a) are also caught.
	included := parseInternalWithStack(absTarget, data, chain)

	return append(into, included.Stmts...)
}

func describeToken(t token) string {
	switch t.kind {
	case tokEOF:
		return "end of file"
	case tokIdent:
		return fmt.Sprintf("identifier %q", t.val)
	case tokString:
		return fmt.Sprintf("string %q", t.val)
	case tokWord:
		return fmt.Sprintf("word %q", t.val)
	case tokLParen:
		return "'('"
	case tokRParen:
		return "')'"
	case tokInt:
		return fmt.Sprintf("integer %s", t.val)
	case tokEq:
		return "'=='"
	case tokLt:
		return "'<'"
	case tokNotEq:
		return "'!='"
	default:
		return fmt.Sprintf("token(kind=%d)", t.kind)
	}
}
