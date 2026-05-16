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

// SetStmt represents SET(NAME value...) or SET(NAME "value").
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

// IfStmt represents `IF (cond) ... ELSE ... ENDIF` (ELSEIF nests as
// IfStmt in the Else branch). Body slices are parsed Stmts in source
// order; IF without ELSE has nil Else. The evaluator
// (`macros.go:EvalCond`) drops unreachable branches in Gen.
type IfStmt struct {
	Cond Expr
	Then []Stmt
	Else []Stmt
	Line int
}

// IncludeStmt is the type for `INCLUDE(path)`. NOTE: Parse / ParseFile
// NEVER emit this in the resulting Stmts slice — includes are inline-
// expanded at parse time so downstream walkers see a flat top-level
// Stmts list. The type stays defined for symmetry and so future PRs
// can re-introduce a marker. See `expandInclude` for the resolution
// rule (path relative to the currently-parsed file's directory).
type IncludeStmt struct {
	Path string
	Line int
}

// JoinSrcsStmt represents `JOIN_SRCS(name srcs...)`. OutputName is the
// first arg; Sources keeps the remaining args in declaration order
// (NOT sorted — JOIN_SRCS preserves source order in the generated
// translation unit; reference cmd_args are byte-exact-sensitive).
type JoinSrcsStmt struct {
	OutputName string
	Sources    []string
	Line       int
}

// AddInclStmt represents `ADDINCL(paths...)` with per-path GLOBAL
// modifier. GLOBAL paths propagate to consumers via PEERDIR; OwnPaths
// apply only to the declaring module.
//
//	ADDINCL(
//	    GLOBAL contrib/libs/cxxsupp/libcxx/include  # → GlobalPaths
//	    contrib/libs/cxxsupp/libcxx/src             # → OwnPaths
//	)
type AddInclStmt struct {
	GlobalPaths []string
	OwnPaths    []string
	// AllPaths preserves declaration order across the GLOBAL split.
	// own cmd_args emission uses AllPaths so -I slots match the
	// reference's declaration-order layout (e.g. libffi
	// ADDINCL(libffi libffi/include libffi/src GLOBAL libffi/include)
	// emits as [libffi, libffi/include, libffi/src], not Global-first).
	AllPaths []string
	Line     int
}

// CFlagsStmt represents `CFLAGS([GLOBAL] flags...)` with per-flag
// GLOBAL modifier. A flag immediately after `GLOBAL` goes to
// GlobalFlags; others go to OwnFlags. GlobalFlags propagates to
// consumers via PEERDIR; OwnFlags applies only to the declaring
// module.
type CFlagsStmt struct {
	GlobalFlags []string
	OwnFlags    []string
	Line        int
}

// CXXFlagsStmt represents `CXXFLAGS([GLOBAL] flags...)`. Same
// per-flag GLOBAL semantics as CFlagsStmt; applies only to C++
// sources (.cpp/.cc/.cxx) — CFLAGS applies to both C and C++.
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

// DefaultVarStmt represents `DEFAULT(NAME value)`. Records a
// configuration-variable default used by CONFIGURE_FILE's $CFG_VARS
// expansion. The value is the second argument; quotes are stripped by
// the parser so the stored value is the bare string (e.g.
// `DEFAULT(KOSHER_SVN_VERSION "")` → Value="").
type DefaultVarStmt struct {
	VarName string
	Value   string
	Line    int
}

// RunProgramStmt represents `RUN_PROGRAM(tool args... [IN ...]
// [OUT ...] [OUT_NOAUTO ...] [STDOUT file] [ENV k=v...] [CWD dir]
// [OUTPUT_INCLUDES ...])`. ToolPath is the PROGRAM's module-relative
// path; Args are positional args before the first keyword. STDOUT (if
// any) goes to outputs[0] of the generated node.
type RunProgramStmt struct {
	ToolPath       string
	Args           []string
	INFiles        []string
	OUTFiles       []string
	OUTNoAutoFiles []string
	StdoutFile     string   // empty when STDOUT not specified
	EnvPairs       []string // "KEY=VALUE" strings from ENV
	CWD            string
	OutputIncludes []string
	Line           int
}

