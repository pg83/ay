package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

var runAntlrKeywords = strKeySet(
	"IN",
	"IN_NOPARSE",
	"IN_DEPS",
	"OUT",
	"OUT_NOAUTO",
	"CWD",
	"OUTPUT_INCLUDES",
	"INDUCED_DEPS",
	"TOOL",
	"ENV",
	"STDOUT",
	"STDOUT_NOAUTO",
	"GRAMMAR_FILES",
	"GRAMMAR_CWD",
)

var runProgramKeywords = strKeySet(
	"IN",
	"IN_NOPARSE",
	"OUT",
	"OUT_NOAUTO",
	"STDOUT",
	"STDOUT_NOAUTO",
	"CWD",
	"ENV",
	"OUTPUT_INCLUDES",
	"INDUCED_DEPS",
	"IN_DEPS",
	"TOOL",
)

// strKeySet interns a keyword list into a BitSet over the STR ids — the
// parser's argument tokens are STR ids and the kw constants intern first at
// init (tiny ids), so a membership check is one bit probe, no map.
func strKeySet(words ...string) BitSet {
	var out BitSet

	for _, w := range words {
		out.add(uint32(internStr(w)))
	}

	return out
}

type MakeFile struct {
	Path  string
	Stmts []Stmt
}

type Stmt interface {
	stmtMarker()
}

type ModuleStmt struct {
	Name TOK
	Args []STR
	Line int
}

type PeerdirStmt struct {
	Paths []STR
	Line  int
}

// DeclareResourceStmt is a DECLARE_EXTERNAL_RESOURCE /
// DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE[_BY_JSON] call inside a RESOURCES_LIBRARY.
// Host-platform uri selection (and json reading for the BY_JSON form) is deferred
// to gen time, where the host Platform and FS are available; Args holds the raw
// macro arguments.
type DeclareResourceStmt struct {
	Macro TOK
	Args  []STR
	Line  int
}

type SrcsStmt struct {
	Sources []STR
	Line    int
}

type SetStmt struct {
	Name    string
	NameEnv ENV // interned Name, set at parse so gen never re-interns
	Value   string
	Line    int
}

type EndStmt struct {
	Line int
}

type UnknownStmt struct {
	Name TOK
	Args []STR
	Line int
}

type IfStmt struct {
	Cond Expr
	Then []Stmt
	Else []Stmt
	Line int
}

type IncludeStmt struct {
	Path string
	Line int
}

type JoinSrcsStmt struct {
	OutputName string
	Sources    []STR
	Line       int
}

type AddInclStmt struct {
	GlobalPaths      []STR
	OneLevelPaths    []STR
	OwnPaths         []STR
	CythonPaths      []STR
	AsmPaths         []STR
	ProtoGlobalPaths []STR
	// UserGlobalPaths contains GLOBAL and ONE_LEVEL paths in declaration order —
	// the equivalent of ymake's UserGlobal. Used to preserve upstream -I ordering.
	UserGlobalPaths []STR

	AllPaths []STR
	Line     int
}

type CFlagsStmt struct {
	GlobalFlags []STR
	OwnFlags    []STR
	Line        int
}

type CXXFlagsStmt struct {
	GlobalFlags []STR
	OwnFlags    []STR
	Line        int
}

type CONLYFlagsStmt struct {
	GlobalFlags []STR
	OwnFlags    []STR
	Line        int
}

type LDFlagsStmt struct {
	Flags []STR
	Line  int
}

type SrcDirStmt struct {
	Dirs []STR
	Line int
}

type GlobalSrcsStmt struct {
	Sources []STR
	Line    int
}

type GenerateEnumSerializationStmt struct {
	Header  string
	Variant string
	Line    int
}

type DefaultVarStmt struct {
	VarName string
	NameEnv ENV // interned VarName, set at parse so gen never re-interns
	Value   string
	Line    int
}

type RunProgramStmt struct {
	ToolPath       STR
	Args           []STR
	INFiles        []STR
	OUTFiles       []STR
	OUTNoAutoFiles []STR
	StdoutFile     *STR
	EnvPairs       []STR
	CWD            *STR
	OutputIncludes []STR
	ToolPaths      []STR
	Line           int
}

type RunPythonStmt struct {
	ScriptPath     STR
	Args           []STR
	INFiles        []STR
	OUTFiles       []STR
	OUTNoAutoFiles []STR
	StdoutFile     *STR
	EnvPairs       []STR
	CWD            *STR
	OutputIncludes []STR
	Line           int
}

type ConfigureFileStmt struct {
	Src  string
	Dst  string
	Line int
}

type CreateBuildInfoStmt struct {
	OutputHeader string
	Line         int
}

type RunAntlr4CppStmt struct {
	Grammar        STR
	Options        []STR
	Visitor        bool
	Listener       bool
	OutputIncludes []STR
	Line           int
}

type RunAntlr4CppSplitStmt struct {
	Lexer          STR
	Parser         STR
	Visitor        bool
	Listener       bool
	OutputIncludes []STR
	Line           int
}

