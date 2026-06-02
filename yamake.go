package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

var runAntlrKeywords = map[string]bool{
	"IN":              true,
	"IN_NOPARSE":      true,
	"IN_DEPS":         true,
	"OUT":             true,
	"OUT_NOAUTO":      true,
	"CWD":             true,
	"OUTPUT_INCLUDES": true,
	"INDUCED_DEPS":    true,
	"TOOL":            true,
	"ENV":             true,
	"STDOUT":          true,
	"STDOUT_NOAUTO":   true,
	"GRAMMAR_FILES":   true,
	"GRAMMAR_CWD":     true,
}

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

type MakeFile struct {
	Path  string
	Stmts []Stmt
}

type Stmt interface {
	stmtMarker()
}

type ModuleStmt struct {
	Name string
	Args []string
	Line int
}

type PeerdirStmt struct {
	Paths []string
	Line  int
}

type SrcsStmt struct {
	Sources []string
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
	Name string
	Args []string
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
	Sources    []string
	Line       int
}

type AddInclStmt struct {
	GlobalPaths      []string
	OneLevelPaths    []string
	OwnPaths         []string
	CythonPaths      []string
	AsmPaths         []string
	ProtoGlobalPaths []string
	// UserGlobalPaths contains GLOBAL and ONE_LEVEL paths in declaration order —
	// the equivalent of ymake's UserGlobal. Used to preserve upstream -I ordering.
	UserGlobalPaths []string

	AllPaths []string
	Line     int
}

type CFlagsStmt struct {
	GlobalFlags []string
	OwnFlags    []string
	Line        int
}

type CXXFlagsStmt struct {
	GlobalFlags []string
	OwnFlags    []string
	Line        int
}

type CONLYFlagsStmt struct {
	GlobalFlags []string
	OwnFlags    []string
	Line        int
}

type LDFlagsStmt struct {
	Flags []string
	Line  int
}

type SrcDirStmt struct {
	Dir  string
	Line int
}

type GlobalSrcsStmt struct {
	Sources []string
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
	ToolPath       string
	Args           []string
	INFiles        []string
	OUTFiles       []string
	OUTNoAutoFiles []string
	StdoutFile     *string
	EnvPairs       []string
	CWD            *string
	OutputIncludes []string
	ToolPaths      []string
	Line           int
}

type RunPythonStmt struct {
	ScriptPath     string
	Args           []string
	INFiles        []string
	OUTFiles       []string
	OUTNoAutoFiles []string
	StdoutFile     *string
	EnvPairs       []string
	CWD            *string
	OutputIncludes []string
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
	Grammar        string
	Options        []string
	Visitor        bool
	Listener       bool
	OutputIncludes []string
	Line           int
}

type RunAntlr4CppSplitStmt struct {
	Lexer          string
	Parser         string
	Visitor        bool
	Listener       bool
	OutputIncludes []string
	Line           int
}

type RunAntlrStmt struct {
	Macro          string
	Args           []string
	INFiles        []string
	OUTFiles       []string
	OUTNoAutoFiles []string
	CWD            *string
	OutputIncludes []string
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
func (*RunPythonStmt) stmtMarker()                 {}
func (*ConfigureFileStmt) stmtMarker()             {}
func (*CreateBuildInfoStmt) stmtMarker()           {}
func (*RunAntlr4CppStmt) stmtMarker()              {}
func (*RunAntlr4CppSplitStmt) stmtMarker()         {}
func (*RunAntlrStmt) stmtMarker()                  {}
func (*ResourceStmt) stmtMarker()                  {}
func (*ResourceFilesStmt) stmtMarker()             {}

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

func (*ExprIdent) exprMarker()  {}
func (*ExprNot) exprMarker()    {}
func (*ExprAnd) exprMarker()    {}
func (*ExprOr) exprMarker()     {}
func (*ExprString) exprMarker() {}
func (*ExprInt) exprMarker()    {}
func (*ExprEq) exprMarker()     {}
func (*ExprLt) exprMarker()     {}

type ParseError struct {
	File    string
	Line    int
	Col     int
	Message string
}

func (e *ParseError) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Col, e.Message)
	}

	return fmt.Sprintf("%d:%d: %s", e.Line, e.Col, e.Message)
}