// ConfigureFileStmt represents `CONFIGURE_FILE(src dst)`. Src is
// SOURCE_ROOT-relative path to the .in template; Dst is BUILD_ROOT-
// relative output. CONFIGURE_FILE is also the implicit handler for
// .cpp.in/.c.in in SRCS — the parser synthesises a ConfigureFileStmt
// with Src=<srcRel>, Dst=<srcRel without .in>.
type ConfigureFileStmt struct {
	Src  string // SOURCE_ROOT-relative
	Dst  string // BUILD_ROOT-relative (module-dir-relative)
	Line int
}

// CreateBuildInfoStmt represents `CREATE_BUILDINFO_FOR(output_header)`.
// OutputHeader is the name of the generated header (e.g.
// "buildinfo_data.h").
type CreateBuildInfoStmt struct {
	OutputHeader string
	Line         int
}

// RunAntlr4CppStmt represents `RUN_ANTLR4_CPP(grammar [options...])`.
// Grammar is the single .g4 file.  Options is the remaining arg list
// (may include -package NConfReader, etc.).  Visitor/NoListener are
// parsed from VISITOR/NO_LISTENER keywords in Options.
// OutputIncludes captures repo-relative headers from the OUTPUT_INCLUDES
// keyword section — propagated into the CP `.g4.cpp` registry entry so
// the CC consumer's include closure inherits their transitive set
// (PR-M3-jv-antlr-system-headers, mirroring PB/EV F-7-D).
type RunAntlr4CppStmt struct {
	Grammar        string   // e.g. "TConf.g4"
	Options        []string // extra args (e.g. ["-package", "NConfReader"])
	Visitor        bool
	Listener       bool
	OutputIncludes []string // repo-relative (e.g. "util/generic/string.h")
	Line           int
}

// RunAntlr4CppSplitStmt represents `RUN_ANTLR4_CPP_SPLIT(lexer parser
// [VISITOR] [LISTENER] [OUTPUT_INCLUDES ...])`. Lexer and Parser are
// the two .g4 grammar files; the generated outputs cover both.
type RunAntlr4CppSplitStmt struct {
	Lexer          string // e.g. "CmdLexer.g4"
	Parser         string // e.g. "CmdParser.g4"
	Visitor        bool
	Listener       bool
	OutputIncludes []string // repo-relative (PR-M3-jv-antlr-system-headers)
	Line           int
}

// ResourcePair holds one (path, key) pair from a RESOURCE / RESOURCE_FILES
// macro. The Path is the source-file path (module-relative) or "-" for a
// kv-only entry; in the latter case Key carries the raw "name=value"
// string fed to --kvs by the upstream packer.
type ResourcePair struct {
	Path string
	Key  string
}

// ResourceStmt represents `RESOURCE([DONT_PARSE] [DONT_COMPRESS]
// path1 key1 ...)`. Pairs preserves declaration order; DONT_PARSE /
// DONT_COMPRESS are dropped (hash + cmd_args do not depend on them).
type ResourceStmt struct {
	Pairs []ResourcePair
	Line  int
}

// ResourceFilesStmt represents `RESOURCE_FILES([DONT_COMPRESS]
// [PREFIX p] [DEST d] [STRIP s] path1 ...)`. Args is the raw token
// list; expansion into RESOURCE pairs (mirroring upstream
// `build/plugins/res.py:onresource_files`) happens in
// gen.go:collectStmts so downstream walkers see a uniform pair list.
type ResourceFilesStmt struct {
	Args []string
	Line int
}