type RunAntlrStmt struct {
	Macro          string
	Args           []STR
	INFiles        []STR
	OUTFiles       []STR
	OUTNoAutoFiles []STR
	CWD            *STR
	OutputIncludes []STR
	Line           int
}

type ResourcePair struct {
	Path string
	Key  string
}

type ResourceStmt struct {
	Pairs []ResourcePair
	Line  int
}

type ResourceFilesStmt struct {
	Args []STR
	Line int
}

func (*ModuleStmt) stmtMarker() {
}

func (*DeclareResourceStmt) stmtMarker() {
}

func (*PeerdirStmt) stmtMarker() {
}

func (*SrcsStmt) stmtMarker() {
}

func (*SetStmt) stmtMarker() {
}

func (*EndStmt) stmtMarker() {
}

func (*UnknownStmt) stmtMarker() {
}

func (*IfStmt) stmtMarker() {
}

func (*IncludeStmt) stmtMarker() {
}

func (*JoinSrcsStmt) stmtMarker() {
}

func (*AddInclStmt) stmtMarker() {
}

func (*CFlagsStmt) stmtMarker() {
}

func (*CXXFlagsStmt) stmtMarker() {
}

func (*CONLYFlagsStmt) stmtMarker() {
}

func (*LDFlagsStmt) stmtMarker() {
}

func (*SrcDirStmt) stmtMarker() {
}

func (*GlobalSrcsStmt) stmtMarker() {
}

func (*GenerateEnumSerializationStmt) stmtMarker() {
}

func (*DefaultVarStmt) stmtMarker() {
}

func (*RunProgramStmt) stmtMarker() {
}

func (*RunPythonStmt) stmtMarker() {
}

func (*ConfigureFileStmt) stmtMarker() {
}

func (*CreateBuildInfoStmt) stmtMarker() {
}

func (*RunAntlr4CppStmt) stmtMarker() {
}

func (*RunAntlr4CppSplitStmt) stmtMarker() {
}

func (*RunAntlrStmt) stmtMarker() {
}

func (*ResourceStmt) stmtMarker() {
}

func (*ResourceFilesStmt) stmtMarker() {
}

type Expr interface {
	exprMarker()
}

type ExprIdent struct {
	Name string
	// Env is the macro name interned to its dense ENV id at parse time, so IF
	// evaluation indexes the Environment array without re-hashing the string.
	Env ENV
}

type ExprNot struct {
	Of Expr
}

type ExprAnd struct {
	Left, Right Expr
}

type ExprOr struct {
	Left, Right Expr
}

type ExprString struct {
	Value string
}

type ExprInt struct {
	Value int
}

type ExprEq struct {
	Left, Right Expr
}

type ExprLt struct {
	Left, Right Expr
}

func (*ExprIdent) exprMarker() {
}

func (*ExprNot) exprMarker() {
}

func (*ExprAnd) exprMarker() {
}

func (*ExprOr) exprMarker() {
}

func (*ExprString) exprMarker() {
}

func (*ExprInt) exprMarker() {
}

func (*ExprEq) exprMarker() {
}

func (*ExprLt) exprMarker() {
}

type ParseError struct {
	File    string
	Line    int
	Col     int
	Message string
}

func (e *ParseError) error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Message)
	}

	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Message)
}

// Error implements the error interface; internal code calls error().
func (e *ParseError) Error() string {
	return e.error()
}

// parseFile parses the ya.make at the source-root-relative rel. The parser's
// whole path space (file names, INCLUDE targets, the once/cycle sets) is
// root-relative: build files cannot reference anything outside the FS.
func parseFile(fs FS, rel string) (mf *MakeFile, err error) {
	exc := try(func() {
		mf = throw2(parse(fs, cleanRel(rel), readOwnedForParse(fs, cleanRel(rel))))
	})

	if exc != nil {
		err = exc.asError()
		mf = nil
	}

	return mf, err
}

// readOwnedForParse returns an owned copy of rel's content: the lexer holds
// its src lazily, and a nested INCLUDE triggers another read mid-parse — which
// would overwrite the reused FS read buffer the outer lexer still points at.
func readOwnedForParse(fs FS, rel string) []byte {
	return append([]byte(nil), fs.read(rel)...)
}

type TokKind int

const (
	tokEOF TokKind = iota
	tokIdent
	tokString
	tokWord
	tokLParen
	tokRParen
	tokInt
	tokEq
	tokLt
	tokNotEq
	tokGe
	tokGt
)

type Token struct {
	kind TokKind
	val  string
	line int
	col  int
}

type Lexer struct {
	name string
	src  []byte
	pos  int
	line int
	col  int

	prevByte byte

	// tokBuf is the reused per-token assembly buffer: token text is interned
	// straight from it (internBytes copies on a table miss), so no per-token
	// []byte survives the call and the buffer never escapes.
	tokBuf []byte
}