func ParseFile(fs FS, path string) (mf *MakeFile, err error) {
	exc := Try(func() {
		data := fs.ReadAbs(path)

		abs, absErr := filepath.Abs(path)

		if absErr != nil {
			abs = path
		}

		mf = Throw2(Parse(fs, abs, data))
	})

	if exc != nil {
		err = exc.AsError()
		mf = nil
	}

	return mf, err
}

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokString
	tokWord
	tokLParen
	tokRParen
	tokInt
	tokEq
	tokLt
	tokNotEq
)

type token struct {
	kind tokKind
	val  string
	line int
	col  int
}

type lexer struct {
	name string
	src  []byte
	pos  int
	line int
	col  int

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

func (l *lexer) throwParse(line, col int, format string, args ...any) {
	pe := &ParseError{
		File:    l.name,
		Line:    line,
		Col:     col,
		Message: fmt.Sprintf(format, args...),
	}

	New(pe).throw()
}

func (l *lexer) advance() byte {
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

func (l *lexer) skipTrivia() {
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
	case b == '"' || b == '\'':
		return l.readString(startLine, startCol, b)
	case b == '<':

		l.advance()
		return token{kind: tokLt, line: startLine, col: startCol}
	case b == '=' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '=':

		l.advance()
		l.advance()
		return token{kind: tokEq, line: startLine, col: startCol}
	case b == '!' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '=':

		l.advance()
		l.advance()
		return token{kind: tokNotEq, line: startLine, col: startCol}
	case b >= '0' && b <= '9':
		return l.readNumberOrWord(startLine, startCol)
	case isIdentStart(b):
		return l.readIdentOrWord(startLine, startCol)
	case isWordByte(b):
		return l.readWord(startLine, startCol)
	default:

		l.advance()
		l.throwParse(startLine, startCol, "unexpected character %q", b)
		return token{}
	}
}

func (l *lexer) readString(startLine, startCol int, quote byte) token {
	l.advance()

	var buf []byte

	for {
		if l.pos >= len(l.src) {
			l.throwParse(startLine, startCol, "unterminated string")
		}

		b := l.src[l.pos]

		if b == quote {
			l.advance()
			return token{kind: tokString, val: string(buf), line: startLine, col: startCol}
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

func (l *lexer) readIdentOrWord(startLine, startCol int) token {
	var buf []byte
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

	val := string(buf)
	kind := tokIdent

	if !pureIdent {
		kind = tokWord
	}

	return token{kind: kind, val: val, line: startLine, col: startCol}
}

func (l *lexer) readWord(startLine, startCol int) token {
	var buf []byte

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

	return token{kind: tokWord, val: string(buf), line: startLine, col: startCol}
}

func (l *lexer) readNumberOrWord(startLine, startCol int) token {
	start := l.pos

	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.advance()
	}

	if l.pos < len(l.src) && isWordByte(l.src[l.pos]) {
		for l.pos < len(l.src) && isWordByte(l.src[l.pos]) {
			l.advance()
		}

		return token{kind: tokWord, val: string(l.src[start:l.pos]), line: startLine, col: startCol}
	}

	return token{kind: tokInt, val: string(l.src[start:l.pos]), line: startLine, col: startCol}
}

func Parse(fs FS, name string, src []byte) (mf *MakeFile, err error) {
	exc := Try(func() {
		mf = parseInternal(fs, name, src)
	})

	if exc != nil {
		err = exc.AsError()
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

func parseInternalWithState(fs FS, name string, src []byte, stack []string, includes *includeState) *MakeFile {
	src = bytes.TrimPrefix(src, []byte{0xEF, 0xBB, 0xBF})
	p := &parser{lex: newLexer(name, src), name: name, includeStack: stack, includes: includes, fs: fs}
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

type stmtTerminator int

const (
	termTopLevel stmtTerminator = iota
	termIfBody
)

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

		if term == termIfBody && (tok.val == "ELSE" || tok.val == "ELSEIF" || tok.val == "ENDIF") {
			return stmts, tok
		}

		stmts = p.parseMacroInto(stmts, tok)
	}
}

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
	includeStack []string
	includes     *includeState
	fs           FS
}

func (p *parser) buildStmt(nameTok token, args []string) Stmt {
	switch nameTok.val {
	case "PROGRAM", "LIBRARY",

		"PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
		"PY3_PROGRAM_BIN", "PY2_PROGRAM", "PY3_PROGRAM",
		"YQL_UDF_YDB", "YQL_UDF_CONTRIB",
		"PROTO_LIBRARY",
		"DLL", "SO_PROGRAM", "DYNAMIC_LIBRARY",
		"PACKAGE", "UNION", "RESOURCES_LIBRARY",
		"UNITTEST_FOR":
		return &ModuleStmt{Name: nameTok.val, Args: args, Line: nameTok.line}
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
			value = strings.Join(args[1:], " ")
		}

		return &SetStmt{Name: args[0], NameEnv: internEnv(args[0]), Value: value, Line: nameTok.line}
	case "END":
		return &EndStmt{Line: nameTok.line}
	case "JOIN_SRCS":
		if len(args) == 0 {
			p.lex.throwParse(nameTok.line, nameTok.col, "JOIN_SRCS expects at least one argument (the output name)")
		}

		sources := append([]string(nil), args[1:]...)
		return &JoinSrcsStmt{OutputName: args[0], Sources: sources, Line: nameTok.line}
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

		varName := ""
		value := ""

		if len(args) >= 1 {
			varName = args[0]
		}

		if len(args) >= 2 {
			value = args[1]
		}

		return &DefaultVarStmt{VarName: varName, NameEnv: internEnv(varName), Value: value, Line: nameTok.line}
	case "CONFIGURE_FILE":

		if len(args) != 2 {
			p.lex.throwParse(nameTok.line, nameTok.col, "CONFIGURE_FILE expects exactly 2 arguments (src dst), got %d", len(args))
		}

		return &ConfigureFileStmt{Src: args[0], Dst: args[1], Line: nameTok.line}
	case "CREATE_BUILDINFO_FOR":

		if len(args) != 1 {
			p.lex.throwParse(nameTok.line, nameTok.col, "CREATE_BUILDINFO_FOR expects exactly 1 argument, got %d", len(args))
		}

		return &CreateBuildInfoStmt{OutputHeader: args[0], Line: nameTok.line}
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
		return &ResourceFilesStmt{Args: append([]string(nil), args...), Line: nameTok.line}
	default:
		return &UnknownStmt{Name: nameTok.val, Args: args, Line: nameTok.line}
	}
}

func parseRunAntlr4Cpp(args []string, line int) *RunAntlr4CppStmt {
	stmt := &RunAntlr4CppStmt{Grammar: args[0], Line: line}
	i := 1

	for i < len(args) {
		switch args[i] {
		case "VISITOR":
			stmt.Visitor = true
			i++
		case "NO_LISTENER", "LISTENER":

			if args[i] == "NO_LISTENER" {
				stmt.Listener = false
			} else {
				stmt.Listener = true
			}

			i++
		case "OUTPUT_INCLUDES":

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
				i++
			}
		case "IN", "OUT", "OUT_NOAUTO", "INDUCED_DEPS", "TOOL":

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

func parseRunAntlr4CppSplit(args []string, line int) *RunAntlr4CppSplitStmt {
	stmt := &RunAntlr4CppSplitStmt{Lexer: args[0], Parser: args[1], Line: line}

	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "VISITOR":
			stmt.Visitor = true
		case "LISTENER":
			stmt.Listener = true
		case "NO_LISTENER":

		case "OUTPUT_INCLUDES":

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
				i++
			}

			i--
		case "IN", "OUT", "OUT_NOAUTO", "INDUCED_DEPS", "TOOL":

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				i++
			}

			i--
		}
	}

	return stmt
}