func (*ModuleStmt) stmtMarker()                    {}
func (*PeerdirStmt) stmtMarker()                   {}
func (*SrcsStmt) stmtMarker()                      {}
func (*SetStmt) stmtMarker()                       {}
func (*EndStmt) stmtMarker()                       {}
func (*UnknownStmt) stmtMarker()                   {}
func (*IfStmt) stmtMarker()                        {}
func (*IncludeStmt) stmtMarker()                   {}
func (*JoinSrcsStmt) stmtMarker()                  {}
func (*AddInclStmt) stmtMarker()                   {}
func (*CFlagsStmt) stmtMarker()                    {}
func (*CXXFlagsStmt) stmtMarker()                  {}
func (*CONLYFlagsStmt) stmtMarker()                {}
func (*LDFlagsStmt) stmtMarker()                   {}
func (*SrcDirStmt) stmtMarker()                    {}
func (*GlobalSrcsStmt) stmtMarker()                {}
func (*GenerateEnumSerializationStmt) stmtMarker() {}
func (*DefaultVarStmt) stmtMarker()                {}
func (*RunProgramStmt) stmtMarker()                {}
func (*ConfigureFileStmt) stmtMarker()             {}
func (*CreateBuildInfoStmt) stmtMarker()           {}
func (*RunAntlr4CppStmt) stmtMarker()              {}
func (*RunAntlr4CppSplitStmt) stmtMarker()         {}
func (*ResourceStmt) stmtMarker()                  {}
func (*ResourceFilesStmt) stmtMarker()             {}

// Expr is the sealed-interface marker for IF-predicate AST nodes.
// Evaluator: macros.go:EvalCond. Constructors: ident, NOT, AND, OR,
// string/int literals, and the `==`, `!=`, `<` comparison operators.
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
// Unsigned (digits only); negative integers unsupported by the
// current lexer.
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

// ParseFile reads path and parses it as a ya.make. Returns a typed
// *ParseError for syntax errors (errors.As-able for line/col); I/O
// errors surface as plain *fs.PathError. Try boundary: Throw2 on
// ReadFile, then delegates to Parse.
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
	// prevByte: most recently consumed source byte. skipTrivia uses
	// it to decide whether a '#' starts a comment (boundary) or is a
	// word byte. 0 = start of file, treated like whitespace.
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

// isWordByte tells whether b can appear inside a bare-word arg. The
// set is deliberately conservative: alphanumerics plus punctuation
// appearing in real ya.make atoms (paths, filenames, version
// literals, flag tokens).
//
// Inclusions worth noting: '{', '}' for ${VAR} interpolation; '#' is
// allowed mid-word because skipTrivia's comment-boundary check
// handles leading '#' before classification.
//
// Quote-like and shell-metacharacter bytes (' ` ; | & ^ < > [ ]) are
// excluded — they would silently swallow real syntax errors.
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

// skipTrivia advances past whitespace and comments. A '#' starts a
// comment only at a trivia boundary (after whitespace, SOF, or '(');
// anywhere else '#' is a word byte — so paths like "a/b#x" remain one
// word. Without this discrimination a mid-word '#' would silently
// swallow the rest of the macro call up to EOL.
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

// readString reads a double-quoted string (val excludes the quotes).
// Body is RAW — no escape processing. \X is two literal bytes; \" is
// NOT an escape (pinned by TestStringHasNoEscapeProcessing). A literal
// newline inside a string is rejected as "unterminated string" at the
// opening-quote's line/col — ya.make strings are single-line.
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

// readIdentOrWord reads a token whose first byte is identifier-start.
// Pure identifier-class bytes → tokIdent; any non-ident byte in the
// run reclassifies the whole token as tokWord. Matches ya.make's
// uppercase-macros vs lowercase-paths distribution.
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

		if isWordByte(b) || (b == '@' && len(buf) > 0) {
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

		if !isWordByte(b) && !(b == '@' && len(buf) > 0) {
			break
		}

		buf = append(buf, l.advance())
	}

	return token{kind: tokWord, val: string(buf), line: startLine, col: startCol}
}

// readNumberOrWord reads a token starting with a digit. Pure digit
// run ending at a non-word boundary → tokInt; digits followed by
// another word byte → tokWord (e.g. version literal "2025-06-20").
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