func newLexer(name string, src []byte) *Lexer {
	return &Lexer{
		name:     name,
		src:      src,
		pos:      0,
		line:     1,
		col:      1,
		prevByte: 0,
	}
}

func (l *Lexer) throwParse(line, col int, format string, args ...any) {
	pe := &ParseError{
		File:    l.name,
		Line:    line,
		Col:     col,
		Message: fmt.Sprintf(format, args...),
	}

	newException(pe).throw()
}

func (l *Lexer) advance() byte {
	b := l.src[l.pos]
	l.pos++

	switch {
	case b == '\n':
		l.line++
		l.col = 1
		l.prevByte = b
	case b == '\r':

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

		'\\':
		return true
	}

	return false
}

func (l *Lexer) skipTrivia() {
	for l.pos < len(l.src) {
		b := l.src[l.pos]

		switch {
		case isWhitespace(b):
			l.advance()
		case b == '#' && commentBoundary(l.prevByte):

			for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.advance()
			}
		default:
			return
		}
	}
}

func commentBoundary(prev byte) bool {
	if prev == 0 {
		return true
	}

	if prev == '(' {
		return true
	}

	return isWhitespace(prev)
}

func (l *Lexer) next() Token {
	return l.readToken()
}

func (l *Lexer) readToken() Token {
	l.skipTrivia()

	if l.pos >= len(l.src) {
		return Token{kind: tokEOF, line: l.line, col: l.col}
	}

	startLine, startCol := l.line, l.col
	b := l.src[l.pos]

	switch {
	case b == '(':
		l.advance()

		return Token{kind: tokLParen, line: startLine, col: startCol}
	case b == ')':
		l.advance()

		return Token{kind: tokRParen, line: startLine, col: startCol}
	case b == '"' || b == '\'':
		return l.readString(startLine, startCol, b)
	case b == '<':

		l.advance()

		return Token{kind: tokLt, line: startLine, col: startCol}
	case b == '>':

		l.advance()

		if l.pos < len(l.src) && l.src[l.pos] == '=' {
			l.advance()

			return Token{kind: tokGe, line: startLine, col: startCol}
		}

		return Token{kind: tokGt, line: startLine, col: startCol}
	case b == '=' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '=':

		l.advance()
		l.advance()

		return Token{kind: tokEq, line: startLine, col: startCol}
	case b == '!' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '=':

		l.advance()
		l.advance()

		return Token{kind: tokNotEq, line: startLine, col: startCol}
	case b >= '0' && b <= '9':
		return l.readNumberOrWord(startLine, startCol)
	case isIdentStart(b):
		return l.readIdentOrWord(startLine, startCol)
	case isWordByte(b):
		return l.readWord(startLine, startCol)
	default:

		l.advance()
		l.throwParse(startLine, startCol, "unexpected character %q", b)

		return Token{}
	}
}

func (l *Lexer) readString(startLine, startCol int, quote byte) Token {
	l.advance()

	buf := l.tokBuf[:0]

	for {
		if l.pos >= len(l.src) {
			l.throwParse(startLine, startCol, "unterminated string")
		}

		b := l.src[l.pos]

		if b == quote {
			l.advance()

			l.tokBuf = buf

			return Token{kind: tokString, val: internBytes(buf).string(), line: startLine, col: startCol}
		}

		if b == '\n' || b == '\r' {
			l.throwParse(startLine, startCol, "unterminated string")
		}

		if b == '\\' && l.pos+1 < len(l.src) {
			buf = append(buf, l.advance())
			buf = append(buf, l.advance())

			continue
		}

		buf = append(buf, l.advance())
	}
}

func (l *Lexer) readIdentOrWord(startLine, startCol int) Token {
	buf := l.tokBuf[:0]
	pureIdent := true

	for l.pos < len(l.src) {
		b := l.src[l.pos]

		if b == '\\' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
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

	l.tokBuf = buf
	// Token text is interned straight from the buffer: on a table hit (the
	// overwhelming case — tokens repeat across ya.makes) the value is a view,
	// not a fresh string; downstream internStr(tok.val) then hits the same slot.
	val := internBytes(buf).string()
	kind := tokIdent

	if !pureIdent {
		kind = tokWord
	}

	return Token{kind: kind, val: val, line: startLine, col: startCol}
}

func (l *Lexer) readWord(startLine, startCol int) Token {
	buf := l.tokBuf[:0]

	for l.pos < len(l.src) {
		b := l.src[l.pos]

		if b == '\\' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
			buf = append(buf, l.advance())
			buf = append(buf, l.advance())

			continue
		}

		if !isWordByte(b) && !(b == '@' && len(buf) > 0) {
			break
		}

		buf = append(buf, l.advance())
	}

	l.tokBuf = buf

	return Token{kind: tokWord, val: internBytes(buf).string(), line: startLine, col: startCol}
}