func parseRunAntlr(args []string, nameTok token) *RunAntlrStmt {
	stmt := &RunAntlrStmt{Macro: nameTok.val, Line: nameTok.line}
	currentSection := "ARGS"

	for i := 0; i < len(args); i++ {
		tok := args[i]

		if runAntlrKeywords[tok] {
			currentSection = tok
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
		case "CWD":
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case "OUTPUT_INCLUDES", "INDUCED_DEPS", "TOOL", "ENV", "GRAMMAR_FILES", "GRAMMAR_CWD":
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		}
	}

	return stmt
}

func isRunAntlrKeyword(s string) bool {
	switch s {
	case "VISITOR", "LISTENER", "NO_LISTENER", "OUTPUT_INCLUDES",
		"IN", "IN_NOPARSE", "IN_DEPS", "OUT", "OUT_NOAUTO",
		"INDUCED_DEPS", "TOOL", "CWD", "ENV", "STDOUT",
		"STDOUT_NOAUTO", "GRAMMAR_FILES", "GRAMMAR_CWD":
		return true
	}

	return false
}

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
			if stmt.StdoutFile == nil {
				stmt.StdoutFile = &tok
			}
		case "ENV":
			stmt.EnvPairs = append(stmt.EnvPairs, tok)
		case "CWD":
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case "OUTPUT_INCLUDES", "INDUCED_DEPS":
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		case "TOOL":
			stmt.ToolPaths = append(stmt.ToolPaths, tok)
		}

		i++
	}

	return stmt
}