// Parse parses src as a ya.make file. Returns a typed *ParseError on
// syntactic problems (errors.As-able). Lexer/parser raise via throw;
// Parse wraps the entry point in Try and converts back to typed
// error so the public contract stays (T, error)-shaped.
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
	return parseInternalWithState(name, src, stack, newIncludeState())
}

func parseInternalWithState(name string, src []byte, stack []string, includes *includeState) *MakeFile {
	p := &parser{lex: newLexer(name, src), name: name, includeStack: stack, includes: includes}
	mf := &MakeFile{Path: name}
	mf.Stmts, _ = p.parseStmts(termTopLevel)

	return mf
}

type includeState struct {
	once map[string]struct{}
}

func newIncludeState() *includeState {
	return &includeState{once: map[string]struct{}{}}
}

// stmtTerminator names the boundary ending a Stmt sequence.
// termTopLevel ends only at tokEOF; termIfBody (used by parseIf)
// stops at ELSE, ELSEIF, or ENDIF.
type stmtTerminator int

const (
	termTopLevel stmtTerminator = iota
	termIfBody
)

// parseStmts collects Stmts until the terminator. For termIfBody it
// returns the terminator macro's name token via endTok so parseIf can
// decide between Else / ElseIf / ENDIF. INCLUDE expands inline (the
// IncludeStmt itself is dropped from the result).
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

// parseMacroInto consumes `(args...)` for nameTok and appends the
// resulting Stmts to into. Most macros produce one Stmt; INCLUDE
// expands inline (zero-or-more); IF reads its own block bodies via
// parseStmts(termIfBody) so the caller's loop sees the IF as one Stmt.
func (p *parser) parseMacroInto(into []Stmt, nameTok token) []Stmt {
	switch nameTok.val {
	case "IF":
		return append(into, p.parseIf(nameTok))
	case "INCLUDE":
		return p.expandInclude(into, nameTok)
	case "INCLUDE_ONCE":
		p.applyIncludeOnce(nameTok)

		return into
	}

	args := p.parseMacroArgs(nameTok)
	stmt := p.buildStmt(nameTok, args)

	return append(into, stmt)
}