func (l *Lexer) readNumberOrWord(startLine, startCol int) Token {
	start := l.pos

	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.advance()
	}

	if l.pos < len(l.src) && isWordByte(l.src[l.pos]) {
		for l.pos < len(l.src) && isWordByte(l.src[l.pos]) {
			l.advance()
		}

		return Token{kind: tokWord, val: internBytes(l.src[start:l.pos]).string(), line: startLine, col: startCol}
	}

	return Token{kind: tokInt, val: internBytes(l.src[start:l.pos]).string(), line: startLine, col: startCol}
}

func parse(fs FS, name string, src []byte) (mf *MakeFile, err error) {
	exc := try(func() {
		mf = parseInternal(fs, name, src)
	})

	if exc != nil {
		err = exc.asError()
		mf = nil
	}

	return mf, err
}

func parseInternal(fs FS, name string, src []byte) *MakeFile {
	return parseInternalWithStack(fs, name, src, nil)
}

func parseInternalWithStack(fs FS, name string, src []byte, stack []string) *MakeFile {
	return parseInternalWithState(fs, name, src, stack, newIncludeState())
}

func parseInternalWithState(fs FS, name string, src []byte, stack []string, includes *IncludeState) *MakeFile {
	src = bytes.TrimPrefix(src, []byte{0xEF, 0xBB, 0xBF})
	p := &Parser{lex: newLexer(name, src), name: name, includeStack: stack, includes: includes, fs: fs}
	mf := &MakeFile{Path: name}
	mf.Stmts, _ = p.parseStmts(termTopLevel)

	return mf
}

type IncludeState struct {
	once map[string]struct{}
}

func newIncludeState() *IncludeState {
	return &IncludeState{once: map[string]struct{}{}}
}

type StmtTerminator int

const (
	termTopLevel StmtTerminator = iota
	termIfBody
)

func (p *Parser) parseStmts(term StmtTerminator) (stmts []Stmt, endTok Token) {
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

		if term == termIfBody && (tok.val == "ELSE" || tok.val == "ELSEIF" || tok.val == "ENDIF") {
			return stmts, tok
		}

		stmts = p.parseMacroInto(stmts, tok)
	}
}