func parseRunPython(args []string, line int) *RunPythonStmt {
	stmt := &RunPythonStmt{ScriptPath: args[0], Line: line}
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
			if stmt.StdoutFile == nil {
				stmt.StdoutFile = &tok
			}
		case "ENV":
			stmt.EnvPairs = append(stmt.EnvPairs, tok)
		case "CWD":
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case "OUTPUT_INCLUDES", "INDUCED_DEPS", "TOOL":
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		}

		i++
	}

	return stmt
}

func parseResource(args []string, nameTok token) *ResourceStmt {
	rest := args

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

func splitGlobalModifier(args []string) (string, []string) {
	if len(args) > 0 && args[0] == "GLOBAL" {
		return "GLOBAL", append([]string(nil), args[1:]...)
	}

	return "", append([]string(nil), args...)
}

func unescapeFlag(s string) string {
	return strings.ReplaceAll(s, `\"`, `"`)
}

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

func splitAddInclPaths(args []string) (globalPaths, oneLevelPaths, ownPaths, cythonPaths, asmPaths, protoGlobalPaths, userGlobalPaths, allPaths []string) {
	for i := 0; i < len(args); i++ {
		if args[i] == "ONE_LEVEL" {
			i++

			if i < len(args) {
				oneLevelPaths = append(oneLevelPaths, args[i])
				ownPaths = append(ownPaths, args[i])
				userGlobalPaths = append(userGlobalPaths, args[i])
				allPaths = append(allPaths, args[i])
			}

			continue
		}

		if args[i] == "FOR" {
			if i+2 < len(args) && args[i+1] == "cython" {
				cythonPaths = append(cythonPaths, args[i+2])
				i += 2

				continue
			}

			if i+2 < len(args) && args[i+1] == "asm" {
				asmPaths = append(asmPaths, args[i+2])
				i += 2

				continue
			}

			i++
			continue
		}

		if args[i] == "GLOBAL" {
			i++

			if i < len(args) && args[i] == "FOR" {
				if i+2 < len(args) && args[i+1] == "cython" {
					cythonPaths = append(cythonPaths, args[i+2])
					i += 2

					continue
				}

				if i+2 < len(args) && args[i+1] == "asm" {
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
				if i+2 < len(args) && args[i+1] == "proto" {
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

		nested := p.parseIf(endTok)
		node.Else = []Stmt{nested}
		return node
	}

	p.lex.throwParse(endTok.line, endTok.col, "internal: unexpected IF terminator %q", endTok.val)

	return nil
}

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

func (p *parser) consumeEmptyMacroArgs(kwTok token) {
	_ = p.parseMacroArgs(kwTok)
}

type condParser struct {
	toks   []token
	pos    int
	parent *parser
	ifTok  token
}

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

		c.consume()
		right := c.parseAtom()
		c.rejectChainedCmp(t)
		return &ExprNot{Of: &ExprEq{Left: left, Right: right}}
	}

	return left
}

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

func (p *parser) expandInclude(into []Stmt, nameTok token) []Stmt {
	args := p.parseMacroArgs(nameTok)

	if len(args) != 1 {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE expects exactly 1 argument (the path), got %d", len(args))
	}

	rel := args[0]

	if strings.HasPrefix(rel, "${ARCADIA_ROOT}/") {
		suffix := strings.TrimPrefix(rel, "${ARCADIA_ROOT}/")
		rel = filepath.Join(p.fs.SourceRoot(), suffix)
	}

	dir := filepath.Dir(p.name)
	full := rel

	if !filepath.IsAbs(rel) {
		full = filepath.Join(dir, rel)
	}

	absTarget, absErr := filepath.Abs(full)

	if absErr != nil {
		absTarget = full
	}

	if _, skip := p.includes.once[absTarget]; skip {
		return into
	}

	chain := append(p.includeStack, p.name)

	for _, visited := range chain {
		if visited == absTarget {
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

	if present, _ := p.fs.ExistsAbs(absTarget); !present {
		return into
	}

	data := p.fs.ReadAbs(absTarget)

	included := parseInternalWithState(p.fs, absTarget, data, chain, p.includes)

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