func (p *parser) applyIncludeOnce(nameTok token) {
	args := p.parseMacroArgs(nameTok)
	enabled := true

	if len(args) > 1 {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE_ONCE expects 0 or 1 arguments, got %d", len(args))
	}
	if len(args) == 1 && args[0] == "no" {
		enabled = false
	}

	if !enabled {
		return
	}

	abs, err := filepath.Abs(p.name)
	if err != nil {
		abs = p.name
	}

	p.includes.once[abs] = struct{}{}
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
	includes     *includeState
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
		if len(args) < 2 {
			p.lex.throwParse(nameTok.line, nameTok.col, "SET expects at least 2 arguments (name and value), got %d", len(args))
		}

		return &SetStmt{Name: args[0], Value: strings.Join(args[1:], " "), Line: nameTok.line}
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
		globalPaths, ownPaths, allPaths := splitAddInclPaths(args)

		return &AddInclStmt{GlobalPaths: globalPaths, OwnPaths: ownPaths, AllPaths: allPaths, Line: nameTok.line}
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
	case "DEFAULT":
		// DEFAULT(NAME value) — configuration variable default.
		// Accepts 1 or 2 args: name and optional value (empty string when
		// the quoted value "" was stripped by the lexer).
		varName := ""
		value := ""
		if len(args) >= 1 {
			varName = args[0]
		}
		if len(args) >= 2 {
			value = args[1]
		}
		return &DefaultVarStmt{VarName: varName, Value: value, Line: nameTok.line}
	case "CONFIGURE_FILE":
		// CONFIGURE_FILE(src dst) — src is SOURCE_ROOT-relative path to
		// the .in template; dst is the output name (BUILD_ROOT-relative to
		// the module dir, no .in suffix).
		if len(args) != 2 {
			p.lex.throwParse(nameTok.line, nameTok.col, "CONFIGURE_FILE expects exactly 2 arguments (src dst), got %d", len(args))
		}
		return &ConfigureFileStmt{Src: args[0], Dst: args[1], Line: nameTok.line}
	case "CREATE_BUILDINFO_FOR":
		// CREATE_BUILDINFO_FOR(output_header) — emits a BI node.
		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "CREATE_BUILDINFO_FOR expects exactly 1 argument, got %d", len(args))
		}
		return &CreateBuildInfoStmt{OutputHeader: args[0], Line: nameTok.line}
	case "RUN_ANTLR4_CPP":
		// RUN_ANTLR4_CPP(grammar [-package pkg] [VISITOR] [NO_LISTENER] ...)
		// The first arg is the .g4 grammar file; subsequent args are
		// options (including VISITOR keyword and -package pairs).
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "RUN_ANTLR4_CPP expects at least 1 argument (grammar)")
		}
		return parseRunAntlr4Cpp(args, nameTok.line)
	case "RUN_ANTLR4_CPP_SPLIT":
		// RUN_ANTLR4_CPP_SPLIT(lexer parser [VISITOR] [LISTENER] ...)
		if len(args) < 2 {
			p.lex.throwParse(nameTok.line, nameTok.col, "RUN_ANTLR4_CPP_SPLIT expects at least 2 arguments (lexer parser)")
		}
		return parseRunAntlr4CppSplit(args, nameTok.line)
	case "RUN_PROGRAM":
		// RUN_PROGRAM(tool args... [IN files...]
		//             [OUT files...] [OUT_NOAUTO files...] [STDOUT file] [ENV key=val...]
		//             [CWD dir] [OUTPUT_INCLUDES files...])
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "RUN_PROGRAM expects at least 1 argument (tool path)")
		}
		return parseRunProgram(args, nameTok.line)
	case "RESOURCE":
		// RESOURCE([DONT_PARSE] [DONT_COMPRESS] path1 key1 ...)
		// Mirrors upstream `devtools/ymake/plugins/resource_handler/impl.cpp:23-26`
		// Strips leading DONT_PARSE / DONT_COMPRESS, then pairs the
		// remaining args as (path, key). Odd-count residual = parse
		// error.
		return parseResource(args, nameTok)
	case "RESOURCE_FILES":
		// RESOURCE_FILES([DONT_COMPRESS] [PREFIX p] [DEST d] [STRIP s]
		//                path1 path2 ...)
		// The plugin-side expansion into RESOURCE pairs (per `build/plugins/res.py`)
		// happens at collect time in gen.go; here we just capture the raw
		// arg list verbatim.
		return &ResourceFilesStmt{Args: append([]string(nil), args...), Line: nameTok.line}
	default:
		return &UnknownStmt{Name: nameTok.val, Args: args, Line: nameTok.line}
	}
}

// parseRunAntlr4Cpp parses RUN_ANTLR4_CPP args into a RunAntlr4CppStmt.
// The grammar is args[0]; subsequent args may include VISITOR, NO_LISTENER,
// OUTPUT_INCLUDES (and the files following it), and other option tokens
// (e.g. -package, NConfReader) that are passed to antlr4 cmd_args.
func parseRunAntlr4Cpp(args []string, line int) *RunAntlr4CppStmt {
	// PR-M3-antlr-listener-default: default `-no-listener`, matching upstream
	// `RUN_ANTLR4_CPP` (ymake.core.conf:5270 — `LISTENER?"GRAMMAR":"_ANTLR4_EMPTY"`
	// with `_ANTLR4_LISTENER__ANTLR4_EMPTY=-no-listener` at line 5252).
	stmt := &RunAntlr4CppStmt{Grammar: args[0], Line: line}
	i := 1
	for i < len(args) {
		switch args[i] {
		case "VISITOR":
			stmt.Visitor = true
			i++
		case "NO_LISTENER", "LISTENER":
			// LISTENER is the positive form; NO_LISTENER suppresses it.
			// RUN_ANTLR4_CPP default: -no-listener unless LISTENER given.
			// The reference uses -visitor -no-listener, so track explicitly.
			if args[i] == "NO_LISTENER" {
				stmt.Listener = false
			} else {
				stmt.Listener = true
			}
			i++
		case "OUTPUT_INCLUDES":
			// PR-M3-jv-antlr-system-headers: capture the OUTPUT_INCLUDES
			// section (repo-relative header paths). Threaded into the CP
			// `.g4.cpp` registry entry so CC scan walks them transitively.
			i++
			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
				i++
			}
		case "IN", "OUT", "OUT_NOAUTO", "INDUCED_DEPS", "TOOL":
			// Skip keyword and its following arguments (until next keyword or end).
			i++
			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				i++
			}
		default:
			stmt.Options = append(stmt.Options, args[i])
			i++
		}
	}
	return stmt
}