func (p *Parser) parseMacroInto(into []Stmt, nameTok Token) []Stmt {
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

func (p *Parser) applyIncludeOnce(nameTok Token) {
	args := p.parseMacroArgs(nameTok)
	enabled := true

	if len(args) > 1 {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE_ONCE expects 0 or 1 arguments, got %d", len(args))
	}

	if len(args) == 1 && args[0].string() == "no" {
		enabled = false
	}

	if !enabled {
		return
	}

	p.includes.once[p.name] = struct{}{}
}

func (p *Parser) parseMacroArgs(nameTok Token) []STR {
	lp := p.lex.next()

	if lp.kind != tokLParen {
		p.lex.throwParse(lp.line, lp.col, "expected '(' after macro name %q, got %s", nameTok.val, describeToken(lp))
	}

	var args []STR

	for {
		tok := p.lex.next()

		switch tok.kind {
		case tokRParen:
			return args
		case tokEOF:
			p.lex.throwParse(nameTok.line, nameTok.col, "unterminated macro call %q (missing ')')", nameTok.val)
		case tokIdent, tokWord, tokString, tokInt:
			// The parser's argument output is interned: every downstream layer
			// (expansion, collect, emit) works in STR id space.
			args = append(args, internStr(tok.val))
		case tokLParen:
			p.lex.throwParse(tok.line, tok.col, "unexpected '(' inside macro call %q", nameTok.val)
		default:
			p.lex.throwParse(tok.line, tok.col, "unexpected %s inside macro call %q", describeToken(tok), nameTok.val)
		}
	}
}

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

type Parser struct {
	lex          *Lexer
	name         string
	includeStack []string
	includes     *IncludeState
	fs           FS
}

func (p *Parser) buildStmt(nameTok Token, args []STR) Stmt {
	switch nameTok.val {
	case "PROGRAM", "LIBRARY",

		"PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
		"PY3_PROGRAM_BIN", "PY2_PROGRAM", "PY3_PROGRAM",
		"YQL_UDF_YDB", "YQL_UDF_CONTRIB",
		"PROTO_LIBRARY",
		"DLL", "SO_PROGRAM", "DYNAMIC_LIBRARY",
		"PACKAGE", "UNION", "RESOURCES_LIBRARY",
		"PREBUILT_PROGRAM",
		"UNITTEST_FOR":
		return &ModuleStmt{Name: internTok(nameTok.val), Args: args, Line: nameTok.line}
	case "DECLARE_EXTERNAL_RESOURCE",
		"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE",
		"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON":
		return &DeclareResourceStmt{Macro: internTok(nameTok.val), Args: args, Line: nameTok.line}
	case "PEERDIR":
		return &PeerdirStmt{Paths: args, Line: nameTok.line}
	case "SRCS":
		return &SrcsStmt{Sources: args, Line: nameTok.line}
	case "SET":
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "SET expects at least 1 argument (name), got %d", len(args))
		}

		value := ""

		if len(args) > 1 {
			value = strings.Join(strStrings(args[1:]), " ")
		}

		return &SetStmt{Name: args[0].string(), NameEnv: internEnv(args[0].string()), Value: value, Line: nameTok.line}
	case "END":
		return &EndStmt{Line: nameTok.line}
	case "JOIN_SRCS":
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "JOIN_SRCS expects at least one argument (the output name)")
		}

		sources := append([]STR(nil), args[1:]...)

		return &JoinSrcsStmt{OutputName: args[0].string(), Sources: sources, Line: nameTok.line}
	case "ADDINCL":
		globalPaths, oneLevelPaths, ownPaths, cythonPaths, asmPaths, protoGlobalPaths, userGlobalPaths, allPaths := splitAddInclPaths(args)

		return &AddInclStmt{GlobalPaths: globalPaths, OneLevelPaths: oneLevelPaths, OwnPaths: ownPaths, CythonPaths: cythonPaths, AsmPaths: asmPaths, ProtoGlobalPaths: protoGlobalPaths, UserGlobalPaths: userGlobalPaths, AllPaths: allPaths, Line: nameTok.line}
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
		return &LDFlagsStmt{Flags: append([]STR(nil), args...), Line: nameTok.line}
	case "SRCDIR":
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "SRCDIR expects at least 1 argument, got %d", len(args))
		}

		return &SrcDirStmt{Dirs: append([]STR(nil), args...), Line: nameTok.line}
	case "GLOBAL_SRCS":
		return &GlobalSrcsStmt{Sources: append([]STR(nil), args...), Line: nameTok.line}
	case "GENERATE_ENUM_SERIALIZATION":
		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "GENERATE_ENUM_SERIALIZATION expects exactly 1 argument (header path), got %d", len(args))
		}

		return &GenerateEnumSerializationStmt{Header: args[0].string(), Variant: "plain", Line: nameTok.line}
	case "GENERATE_ENUM_SERIALIZATION_WITH_HEADER":
		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "GENERATE_ENUM_SERIALIZATION_WITH_HEADER expects exactly 1 argument (header path), got %d", len(args))
		}

		return &GenerateEnumSerializationStmt{Header: args[0].string(), Variant: "with_header", Line: nameTok.line}
	case "GENERATE_ENUM_SERIALIZATION_NOUTF":
		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "GENERATE_ENUM_SERIALIZATION_NOUTF expects exactly 1 argument (header path), got %d", len(args))
		}

		return &GenerateEnumSerializationStmt{Header: args[0].string(), Variant: "noutf", Line: nameTok.line}
	case "DEFAULT":

		varName := ""
		value := ""

		if len(args) >= 1 {
			varName = args[0].string()
		}

		if len(args) >= 2 {
			value = args[1].string()
		}

		return &DefaultVarStmt{VarName: varName, NameEnv: internEnv(varName), Value: value, Line: nameTok.line}
	case "CONFIGURE_FILE":

		if len(args) != 2 {
			p.lex.throwParse(nameTok.line, nameTok.col, "CONFIGURE_FILE expects exactly 2 arguments (src dst), got %d", len(args))
		}

		return &ConfigureFileStmt{Src: args[0].string(), Dst: args[1].string(), Line: nameTok.line}
	case "CREATE_BUILDINFO_FOR":

		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "CREATE_BUILDINFO_FOR expects exactly 1 argument, got %d", len(args))
		}

		return &CreateBuildInfoStmt{OutputHeader: args[0].string(), Line: nameTok.line}
	case "RUN_ANTLR4_CPP":

		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "RUN_ANTLR4_CPP expects at least 1 argument (grammar)")
		}

		return parseRunAntlr4Cpp(args, nameTok.line)
	case "RUN_ANTLR4_CPP_SPLIT":

		if len(args) < 2 {
			p.lex.throwParse(nameTok.line, nameTok.col, "RUN_ANTLR4_CPP_SPLIT expects at least 2 arguments (lexer parser)")
		}

		return parseRunAntlr4CppSplit(args, nameTok.line)
	case "RUN_ANTLR", "RUN_ANTLR4":
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "%s expects at least 1 argument", nameTok.val)
		}

		return parseRunAntlr(args, nameTok)
	case "RUN_PROGRAM", "RUN_PY3_PROGRAM":

		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "%s expects at least 1 argument (tool path)", nameTok.val)
		}

		return parseRunProgram(args, nameTok.line)
	case "RUN_PYTHON3":
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "RUN_PYTHON3 expects at least 1 argument (script path)")
		}

		return parseRunPython(args, nameTok.line)
	case "RESOURCE":

		return parseResource(args, nameTok)
	case "RESOURCE_FILES":
		return &ResourceFilesStmt{Args: append([]STR(nil), args...), Line: nameTok.line}
	default:
		return &UnknownStmt{Name: internTok(nameTok.val), Args: args, Line: nameTok.line}
	}
}

