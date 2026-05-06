package main

import (
	"fmt"
	"os"
	"path/filepath"
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

func (*ModuleStmt) stmtMarker()  {}
func (*PeerdirStmt) stmtMarker() {}
func (*SrcsStmt) stmtMarker()    {}
func (*SetStmt) stmtMarker()     {}
func (*EndStmt) stmtMarker()     {}
func (*UnknownStmt) stmtMarker() {}

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

// ParseFile reads path and parses it as a ya.make file.
func ParseFile(path string) (*MakeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	abs, absErr := filepath.Abs(path)
	if absErr != nil {
		// Fall back to caller-provided path if Abs fails for any reason.
		abs = path
	}
	mf, err := Parse(abs, data)
	if err != nil {
		return nil, err
	}
	return mf, nil
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

func (l *lexer) errorf(line, col int, format string, args ...any) *ParseError {
	return &ParseError{
		File:    l.name,
		Line:    line,
		Col:     col,
		Message: fmt.Sprintf(format, args...),
	}
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
	case '_', '-', '.', '/', '+', ':', '=', '*', '?', '$', '%', '~', ',', '!', '{', '}', '#':
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

// next returns the next token. On EOF it returns a tokEOF token with no error.
// On a malformed token (e.g. unterminated string) it returns a *ParseError.
func (l *lexer) next() (token, error) {
	return l.readToken()
}

func (l *lexer) readToken() (token, error) {
	l.skipTrivia()
	if l.pos >= len(l.src) {
		return token{kind: tokEOF, line: l.line, col: l.col}, nil
	}
	startLine, startCol := l.line, l.col
	b := l.src[l.pos]
	switch {
	case b == '(':
		l.advance()
		return token{kind: tokLParen, line: startLine, col: startCol}, nil
	case b == ')':
		l.advance()
		return token{kind: tokRParen, line: startLine, col: startCol}, nil
	case b == '"':
		return l.readString(startLine, startCol)
	case isIdentStart(b):
		return l.readIdentOrWord(startLine, startCol)
	case isWordByte(b):
		return l.readWord(startLine, startCol)
	default:
		// Consume the offending byte so callers can keep advancing past
		// malformed input if they want to (we always return early below).
		l.advance()
		return token{}, l.errorf(startLine, startCol, "unexpected character %q", b)
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
func (l *lexer) readString(startLine, startCol int) (token, error) {
	l.advance() // consume opening "
	var buf []byte
	for {
		if l.pos >= len(l.src) {
			return token{}, l.errorf(startLine, startCol, "unterminated string")
		}
		b := l.src[l.pos]
		if b == '"' {
			l.advance() // consume closing "
			return token{kind: tokString, val: string(buf), line: startLine, col: startCol}, nil
		}
		if b == '\n' || b == '\r' {
			return token{}, l.errorf(startLine, startCol, "unterminated string")
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
func (l *lexer) readIdentOrWord(startLine, startCol int) (token, error) {
	start := l.pos
	pureIdent := true
	for l.pos < len(l.src) {
		b := l.src[l.pos]
		if isIdentCont(b) {
			l.advance()
			continue
		}
		if isWordByte(b) {
			pureIdent = false
			l.advance()
			continue
		}
		break
	}
	val := string(l.src[start:l.pos])
	kind := tokIdent
	if !pureIdent {
		kind = tokWord
	}
	return token{kind: kind, val: val, line: startLine, col: startCol}, nil
}

// readWord reads a bare-word token (path, lowercase identifier, etc.).
func (l *lexer) readWord(startLine, startCol int) (token, error) {
	start := l.pos
	for l.pos < len(l.src) && isWordByte(l.src[l.pos]) {
		l.advance()
	}
	return token{kind: tokWord, val: string(l.src[start:l.pos]), line: startLine, col: startCol}, nil
}

// ----------------------------------------------------------------------
// Parser
// ----------------------------------------------------------------------

// Parse parses src as a ya.make file. name is used in error messages.
func Parse(name string, src []byte) (*MakeFile, error) {
	p := &parser{lex: newLexer(name, src), name: name}
	mf := &MakeFile{Path: name}
	for {
		tok, err := p.lex.next()
		if err != nil {
			return nil, err
		}
		if tok.kind == tokEOF {
			break
		}
		// A macro name is either a tokIdent (e.g. PROGRAM, PEERDIR — the
		// uppercase macros our buildStmt routes specially) or a tokWord
		// whose textual form is identifier-shaped (e.g. "lowercase_macro",
		// "Mixed_Case"). The latter case lets per-spec "everything else"
		// macros parse as UnknownStmt rather than erroring out. A non-
		// ident-shaped word (e.g. a stray path "a/b" at top level) is
		// still a parse error.
		if tok.kind != tokIdent && !(tok.kind == tokWord && isIdentShapedName(tok.val)) {
			return nil, p.lex.errorf(tok.line, tok.col, "expected macro name, got %s", describeToken(tok))
		}
		stmt, err := p.parseMacro(tok)
		if err != nil {
			return nil, err
		}
		mf.Stmts = append(mf.Stmts, stmt)
	}
	return mf, nil
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
	lex  *lexer
	name string
}

// parseMacro is called with the macro-name token already consumed. It
// expects a '(' next, then args (any number, including zero), then ')'.
func (p *parser) parseMacro(nameTok token) (Stmt, error) {
	lp, err := p.lex.next()
	if err != nil {
		return nil, err
	}
	if lp.kind != tokLParen {
		return nil, p.lex.errorf(lp.line, lp.col, "expected '(' after macro name %q, got %s", nameTok.val, describeToken(lp))
	}
	var args []string
	for {
		tok, err := p.lex.next()
		if err != nil {
			return nil, err
		}
		switch tok.kind {
		case tokRParen:
			return p.buildStmt(nameTok, args)
		case tokEOF:
			return nil, p.lex.errorf(nameTok.line, nameTok.col, "unterminated macro call %q (missing ')')", nameTok.val)
		case tokIdent, tokWord, tokString:
			args = append(args, tok.val)
		case tokLParen:
			return nil, p.lex.errorf(tok.line, tok.col, "unexpected '(' inside macro call %q", nameTok.val)
		default:
			return nil, p.lex.errorf(tok.line, tok.col, "unexpected %s inside macro call %q", describeToken(tok), nameTok.val)
		}
	}
}

// buildStmt routes recognized macro names to typed Stmt; everything else
// becomes UnknownStmt.
func (p *parser) buildStmt(nameTok token, args []string) (Stmt, error) {
	switch nameTok.val {
	case "PROGRAM", "LIBRARY":
		return &ModuleStmt{Name: nameTok.val, Args: args, Line: nameTok.line}, nil
	case "PEERDIR":
		return &PeerdirStmt{Paths: args, Line: nameTok.line}, nil
	case "SRCS":
		return &SrcsStmt{Sources: args, Line: nameTok.line}, nil
	case "SET":
		if len(args) != 2 {
			return nil, p.lex.errorf(nameTok.line, nameTok.col, "SET expects exactly 2 arguments (name and value), got %d", len(args))
		}
		return &SetStmt{Name: args[0], Value: args[1], Line: nameTok.line}, nil
	case "END":
		return &EndStmt{Line: nameTok.line}, nil
	default:
		return &UnknownStmt{Name: nameTok.val, Args: args, Line: nameTok.line}, nil
	}
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
	default:
		return fmt.Sprintf("token(kind=%d)", t.kind)
	}
}