// parseRunAntlr4CppSplit parses RUN_ANTLR4_CPP_SPLIT args.
// Args[0]=lexer .g4, args[1]=parser .g4; remaining: VISITOR/LISTENER keywords
// and OUTPUT_INCLUDES section. PR-M3-jv-antlr-system-headers: OUTPUT_INCLUDES
// tokens are captured for CP registry use; other keyword sections are skipped.
func parseRunAntlr4CppSplit(args []string, line int) *RunAntlr4CppSplitStmt {
	stmt := &RunAntlr4CppSplitStmt{Lexer: args[0], Parser: args[1], Line: line}
	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "VISITOR":
			stmt.Visitor = true
		case "LISTENER":
			stmt.Listener = true
		case "NO_LISTENER":
			// no-op: default is already no-listener
		case "OUTPUT_INCLUDES":
			// PR-M3-jv-antlr-system-headers: capture repo-relative headers.
			i++
			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
				i++
			}
			i-- // outer loop will i++
		case "IN", "OUT", "OUT_NOAUTO", "INDUCED_DEPS", "TOOL":
			// skip keyword block
			i++
			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				i++
			}
			i-- // outer loop will i++
		}
	}
	return stmt
}

// isRunAntlrKeyword reports whether s is a keyword that terminates a
// positional-arg section in RUN_ANTLR4_CPP / RUN_ANTLR4_CPP_SPLIT.
func isRunAntlrKeyword(s string) bool {
	switch s {
	case "VISITOR", "LISTENER", "NO_LISTENER", "OUTPUT_INCLUDES",
		"IN", "OUT", "OUT_NOAUTO", "INDUCED_DEPS", "TOOL":
		return true
	}
	return false
}

// runProgramKeywords is the set of keyword tokens in RUN_PROGRAM that
// introduce a named argument list. When the parser encounters one of
// these, subsequent tokens up to the next keyword (or end) belong to
// that section rather than to the positional Args list.
var runProgramKeywords = map[string]bool{
	"IN":              true,
	"IN_NOPARSE":      true,
	"OUT":             true,
	"OUT_NOAUTO":      true,
	"STDOUT":          true,
	"STDOUT_NOAUTO":   true,
	"CWD":             true,
	"ENV":             true,
	"OUTPUT_INCLUDES": true,
	"INDUCED_DEPS":    true,
	"IN_DEPS":         true,
	"TOOL":            true,
}

// parseRunProgram parses RUN_PROGRAM args into a RunProgramStmt.
// args[0] is the tool path (module-relative); remaining args are a mix
// of positional args and keyword-headed sections (IN, OUT, OUT_NOAUTO,
// STDOUT, ENV, CWD, OUTPUT_INCLUDES, INDUCED_DEPS, TOOL).
func parseRunProgram(args []string, line int) *RunProgramStmt {
	stmt := &RunProgramStmt{ToolPath: args[0], Line: line}
	i := 1
	currentSection := "ARGS"
	for i < len(args) {
		tok := args[i]
		if runProgramKeywords[tok] {
			currentSection = tok
			i++
			continue
		}
		switch currentSection {
		case "ARGS":
			stmt.Args = append(stmt.Args, tok)
		case "IN", "IN_NOPARSE", "IN_DEPS":
			stmt.INFiles = append(stmt.INFiles, tok)
		case "OUT":
			stmt.OUTFiles = append(stmt.OUTFiles, tok)
		case "OUT_NOAUTO":
			stmt.OUTNoAutoFiles = append(stmt.OUTNoAutoFiles, tok)
		case "STDOUT", "STDOUT_NOAUTO":
			if stmt.StdoutFile == "" {
				stmt.StdoutFile = tok
			}
		case "ENV":
			stmt.EnvPairs = append(stmt.EnvPairs, tok)
		case "CWD":
			if stmt.CWD == "" {
				stmt.CWD = tok
			}
		case "OUTPUT_INCLUDES", "INDUCED_DEPS", "TOOL":
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		}
		i++
	}
	return stmt
}