func parseRunAntlr4Cpp(args []STR, line int) *RunAntlr4CppStmt {
	stmt := &RunAntlr4CppStmt{Grammar: args[0], Line: line}
	i := 1

	for i < len(args) {
		switch args[i] {
		case kwVISITOR:
			stmt.Visitor = true
			i++
		case kwNO_LISTENER, kwLISTENER:

			if args[i] == kwNO_LISTENER {
				stmt.Listener = false
			} else {
				stmt.Listener = true
			}

			i++
		case kwOUTPUT_INCLUDES:

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
				i++
			}
		case kwIN, kwOUT, kwOUT_NOAUTO, kwINDUCED_DEPS, kwTOOL:

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

func parseRunAntlr4CppSplit(args []STR, line int) *RunAntlr4CppSplitStmt {
	stmt := &RunAntlr4CppSplitStmt{Lexer: args[0], Parser: args[1], Line: line}

	for i := 2; i < len(args); i++ {
		switch args[i] {
		case kwVISITOR:
			stmt.Visitor = true
		case kwLISTENER:
			stmt.Listener = true
		case kwNO_LISTENER:

		case kwOUTPUT_INCLUDES:

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
				i++
			}

			i--
		case kwIN, kwOUT, kwOUT_NOAUTO, kwINDUCED_DEPS, kwTOOL:

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				i++
			}

			i--
		}
	}

	return stmt
}

func parseRunAntlr(args []STR, nameTok Token) *RunAntlrStmt {
	stmt := &RunAntlrStmt{Macro: nameTok.val, Line: nameTok.line}
	currentSection := kwARGS

	for i := 0; i < len(args); i++ {
		tok := args[i]

		if runAntlrKeywords.has(uint32(tok)) {
			currentSection = tok

			continue
		}

		switch currentSection {
		case kwARGS:
			stmt.Args = append(stmt.Args, tok)
		case kwIN, kwIN_NOPARSE, kwIN_DEPS:
			stmt.INFiles = append(stmt.INFiles, tok)
		case kwOUT:
			stmt.OUTFiles = append(stmt.OUTFiles, tok)
		case kwOUT_NOAUTO:
			stmt.OUTNoAutoFiles = append(stmt.OUTNoAutoFiles, tok)
		case kwCWD:
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case kwOUTPUT_INCLUDES, kwINDUCED_DEPS, kwTOOL, kwENV, kwGRAMMAR_FILES, kwGRAMMAR_CWD:
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		}
	}

	return stmt
}

func isRunAntlrKeyword(s STR) bool {
	switch s {
	case kwVISITOR, kwLISTENER, kwNO_LISTENER, kwOUTPUT_INCLUDES,
		kwIN, kwIN_NOPARSE, kwIN_DEPS, kwOUT, kwOUT_NOAUTO,
		kwINDUCED_DEPS, kwTOOL, kwCWD, kwENV, kwSTDOUT,
		kwSTDOUT_NOAUTO, kwGRAMMAR_FILES, kwGRAMMAR_CWD:
		return true
	}

	return false
}

func parseRunProgram(args []STR, line int) *RunProgramStmt {
	stmt := &RunProgramStmt{ToolPath: args[0], Line: line}
	i := 1
	currentSection := kwARGS

	for i < len(args) {
		tok := args[i]

		if runProgramKeywords.has(uint32(tok)) {
			currentSection = tok
			i++

			continue
		}

		switch currentSection {
		case kwARGS:
			stmt.Args = append(stmt.Args, tok)
		case kwIN, kwIN_NOPARSE, kwIN_DEPS:
			stmt.INFiles = append(stmt.INFiles, tok)
		case kwOUT:
			stmt.OUTFiles = append(stmt.OUTFiles, tok)
		case kwOUT_NOAUTO:
			stmt.OUTNoAutoFiles = append(stmt.OUTNoAutoFiles, tok)
		case kwSTDOUT, kwSTDOUT_NOAUTO:
			if stmt.StdoutFile == nil {
				stmt.StdoutFile = &tok
			}
		case kwENV:
			stmt.EnvPairs = append(stmt.EnvPairs, tok)
		case kwCWD:
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case kwOUTPUT_INCLUDES, kwINDUCED_DEPS:
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		case kwTOOL:
			stmt.ToolPaths = append(stmt.ToolPaths, tok)
		}

		i++
	}

	return stmt
}

func parseRunPython(args []STR, line int) *RunPythonStmt {
	stmt := &RunPythonStmt{ScriptPath: args[0], Line: line}
	i := 1
	currentSection := kwARGS

	for i < len(args) {
		tok := args[i]

		if runProgramKeywords.has(uint32(tok)) {
			currentSection = tok
			i++

			continue
		}

		switch currentSection {
		case kwARGS:
			stmt.Args = append(stmt.Args, tok)
		case kwIN, kwIN_NOPARSE, kwIN_DEPS:
			stmt.INFiles = append(stmt.INFiles, tok)
		case kwOUT:
			stmt.OUTFiles = append(stmt.OUTFiles, tok)
		case kwOUT_NOAUTO:
			stmt.OUTNoAutoFiles = append(stmt.OUTNoAutoFiles, tok)
		case kwSTDOUT, kwSTDOUT_NOAUTO:
			if stmt.StdoutFile == nil {
				stmt.StdoutFile = &tok
			}
		case kwENV:
			stmt.EnvPairs = append(stmt.EnvPairs, tok)
		case kwCWD:
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case kwOUTPUT_INCLUDES, kwINDUCED_DEPS, kwTOOL:
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		}

		i++
	}

	return stmt
}

func parseResource(args []STR, nameTok Token) *ResourceStmt {
	rest := args

	for i := 0; i < 2 && len(rest) > 0; i++ {
		if rest[0] == kwDONT_PARSE || rest[0] == kwDONT_COMPRESS {
			rest = rest[1:]

			continue
		}

		break
	}

	if len(rest)%2 != 0 {
		throwFmt("RESOURCE at line %d: argument count after DONT_PARSE/DONT_COMPRESS strip must be even (got %d)", nameTok.line, len(rest))
	}

	pairs := make([]ResourcePair, 0, len(rest)/2)

	for i := 0; i < len(rest); i += 2 {
		pairs = append(pairs, ResourcePair{Path: rest[i].string(), Key: rest[i+1].string()})
	}

	return &ResourceStmt{Pairs: pairs, Line: nameTok.line}
}

func unescapeFlag(s string) string {
	return strings.ReplaceAll(s, `\"`, `"`)
}

func splitFlagsByGlobal(args []STR) (globalFlags, ownFlags []STR) {
	for i := 0; i < len(args); i++ {
		if args[i] == kwGLOBAL {
			i++

			if i < len(args) {
				globalFlags = append(globalFlags, internStr(unescapeFlag(args[i].string())))
			}
		} else {
			ownFlags = append(ownFlags, internStr(unescapeFlag(args[i].string())))
		}
	}

	return globalFlags, ownFlags
}

func splitAddInclPaths(args []STR) (globalPaths, oneLevelPaths, ownPaths, cythonPaths, asmPaths, protoGlobalPaths, userGlobalPaths, allPaths []STR) {
	for i := 0; i < len(args); i++ {
		if args[i] == kwONE_LEVEL {
			i++

			if i < len(args) {
				oneLevelPaths = append(oneLevelPaths, args[i])
				ownPaths = append(ownPaths, args[i])
				userGlobalPaths = append(userGlobalPaths, args[i])
				allPaths = append(allPaths, args[i])
			}

			continue
		}

		if args[i] == kwFOR {
			if i+2 < len(args) && args[i+1].string() == "cython" {
				cythonPaths = append(cythonPaths, args[i+2])
				i += 2

				continue
			}

			if i+2 < len(args) && args[i+1].string() == "asm" {
				asmPaths = append(asmPaths, args[i+2])
				i += 2

				continue
			}

			i++

			continue
		}

		if args[i] == kwGLOBAL {
			i++

			if i < len(args) && args[i] == kwFOR {
				if i+2 < len(args) && args[i+1].string() == "cython" {
					cythonPaths = append(cythonPaths, args[i+2])
					i += 2

					continue
				}

				if i+2 < len(args) && args[i+1].string() == "asm" {
					asmPaths = append(asmPaths, args[i+2])
					i += 2

					continue
				}

				// `GLOBAL FOR proto X` — upstream's PROTO_ADDINCL macro
				// (yatool/build/conf/proto.conf:117-120) and contrib libs
				// such as protobuf use this to add `-I=$X` to the protoc
				// command in every transitive consumer's _PROTO__INCLUDE
				// chain. Track these separately from the C++ GLOBAL chain
				// (we were silently coalescing them, missing protobuf-src
				// from tablet_flat's protoc cmd vs REF).
				if i+2 < len(args) && args[i+1].string() == "proto" {
					protoGlobalPaths = append(protoGlobalPaths, args[i+2])
					i += 2

					continue
				}

				i++
				i++
			}

			if i < len(args) {
				globalPaths = append(globalPaths, args[i])
				userGlobalPaths = append(userGlobalPaths, args[i])
				allPaths = append(allPaths, args[i])
			}
		} else {
			ownPaths = append(ownPaths, args[i])
			allPaths = append(allPaths, args[i])
		}
	}

	return globalPaths, oneLevelPaths, ownPaths, cythonPaths, asmPaths, protoGlobalPaths, userGlobalPaths, allPaths
}

func (p *Parser) parseIf(ifTok Token) *IfStmt {
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

		nested := p.parseIf(endTok)
		node.Else = []Stmt{nested}

		return node
	}

	p.lex.throwParse(endTok.line, endTok.col, "internal: unexpected IF terminator %q", endTok.val)

	return nil
}

func (p *Parser) readCondTokens(ifTok Token) []Token {
	lp := p.lex.next()

	if lp.kind != tokLParen {
		p.lex.throwParse(lp.line, lp.col, "expected '(' after IF, got %s", describeToken(lp))
	}

	var (
		out   []Token
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
		case tokIdent, tokWord, tokString, tokInt, tokEq, tokLt, tokNotEq, tokGe, tokGt:
			out = append(out, tok)
		}
	}
}