// parseResource parses RESOURCE(...) by stripping leading DONT_PARSE /
// DONT_COMPRESS (per upstream
// `devtools/ymake/plugins/resource_handler/impl.cpp:23-26`) and pairing
// the remaining args. Odd-count residual = parse error.
func parseResource(args []string, nameTok token) *ResourceStmt {
	rest := args
	// Strip up to two leading modifier tokens (DONT_PARSE, DONT_COMPRESS).
	// Upstream caps the strip at two; we mirror that bound so a stray
	// third keyword would round-trip as a (malformed) path token.
	for i := 0; i < 2 && len(rest) > 0; i++ {
		if rest[0] == "DONT_PARSE" || rest[0] == "DONT_COMPRESS" {
			rest = rest[1:]

			continue
		}

		break
	}

	if len(rest)%2 != 0 {
		p := &parser{lex: &lexer{}}
		_ = p
		ThrowFmt("RESOURCE at line %d: argument count after DONT_PARSE/DONT_COMPRESS strip must be even (got %d)", nameTok.line, len(rest))
	}

	pairs := make([]ResourcePair, 0, len(rest)/2)
	for i := 0; i < len(rest); i += 2 {
		pairs = append(pairs, ResourcePair{Path: rest[i], Key: rest[i+1]})
	}

	return &ResourceStmt{Pairs: pairs, Line: nameTok.line}
}

// splitGlobalModifier extracts a leading "GLOBAL" pseudo-arg.
// Returns ("GLOBAL", rest) when args[0] is exactly "GLOBAL";
// otherwise ("", args). Uppercase match deliberate: lowercase
// "global" is a regular token, not a modifier.
func splitGlobalModifier(args []string) (string, []string) {
	if len(args) > 0 && args[0] == "GLOBAL" {
		return "GLOBAL", append([]string(nil), args[1:]...)
	}

	return "", append([]string(nil), args...)
}

// unescapeFlag converts backslash-quoted double-quotes (`\"`) to
// bare double-quotes (`"`). ya.make source writes -DFOO=\"bar\" but
// the reference stores the unescaped -DFOO="bar". Applied at the
// flag-split boundary so downstream emitters see the unescaped form.
func unescapeFlag(s string) string {
	return strings.ReplaceAll(s, `\"`, `"`)
}

// splitFlagsByGlobal separates CFLAGS / CXXFLAGS / CONLYFLAGS args
// into global and own slices using per-flag GLOBAL semantics.
// Mirrors splitAddInclPaths. Each flag passes through unescapeFlag
// so `\"…\"` becomes `"…"`, matching the reference cmd_args
// encoding.
func splitFlagsByGlobal(args []string) (globalFlags, ownFlags []string) {
	for i := 0; i < len(args); i++ {
		if args[i] == "GLOBAL" {
			i++

			if i < len(args) {
				globalFlags = append(globalFlags, unescapeFlag(args[i]))
			}
		} else {
			ownFlags = append(ownFlags, unescapeFlag(args[i]))
		}
	}

	return globalFlags, ownFlags
}