func (p *Parser) consumeEmptyMacroArgs(kwTok Token) {
	p.parseMacroArgs(kwTok)
}

type CondParser struct {
	toks   []Token
	pos    int
	parent *Parser
	ifTok  Token
}

func parseCondExpr(parent *Parser, ifTok Token, toks []Token) Expr {
	cp := &CondParser{toks: toks, parent: parent, ifTok: ifTok}
	expr := cp.parseOr()

	if cp.pos != len(cp.toks) {
		t := cp.toks[cp.pos]
		parent.lex.throwParse(t.line, t.col, "unexpected %s in IF condition", describeToken(t))
	}

	return expr
}

func (c *CondParser) peek() (Token, bool) {
	if c.pos >= len(c.toks) {
		return Token{}, false
	}

	return c.toks[c.pos], true
}

func (c *CondParser) consume() Token {
	t := c.toks[c.pos]
	c.pos++

	return t
}

func (c *CondParser) parseOr() Expr {
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

func (c *CondParser) parseAnd() Expr {
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

func (c *CondParser) parseNot() Expr {
	t, ok := c.peek()

	if ok && t.kind == tokIdent && t.val == "NOT" {
		c.consume()

		return &ExprNot{Of: c.parseNot()}
	}

	return c.parseCmp()
}

func (c *CondParser) parseCmp() Expr {
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

		c.consume()
		right := c.parseAtom()
		c.rejectChainedCmp(t)

		return &ExprNot{Of: &ExprEq{Left: left, Right: right}}
	case tokGt:
		// a > b  ≡  b < a
		c.consume()
		right := c.parseAtom()
		c.rejectChainedCmp(t)

		return &ExprLt{Left: right, Right: left}
	case tokGe:
		// a >= b  ≡  !(a < b)
		c.consume()
		right := c.parseAtom()
		c.rejectChainedCmp(t)

		return &ExprNot{Of: &ExprLt{Left: left, Right: right}}
	}

	return left
}

func (c *CondParser) rejectChainedCmp(prev Token) {
	t, ok := c.peek()

	if !ok {
		return
	}

	if t.kind == tokEq || t.kind == tokLt || t.kind == tokNotEq || t.kind == tokGe || t.kind == tokGt {
		c.parent.lex.throwParse(t.line, t.col, "chained comparison %s after %s is not supported", describeToken(t), describeToken(prev))
	}
}

func (c *CondParser) parseAtom() Expr {
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

		n := 0

		for i := 0; i < len(t.val); i++ {
			n = n*10 + int(t.val[i]-'0')
		}

		return &ExprInt{Value: n}
	}

	if t.kind == tokIdent || (t.kind == tokWord && isIdentShapedName(t.val)) {
		if t.val == "AND" || t.val == "OR" || t.val == "NOT" {
			c.parent.lex.throwParse(t.line, t.col, "operator %q used as identifier in IF condition", t.val)
		}

		c.consume()

		return &ExprIdent{Name: t.val, Env: internEnv(t.val)}
	}

	c.parent.lex.throwParse(t.line, t.col, "unexpected %s in IF condition", describeToken(t))

	return nil
}

func (p *Parser) expandInclude(into []Stmt, nameTok Token) []Stmt {
	args := p.parseMacroArgs(nameTok)

	if len(args) != 1 {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE expects exactly 1 argument (the path), got %d", len(args))
	}

	rel := args[0].string()

	// The target is source-root-relative: either spelled from the root via
	// ${ARCADIA_ROOT}/, or relative to the including file's directory. An
	// absolute path would escape the FS — that is a build-file defect.
	var target string

	if suffix, ok := strings.CutPrefix(rel, "${ARCADIA_ROOT}/"); ok {
		target = cleanRel(suffix)
	} else if filepath.IsAbs(rel) {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE(%s): absolute paths escape the source root", rel)
	} else {
		target = cleanRel(joinRel(pathDir(p.name), rel))
	}

	if _, skip := p.includes.once[target]; skip {
		return into
	}

	chain := append(p.includeStack, p.name)

	for _, visited := range chain {
		if visited == target {
			chainStr := ""

			for i, v := range chain {
				if i > 0 {
					chainStr += " -> "
				}

				chainStr += v
			}

			chainStr += " -> " + target
			p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE cycle: %s", chainStr)
		}
	}

	if present, _ := p.fs.exists(srcRootVFS, target); !present {
		return into
	}

	data := readOwnedForParse(p.fs, target)

	included := parseInternalWithState(p.fs, target, data, chain, p.includes)

	return append(into, included.Stmts...)
}

func describeToken(t Token) string {
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
	case tokGe:
		return "'>='"
	case tokGt:
		return "'>'"
	default:
		return fmt.Sprintf("token(kind=%d)", t.kind)
	}
}