// splitAddInclPaths separates ADDINCL args into global and own path
// lists using per-path GLOBAL semantics: a path immediately after
// "GLOBAL" is global; all others are own.
//
//	ADDINCL(GLOBAL a b)  →  global=[a], own=[b]
//
// FOR <kind> qualifiers are dropped unconditionally — they select a
// non-C/C++ language axis irrelevant for CC/AS include paths.
func splitAddInclPaths(args []string) (globalPaths, ownPaths, allPaths []string) {
	for i := 0; i < len(args); i++ {
		if args[i] == "FOR" {
			// FOR <kind> — drop both tokens; not a C/C++ include path.
			i++ // skip kind
			continue
		}

		if args[i] == "GLOBAL" {
			i++

			// GLOBAL FOR <kind> <path>: skip the FOR <kind> pair.
			if i < len(args) && args[i] == "FOR" {
				i++ // skip FOR
				i++ // skip kind
			}

			if i < len(args) {
				globalPaths = append(globalPaths, args[i])
				allPaths = append(allPaths, args[i])
			}
		} else {
			ownPaths = append(ownPaths, args[i])
			allPaths = append(allPaths, args[i])
		}
	}

	return globalPaths, ownPaths, allPaths
}

// parseIf is invoked with the `IF` name token already consumed. Reads
// `(...)` args into an Expr, collects THEN body until ELSE/ELSEIF/
// ENDIF, recurses for ELSEIF, reads ELSE body until ENDIF. ELSEIF =
// "an IF inside the parent's Else"; chained ELSEIFs become
// right-leaning nested IfStmts (like C's `else if`).
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

// readCondTokens reads IF's `(...)` args, allowing arbitrary
// inner-paren grouping. Returns tokens WITHOUT the outer `(`/`)`
// pair; inner parens stay as tokLParen/tokRParen for parseCondExpr.
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

// parseCondExpr parses IF condition tokens into an Expr ADT.
// Precedence (lowest → highest): OR, AND, NOT, comparator (`==`, `!=`,
// `<`), atom. Comparators bind tighter than NOT so `NOT X == Y`
// parses as `NOT (X == Y)`. Comparators are non-associative:
// `A == B == C` is a syntax error. AND/OR left-associative; NOT
// right-associative.
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

// parseCmp recognises a single `X op Y` (op = `==`, `!=`, `<`).
// Non-associative: `A == B == C` throws. When no comparator follows
// the leading atom, parseCmp returns it as-is.
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
// file's top-level Stmts. Parse/ParseFile NEVER emit IncludeStmt —
// downstream walkers see a flat list.
//
// Path resolution: relative to filepath.Dir(p.name). With a non-path
// label, Dir returns `.` (resolves against process CWD).
//
// Cycle detection via p.includeStack (absolute paths, outermost
// first). Throws a *ParseError pinned at the INCLUDE site if the
// target already appears in the stack. Stack propagates to the child
// parser so transitive cycles (a→b→a) are also caught.
func (p *parser) expandInclude(into []Stmt, nameTok token) []Stmt {
	args := p.parseMacroArgs(nameTok)

	if len(args) != 1 {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE expects exactly 1 argument (the path), got %d", len(args))
	}

	rel := args[0]

	// Resolve ${ARCADIA_ROOT}/... by walking up from dir(p.name) a
	// bounded number of steps and testing each candidate join against
	// the filesystem. The first existing candidate wins. If nothing
	// resolves the original rel is left intact and ReadFile below
	// surfaces the error.
	if strings.HasPrefix(rel, "${ARCADIA_ROOT}/") {
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
	if _, skip := p.includes.once[absTarget]; skip {
		return into
	}

	// Chain = inherited stack + p.name (the file we're parsing right
	// now, set by parseInternalWithStack); p.name is the chain's last
	// element leading into this INCLUDE site.
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
		// Silently skip optional includes (file missing). Handles
		// conditional branches like
		// `IF (USE_SYSTEM_OPENSSL) INCLUDE(system_openssl.ya.inc)` —
		// the parser expands INCLUDE in both branches before
		// evaluating the condition, so missing files in dead branches
		// must not be fatal.
		if os.IsNotExist(ioErr) {
			return into
		}

		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE %q: %v", rel, ioErr)
	}

	// Recurse with the updated chain propagated into the child parser so
	// transitive cycles (a→b→a) are also caught.
	included := parseInternalWithState(absTarget, data, chain, p.includes)

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
