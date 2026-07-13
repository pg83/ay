package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	parseBufPool = sync.Pool{New: func() any { return new([]byte) }}
	parserPool   = sync.Pool{New: func() any { return &Parser{lex: &Lexer{}} }}
	astArgs      = newBumpAllocator[ANY]()
	astStmts     = newBumpAllocator[Stmt]()
	astFiles     = newBumpAllocator[MakeFile]()
	astUnknowns  = newBumpAllocator[UnknownStmt]()
	astModules   = newBumpAllocator[ModuleStmt]()
	astSrcs      = newBumpAllocator[SrcsStmt]()
	astPeerdirs  = newBumpAllocator[PeerdirStmt]()
	astIfs       = newBumpAllocator[IfStmt]()
	astSets      = newBumpAllocator[SetStmt]()
	astCFlags    = newBumpAllocator[CFlagsStmt]()
	astCXXFlags  = newBumpAllocator[CXXFlagsStmt]()
	astEnumSers  = newBumpAllocator[GenerateEnumSerializationStmt]()
	astConds     = newBumpAllocator[CondNode]()
	astEnds      = newBumpAllocator[EndStmt]()
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

var structCodegenOutputIncludes = anysOf(
	"util/generic/singleton.h",
	"util/generic/strbuf.h",
	"util/generic/vector.h",
	"util/generic/ptr.h",
	"util/generic/yexception.h",
	"kernel/struct_codegen/reflection/reflection.h",
	"kernel/struct_codegen/reflection/floats.h",
)

var structCodegenPeerdirs = anysOf(
	"kernel/struct_codegen/metadata",
	"kernel/struct_codegen/reflection",
)

var includeStatePool = sync.Pool{New: func() any {
	return &IncludeState{once: newIntSet(16), env: newEnvironment()}
}}

const (
	splitCodegenDefaultOutNum = 25
	splitCodegenStreamCount   = 5
	structCodegenTool         = "kernel/struct_codegen/codegen_tool"
)

const (
	ckIdent CondKind = iota
	ckString
	ckInt
	ckNot
	ckAnd
	ckOr
	ckEq
	ckLt
	ckStartsWith
	ckMatches
	ckVersionCmp
	ckDefined
)

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

const (
	termTopLevel StmtTerminator = iota
	termIfBody
)

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
	Name   TOK
	Schema bool
	Args   []ANY
	Line   int
}

type PeerdirStmt struct {
	Paths []ANY
	Line  int
}

type DeclareResourceStmt struct {
	Macro TOK
	Args  []ANY
	Line  int
}

type SrcsStmt struct {
	Sources []ANY
	Line    int
}

type SetStmt struct {
	Name    string
	NameEnv ENV
	Value   string
	Line    int
}

type EndStmt struct {
	Line int
}

type UnknownStmt struct {
	Name TOK
	Raw  STR
	Args []ANY
	Line int
}

type IfStmt struct {
	Cond []CondNode
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
	Sources    []ANY
	Line       int
	Seq        int
}

type AddInclStmt struct {
	GlobalPaths      []ANY
	OneLevelPaths    []ANY
	OwnPaths         []ANY
	CythonPaths      []ANY
	AsmPaths         []ANY
	ProtoGlobalPaths []ANY
	UserGlobalPaths  []ANY
	AllPaths         []ANY
	Line             int
}

type CFlagsStmt struct {
	GlobalFlags []ANY
	OwnFlags    []ANY
	Line        int
}

type CXXFlagsStmt struct {
	GlobalFlags []ANY
	OwnFlags    []ANY
	Line        int
}

type CONLYFlagsStmt struct {
	GlobalFlags []ANY
	OwnFlags    []ANY
	Line        int
}

type LDFlagsStmt struct {
	Flags []ANY
	Line  int
}

type SrcDirStmt struct {
	Dirs []ANY
	Line int
}

type GlobalSrcsStmt struct {
	Sources []ANY
	Line    int
}

type GenerateEnumSerializationStmt struct {
	Header  string
	Variant string
	Line    int
	DeclSeq int
}

type DefaultVarStmt struct {
	VarName string
	NameEnv ENV
	Value   string
	Line    int
}

type RunProgramStmt struct {
	ToolPath       ANY
	Args           []ANY
	INFiles        []ANY
	OUTFiles       []ANY
	OUTNoAutoFiles []ANY
	StdoutFile     *ANY
	StdoutNoAuto   bool
	EnvPairs       []ANY
	CWD            *ANY
	OutputIncludes []ANY
	ToolPaths      []ANY
	Line           int
	DeclSeq        int
}

type SplitCodegenStmt struct {
	ToolPath       ANY
	Prefix         ANY
	Opts           []ANY
	OutNum         int
	OutputIncludes []ANY
	Line           int
}

type BaseCodegenStmt struct {
	ToolPath       ANY
	Prefix         ANY
	Opts           []ANY
	OutputIncludes []ANY
	Peerdirs       []ANY
	Line           int
}

type FromSandboxStmt struct {
	ResourceId     ANY
	OUTFiles       []ANY
	OUTNoAutoFiles []ANY
	OutputIncludes []ANY
	Renames        []ANY
	File           bool
	Executable     bool
	Prefix         string
	Line           int
}

type RunPythonStmt struct {
	Lua            bool
	ScriptPath     ANY
	Args           []ANY
	INFiles        []ANY
	OUTFiles       []ANY
	OUTNoAutoFiles []ANY
	StdoutFile     *ANY
	StdoutNoAuto   bool
	EnvPairs       []ANY
	CWD            *ANY
	OutputIncludes []ANY
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
	Grammar        ANY
	Options        []ANY
	Visitor        bool
	Listener       bool
	OutputIncludes []ANY
	Line           int
}

type RunAntlr4CppSplitStmt struct {
	Lexer          ANY
	Parser         ANY
	Visitor        bool
	Listener       bool
	OutputIncludes []ANY
	Line           int
}

type RunAntlrStmt struct {
	Macro          string
	Args           []ANY
	INFiles        []ANY
	OUTFiles       []ANY
	OUTNoAutoFiles []ANY
	CWD            *ANY
	OutputIncludes []ANY
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
	Args []ANY
	Line int
}

type AllResourceFilesStmt struct {
	Args     []ANY
	FromDirs bool
	Line     int
}

type CondKind uint8

type CondNode struct {
	Kind CondKind
	Name string
	Env  ENV
	Ival int
	L, R int32
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

func (e *ParseError) Error() string {
	return e.error()
}

func parseFile(fs FS, rel string) (mf *MakeFile, err error) {
	exc := try(func() {
		bp := readForParse(fs, cleanRel(rel))

		defer releaseParseBuf(bp)

		mf = throw2(parse(fs, cleanRel(rel), *bp))
	})

	if exc != nil {
		err = exc.asError()
		mf = nil
	}

	return mf, err
}

func readForParse(fs FS, rel string) *[]byte {
	bp := parseBufPool.Get().(*[]byte)

	*bp = (*bp)[:0]

	for _, chunk := range fs.read(rel) {
		*bp = append(*bp, chunk...)
	}

	return bp
}

func releaseParseBuf(bp *[]byte) {
	parseBufPool.Put(bp)
}

type TokKind int

type Token struct {
	kind TokKind
	val  string
	line int
	col  int
}

type Lexer struct {
	name     string
	src      []byte
	pos      int
	line     int
	col      int
	prevByte byte
	tokBuf   []byte
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
	case '_', '-', '.', '/', '+', ':', '=', '*', '?', '$', '%', '~', ',', '!', '{', '}', '#', '[', ']',

		'\\':
		return true
	}

	return b >= 0x80
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

func (l *Lexer) appendQuotedSegment(buf []byte, quote byte, startLine, startCol int) []byte {
	l.advance()

	for {
		if l.pos >= len(l.src) {
			l.throwParse(startLine, startCol, "unterminated string")
		}

		b := l.src[l.pos]

		if b == quote {
			l.advance()

			return buf
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
	start := l.pos
	pos := start
	pureIdent := true
	complex := false

	for pos < len(l.src) {
		b := l.src[pos]

		if (b == '\\' && pos+1 < len(l.src) && l.src[pos+1] == '"') || b == '"' || b == '\'' {
			complex = true

			break
		}

		if isIdentCont(b) {
			pos++

			continue
		}

		if isWordByte(b) || b == '@' && pos > start {
			pureIdent = false
			pos++

			continue
		}

		break
	}

	if !complex {
		l.pos = pos
		l.col += pos - start
		l.prevByte = l.src[pos-1]

		kind := tokIdent

		if !pureIdent {
			kind = tokWord
		}

		return Token{kind: kind, val: internBytes(l.src[start:pos]).string(), line: startLine, col: startCol}
	}

	buf := l.tokBuf[:0]
	pureIdent = true

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

		if (b == '"' || b == '\'') && len(buf) > 0 {
			pureIdent = false
			buf = l.appendQuotedSegment(buf, b, startLine, startCol)

			continue
		}

		break
	}

	l.tokBuf = buf

	val := internBytes(buf).string()
	kind := tokIdent

	if !pureIdent {
		kind = tokWord
	}

	return Token{kind: kind, val: val, line: startLine, col: startCol}
}

func (l *Lexer) readWord(startLine, startCol int) Token {
	start := l.pos
	pos := start
	complex := false

	for pos < len(l.src) {
		b := l.src[pos]

		if (b == '\\' && pos+1 < len(l.src) && l.src[pos+1] == '"') || (b == '"' || b == '\'') && pos > start {
			complex = true

			break
		}

		if !isWordByte(b) && !(b == '@' && pos > start) {
			break
		}

		pos++
	}

	if !complex {
		l.pos = pos
		l.col += pos - start
		l.prevByte = l.src[pos-1]

		return Token{kind: tokWord, val: internBytes(l.src[start:pos]).string(), line: startLine, col: startCol}
	}

	buf := l.tokBuf[:0]

	for l.pos < len(l.src) {
		b := l.src[l.pos]

		if b == '\\' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '"' {
			buf = append(buf, l.advance())
			buf = append(buf, l.advance())

			continue
		}

		if (b == '"' || b == '\'') && len(buf) > 0 {
			buf = l.appendQuotedSegment(buf, b, startLine, startCol)

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
	pos := start

	for pos < len(l.src) && l.src[pos] >= '0' && l.src[pos] <= '9' {
		pos++
	}

	kind := tokInt

	if pos < len(l.src) && isWordByte(l.src[pos]) {
		kind = tokWord

		for pos < len(l.src) && isWordByte(l.src[pos]) {
			pos++
		}
	}

	l.pos = pos
	l.col += pos - start
	l.prevByte = l.src[pos-1]

	return Token{kind: kind, val: internBytes(l.src[start:pos]).string(), line: startLine, col: startCol}
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
	st := acquireIncludeState()

	defer releaseIncludeState(st)

	st.env.setString(envMODDIR, pathDir(name))

	return parseInternalWithState(fs, name, src, stack, st)
}

func parseInternalWithState(fs FS, name string, src []byte, stack []string, includes *IncludeState) *MakeFile {
	src = bytes.TrimPrefix(src, []byte{0xEF, 0xBB, 0xBF})

	p := parserPool.Get().(*Parser)
	lex := p.lex
	tokBuf := lex.tokBuf
	condScratch := p.condScratch
	condTokBuf := p.condTokBuf

	defer parserPool.Put(p)

	*lex = Lexer{name: name, src: src, line: 1, col: 1, tokBuf: tokBuf[:0]}

	argScratch := p.argScratch
	stmtScratch := scrubCap(p.stmtScratch)

	*p = Parser{lex: lex, name: name, includeStack: stack, includes: includes, fs: fs, condScratch: condScratch[:0], condTokBuf: condTokBuf[:0], argScratch: argScratch[:0], stmtScratch: stmtScratch}

	mf := astOne(astFiles, MakeFile{Path: name})

	mf.Stmts, _ = p.parseStmts(termTopLevel)

	return mf
}

type IncludeState struct {
	once *IntSet
	env  Environment
}

func acquireIncludeState() *IncludeState {
	st := includeStatePool.Get().(*IncludeState)

	st.once.reset()
	st.env.s.val = st.env.s.val[:0]
	st.env.s.kind = st.env.s.kind[:0]

	return st
}

func releaseIncludeState(st *IncludeState) {
	includeStatePool.Put(st)
}

type StmtTerminator int

func (p *Parser) takeStmts(mark int) []Stmt {
	out := astStmts.list(p.stmtScratch[mark:]...)

	p.stmtScratch = p.stmtScratch[:mark]

	return out
}

func (p *Parser) parseStmts(term StmtTerminator) (stmts []Stmt, endTok Token) {
	mark := len(p.stmtScratch)

	for {
		tok := p.lex.next()

		if tok.kind == tokEOF {
			if term != termTopLevel {
				p.lex.throwParse(tok.line, tok.col, "unexpected end of file inside IF block (missing ENDIF)")
			}

			return p.takeStmts(mark), tok
		}

		if tok.kind != tokIdent && !(tok.kind == tokWord && isIdentShapedName(tok.val)) {
			p.lex.throwParse(tok.line, tok.col, "expected macro name, got %s", describeToken(tok))
		}

		if term == termIfBody && (tok.val == "ELSE" || tok.val == "ELSEIF" || tok.val == "ENDIF") {
			return p.takeStmts(mark), tok
		}

		p.parseMacroInto(tok)
	}
}

func (p *Parser) parseMacroInto(nameTok Token) {
	switch nameTok.val {
	case "IF":
		st := p.parseIf(nameTok)

		p.stmtScratch = append(p.stmtScratch, st)

		return
	case "INCLUDE":
		p.expandInclude(nameTok)

		return
	case "INCLUDE_ONCE":
		p.applyIncludeOnce(nameTok)

		return
	}

	args := p.parseMacroArgs(nameTok)

	if nameTok.val == "CPP_ENUMS_SERIALIZATION" {
		for i := 0; i < len(args); i++ {
			if args[i].string() == "NAMESPACE" {
				i++

				continue
			}

			p.stmtScratch = append(p.stmtScratch, astOne(astEnumSers, GenerateEnumSerializationStmt{Header: args[i].string(), Variant: "with_header", Line: nameTok.line}))
		}

		return
	}

	stmt := p.buildStmt(nameTok, args)

	if set, ok := stmt.(*SetStmt); ok {
		p.includes.env.setFromString(set.NameEnv, expandScalarVarRef(set.Value, p.includes.env))
	}

	if def, ok := stmt.(*DefaultVarStmt); ok && !p.includes.env.hasBindingID(def.NameEnv) {
		p.includes.env.setFromString(def.NameEnv, expandScalarVarRef(def.Value, p.includes.env))
	}

	p.stmtScratch = append(p.stmtScratch, stmt)
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

	p.includes.once.put(uint64(internStr(p.name).strID()), true)
}

func (p *Parser) parseMacroArgs(nameTok Token) []ANY {
	scratch := p.argScratch[:0]

	defer func() { p.argScratch = scratch[:0] }()

	lp := p.lex.next()

	if lp.kind != tokLParen {
		p.lex.throwParse(lp.line, lp.col, "expected '(' after macro name %q, got %s", nameTok.val, describeToken(lp))
	}

	for {
		tok := p.lex.next()

		switch tok.kind {
		case tokRParen:
			if len(scratch) == 0 {
				return nil
			}

			return astArgs.list(scratch...)
		case tokEOF:
			p.lex.throwParse(nameTok.line, nameTok.col, "unterminated macro call %q (missing ')')", nameTok.val)
		case tokIdent, tokWord, tokString, tokInt:

			scratch = append(scratch, internStr(tok.val).any())
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
	condScratch  []CondNode
	condTokBuf   []Token
	argScratch   []ANY
	stmtScratch  []Stmt
}

func (p *Parser) buildStmt(nameTok Token, args []ANY) Stmt {
	return buildStmtFor(nameTok.val, args, nameTok.line, func(format string, a ...any) {
		p.lex.throwParse(nameTok.line, nameTok.col, format, a...)
	})
}

func buildStmtFor(name string, args []ANY, line int, fail func(format string, a ...any)) Stmt {
	switch name {
	case "PROGRAM", "LIBRARY",

		"PY23_NATIVE_LIBRARY", "PY3_LIBRARY", "PY23_LIBRARY", "PY2_LIBRARY",
		"PY3_PROGRAM_BIN", "PY2_PROGRAM", "PY3_PROGRAM",
		"YQL_UDF_YDB", "YQL_UDF_CONTRIB",
		"PROTO_LIBRARY",
		"PROTO_DESCRIPTIONS",
		"DLL", "DLL_TOOL", "SO_PROGRAM", "DYNAMIC_LIBRARY",
		"PACKAGE", "UNION", "RESOURCES_LIBRARY",
		"PREBUILT_PROGRAM",
		"FBS_LIBRARY",
		"GO_LIBRARY", "GO_PROGRAM",
		"UNITTEST_FOR":
		return astOne(astModules, ModuleStmt{Name: internTok(name), Args: args, Line: line})
	case "PROTO_SCHEMA":

		return astOne(astModules, ModuleStmt{Name: tokProtoLibrary, Schema: true, Args: args, Line: line})
	case "DECLARE_EXTERNAL_RESOURCE",
		"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE",
		"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON":
		return &DeclareResourceStmt{Macro: internTok(name), Args: args, Line: line}
	case "PEERDIR":
		return astOne(astPeerdirs, PeerdirStmt{Paths: args, Line: line})
	case "SRCS":
		return astOne(astSrcs, SrcsStmt{Sources: args, Line: line})
	case "SET":
		if len(args) == 0 {
			fail("SET expects at least 1 argument (name), got %d", len(args))
		}

		value := ""

		if len(args) > 1 {
			value = strings.Join(anyStrs(args[1:]), " ")
		}

		return astOne(astSets, SetStmt{Name: args[0].string(), NameEnv: internEnv(args[0].string()), Value: internStr(value).string(), Line: line})
	case "END":
		return astOne(astEnds, EndStmt{Line: line})
	case "JOIN_SRCS":
		if len(args) == 0 {
			fail("JOIN_SRCS expects at least one argument (the output name)")
		}

		sources := astArgs.list(args[1:]...)

		return &JoinSrcsStmt{OutputName: args[0].string(), Sources: sources, Line: line}
	case "ADDINCL":
		globalPaths, oneLevelPaths, ownPaths, cythonPaths, asmPaths, protoGlobalPaths, userGlobalPaths, allPaths := splitAddInclPaths(args)

		return &AddInclStmt{GlobalPaths: globalPaths, OneLevelPaths: oneLevelPaths, OwnPaths: ownPaths, CythonPaths: cythonPaths, AsmPaths: asmPaths, ProtoGlobalPaths: protoGlobalPaths, UserGlobalPaths: userGlobalPaths, AllPaths: allPaths, Line: line}
	case "CFLAGS":
		globalFlags, ownFlags := splitFlagsByGlobal(args)

		return astOne(astCFlags, CFlagsStmt{GlobalFlags: globalFlags, OwnFlags: ownFlags, Line: line})
	case "CXXFLAGS":
		globalFlags, ownFlags := splitFlagsByGlobal(args)

		return astOne(astCXXFlags, CXXFlagsStmt{GlobalFlags: globalFlags, OwnFlags: ownFlags, Line: line})
	case "CONLYFLAGS":
		globalFlags, ownFlags := splitFlagsByGlobal(args)

		return &CONLYFlagsStmt{GlobalFlags: globalFlags, OwnFlags: ownFlags, Line: line}
	case "LDFLAGS":
		return &LDFlagsStmt{Flags: append([]ANY(nil), args...), Line: line}
	case "SRCDIR":
		if len(args) == 0 {
			fail("SRCDIR expects at least 1 argument, got %d", len(args))
		}

		return &SrcDirStmt{Dirs: append([]ANY(nil), args...), Line: line}
	case "GLOBAL_SRCS":
		return &GlobalSrcsStmt{Sources: append([]ANY(nil), args...), Line: line}
	case "GENERATE_ENUM_SERIALIZATION":
		if len(args) != 1 {
			fail("GENERATE_ENUM_SERIALIZATION expects exactly 1 argument (header path), got %d", len(args))
		}

		return astOne(astEnumSers, GenerateEnumSerializationStmt{Header: args[0].string(), Variant: "plain", Line: line})
	case "GENERATE_ENUM_SERIALIZATION_WITH_HEADER":
		if len(args) != 1 {
			fail("GENERATE_ENUM_SERIALIZATION_WITH_HEADER expects exactly 1 argument (header path), got %d", len(args))
		}

		return &GenerateEnumSerializationStmt{Header: args[0].string(), Variant: "with_header", Line: line}
	case "GENERATE_ENUM_SERIALIZATION_NOUTF":
		if len(args) != 1 {
			fail("GENERATE_ENUM_SERIALIZATION_NOUTF expects exactly 1 argument (header path), got %d", len(args))
		}

		return &GenerateEnumSerializationStmt{Header: args[0].string(), Variant: "noutf", Line: line}
	case "DEFAULT":

		varName := ""
		value := ""

		if len(args) >= 1 {
			varName = args[0].string()
		}

		if len(args) >= 2 {
			value = args[1].string()
		}

		return &DefaultVarStmt{VarName: varName, NameEnv: internEnv(varName), Value: value, Line: line}
	case "CONFIGURE_FILE":

		if len(args) != 2 {
			fail("CONFIGURE_FILE expects exactly 2 arguments (src dst), got %d", len(args))
		}

		return &ConfigureFileStmt{Src: args[0].string(), Dst: args[1].string(), Line: line}
	case "CREATE_BUILDINFO_FOR":

		if len(args) != 1 {
			fail("CREATE_BUILDINFO_FOR expects exactly 1 argument, got %d", len(args))
		}

		return &CreateBuildInfoStmt{OutputHeader: args[0].string(), Line: line}
	case "RUN_ANTLR4_CPP":

		if len(args) == 0 {
			fail("RUN_ANTLR4_CPP expects at least 1 argument (grammar)")
		}

		return parseRunAntlr4Cpp(args, line)
	case "RUN_ANTLR4_CPP_SPLIT":

		if len(args) < 2 {
			fail("RUN_ANTLR4_CPP_SPLIT expects at least 2 arguments (lexer parser)")
		}

		return parseRunAntlr4CppSplit(args, line)
	case "RUN_ANTLR", "RUN_ANTLR4":
		if len(args) == 0 {
			fail("%s expects at least 1 argument", name)
		}

		return parseRunAntlr(args, Token{val: name, line: line})
	case "RUN_PROGRAM", "RUN_PY3_PROGRAM":

		if len(args) == 0 {
			fail("%s expects at least 1 argument (tool path)", name)
		}

		return parseRunProgram(args, line)
	case "RUN_PYTHON3":
		if len(args) == 0 {
			fail("RUN_PYTHON3 expects at least 1 argument (script path)")
		}

		return parseRunPython(args, line)
	case "RUN_LUA":
		if len(args) == 0 {
			fail("RUN_LUA expects at least 1 argument (script path)")
		}

		stmt := parseRunPython(args, line)

		stmt.Lua = true

		return stmt
	case "SPLIT_CODEGEN":
		if len(args) < 2 {
			fail("SPLIT_CODEGEN expects at least 2 arguments (tool prefix), got %d", len(args))
		}

		return parseSplitCodegen(args, line)
	case "BASE_CODEGEN":
		if len(args) < 2 {
			fail("BASE_CODEGEN expects at least 2 arguments (tool prefix), got %d", len(args))
		}

		return parseBaseCodegen(args, line)
	case "STRUCT_CODEGEN":
		if len(args) != 1 {
			fail("STRUCT_CODEGEN expects exactly 1 argument (prefix), got %d", len(args))
		}

		return parseStructCodegen(args[0], line)
	case "FROM_SANDBOX":

		if len(args) == 0 {
			fail("FROM_SANDBOX expects at least 1 argument (resource id)")
		}

		return parseFromSandbox(args, line)
	case "RESOURCE":

		return parseResource(args, Token{val: name, line: line})
	case "RESOURCE_FILES":
		return &ResourceFilesStmt{Args: astArgs.list(args...), Line: line}
	case "ALL_RESOURCE_FILES":
		if len(args) == 0 {
			fail("ALL_RESOURCE_FILES expects at least 1 argument (the extension)")
		}

		return &AllResourceFilesStmt{Args: astArgs.list(args...), Line: line}
	case "ALL_RESOURCE_FILES_FROM_DIRS":
		return &AllResourceFilesStmt{Args: astArgs.list(args...), FromDirs: true, Line: line}
	default:
		return astOne(astUnknowns, UnknownStmt{Name: internTokMaybe(name), Raw: internStr(name), Args: args, Line: line})
	}
}

func parseRunAntlr4Cpp(args []ANY, line int) *RunAntlr4CppStmt {
	stmt := &RunAntlr4CppStmt{Grammar: args[0], Line: line}
	i := 1

	for i < len(args) {
		switch args[i] {
		case kwVISITOR.any():
			stmt.Visitor = true
			i++
		case kwNO_LISTENER.any(), kwLISTENER.any():

			if args[i] == kwNO_LISTENER.any() {
				stmt.Listener = false
			} else {
				stmt.Listener = true
			}

			i++
		case kwOUTPUT_INCLUDES.any():

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
				i++
			}
		case kwIN.any(), kwOUT.any(), kwOUT_NOAUTO.any(), kwINDUCED_DEPS.any(), kwTOOL.any():

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

func parseRunAntlr4CppSplit(args []ANY, line int) *RunAntlr4CppSplitStmt {
	stmt := &RunAntlr4CppSplitStmt{Lexer: args[0], Parser: args[1], Line: line}

	for i := 2; i < len(args); i++ {
		switch args[i] {
		case kwVISITOR.any():
			stmt.Visitor = true
		case kwLISTENER.any():
			stmt.Listener = true
		case kwNO_LISTENER.any():

		case kwOUTPUT_INCLUDES.any():

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
				i++
			}

			i--
		case kwIN.any(), kwOUT.any(), kwOUT_NOAUTO.any(), kwINDUCED_DEPS.any(), kwTOOL.any():

			i++

			for i < len(args) && !isRunAntlrKeyword(args[i]) {
				i++
			}

			i--
		}
	}

	return stmt
}

func parseRunAntlr(args []ANY, nameTok Token) *RunAntlrStmt {
	stmt := &RunAntlrStmt{Macro: nameTok.val, Line: nameTok.line}
	currentSection := kwARGS.any()

	for i := 0; i < len(args); i++ {
		tok := args[i]

		if runAntlrKeywords.has(uint32(tok.str())) {
			currentSection = tok

			continue
		}

		switch currentSection {
		case kwARGS.any():
			stmt.Args = append(stmt.Args, tok)
		case kwIN.any(), kwIN_NOPARSE.any(), kwIN_DEPS.any():
			stmt.INFiles = append(stmt.INFiles, tok)
		case kwOUT.any():
			stmt.OUTFiles = append(stmt.OUTFiles, tok)
		case kwOUT_NOAUTO.any():
			stmt.OUTNoAutoFiles = append(stmt.OUTNoAutoFiles, tok)
		case kwCWD.any():
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case kwOUTPUT_INCLUDES.any(), kwINDUCED_DEPS.any(), kwTOOL.any(), kwENV.any(), kwGRAMMAR_FILES.any(), kwGRAMMAR_CWD.any():
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		}
	}

	return stmt
}

func isRunAntlrKeyword(s ANY) bool {
	switch s {
	case kwVISITOR.any(), kwLISTENER.any(), kwNO_LISTENER.any(), kwOUTPUT_INCLUDES.any(),
		kwIN.any(), kwIN_NOPARSE.any(), kwIN_DEPS.any(), kwOUT.any(), kwOUT_NOAUTO.any(),
		kwINDUCED_DEPS.any(), kwTOOL.any(), kwCWD.any(), kwENV.any(), kwSTDOUT.any(),
		kwSTDOUT_NOAUTO.any(), kwGRAMMAR_FILES.any(), kwGRAMMAR_CWD.any():
		return true
	}

	return false
}

func parseFromSandbox(args []ANY, line int) *FromSandboxStmt {
	stmt := &FromSandboxStmt{Line: line, Prefix: "."}
	section := ""

	for i := 0; i < len(args); i++ {
		switch args[i].string() {
		case "FILE":
			stmt.File = true
			section = ""
		case "EXECUTABLE":
			stmt.Executable = true
			section = ""
		case "AUTOUPDATED", "PREFIX", "SBR":
			section = args[i].string()
		case "OUT":
			section = "OUT"
		case "OUT_NOAUTO":
			section = "OUT_NOAUTO"
		case "OUTPUT_INCLUDES":
			section = "OUTPUT_INCLUDES"
		case "RENAME":
			section = "RENAME"
		case "INDUCED_DEPS":
			section = "SKIP"
		default:
			switch section {
			case "":
				if stmt.ResourceId == 0 {
					stmt.ResourceId = args[i]
				}
			case "PREFIX":
				stmt.Prefix = args[i].string()
				section = ""
			case "AUTOUPDATED", "SBR":
				section = ""
			case "OUT":
				stmt.OUTFiles = append(stmt.OUTFiles, args[i])
			case "OUT_NOAUTO":
				stmt.OUTNoAutoFiles = append(stmt.OUTNoAutoFiles, args[i])
			case "OUTPUT_INCLUDES":
				stmt.OutputIncludes = append(stmt.OutputIncludes, args[i])
			case "RENAME":
				stmt.Renames = append(stmt.Renames, args[i])
			case "SKIP":
			}
		}
	}

	return stmt
}

func parseRunProgram(args []ANY, line int) *RunProgramStmt {
	stmt := &RunProgramStmt{ToolPath: args[0], Line: line}
	i := 1
	currentSection := kwARGS.any()

	for i < len(args) {
		tok := args[i]

		if runProgramKeywords.has(uint32(tok.str())) {
			currentSection = tok
			i++

			continue
		}

		switch currentSection {
		case kwARGS.any():
			stmt.Args = append(stmt.Args, tok)
		case kwIN.any(), kwIN_NOPARSE.any(), kwIN_DEPS.any():
			stmt.INFiles = append(stmt.INFiles, tok)
		case kwOUT.any():
			stmt.OUTFiles = append(stmt.OUTFiles, tok)
		case kwOUT_NOAUTO.any():
			stmt.OUTNoAutoFiles = append(stmt.OUTNoAutoFiles, tok)
		case kwSTDOUT.any(), kwSTDOUT_NOAUTO.any():
			if stmt.StdoutFile == nil {
				stmt.StdoutFile = &tok
				stmt.StdoutNoAuto = currentSection == kwSTDOUT_NOAUTO.any()
			}
		case kwENV.any():
			stmt.EnvPairs = append(stmt.EnvPairs, tok)
		case kwCWD.any():
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case kwOUTPUT_INCLUDES.any(), kwINDUCED_DEPS.any():
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		case kwTOOL.any():
			stmt.ToolPaths = append(stmt.ToolPaths, tok)
		}

		i++
	}

	return stmt
}

func parseRunPython(args []ANY, line int) *RunPythonStmt {
	stmt := &RunPythonStmt{ScriptPath: args[0], Line: line}
	i := 1
	currentSection := kwARGS.any()

	for i < len(args) {
		tok := args[i]

		if runProgramKeywords.has(uint32(tok.str())) {
			currentSection = tok
			i++

			continue
		}

		switch currentSection {
		case kwARGS.any():
			stmt.Args = append(stmt.Args, tok)
		case kwIN.any(), kwIN_NOPARSE.any(), kwIN_DEPS.any():
			stmt.INFiles = append(stmt.INFiles, tok)
		case kwOUT.any():
			stmt.OUTFiles = append(stmt.OUTFiles, tok)
		case kwOUT_NOAUTO.any():
			stmt.OUTNoAutoFiles = append(stmt.OUTNoAutoFiles, tok)
		case kwSTDOUT.any(), kwSTDOUT_NOAUTO.any():
			if stmt.StdoutFile == nil {
				stmt.StdoutFile = &tok
				stmt.StdoutNoAuto = currentSection == kwSTDOUT_NOAUTO.any()
			}
		case kwENV.any():
			stmt.EnvPairs = append(stmt.EnvPairs, tok)
		case kwCWD.any():
			if stmt.CWD == nil {
				stmt.CWD = &tok
			}
		case kwOUTPUT_INCLUDES.any(), kwINDUCED_DEPS.any(), kwTOOL.any():
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		}

		i++
	}

	return stmt
}

func parseSplitCodegen(args []ANY, line int) *SplitCodegenStmt {
	stmt := &SplitCodegenStmt{OutNum: splitCodegenDefaultOutNum, Line: line}

	var positional []ANY

	section := STR(0)

	for _, tok := range args {
		switch tok {
		case kwSplitOutNum.any():
			section = kwSplitOutNum

			continue
		case kwSplitOutputIncludes.any():
			section = kwSplitOutputIncludes

			continue
		}

		switch section {
		case kwSplitOutNum:
			if n, err := strconv.Atoi(tok.string()); err == nil {
				stmt.OutNum = n
			}

			section = 0
		case kwSplitOutputIncludes:
			stmt.OutputIncludes = append(stmt.OutputIncludes, tok)
		default:
			positional = append(positional, tok)
		}
	}

	if len(positional) > 0 {
		stmt.ToolPath = positional[0]
	}

	if len(positional) > 1 {
		stmt.Prefix = positional[1]
	}

	if len(positional) > 2 {
		stmt.Opts = positional[2:]
	}

	return stmt
}

func parseBaseCodegen(args []ANY, line int) *BaseCodegenStmt {
	stmt := &BaseCodegenStmt{ToolPath: args[0], Prefix: args[1], Line: line}

	if len(args) > 2 {
		stmt.Opts = args[2:]
	}

	return stmt
}

func parseStructCodegen(prefix ANY, line int) *BaseCodegenStmt {
	return &BaseCodegenStmt{
		ToolPath:       internStr(structCodegenTool).any(),
		Prefix:         prefix,
		OutputIncludes: structCodegenOutputIncludes,
		Peerdirs:       structCodegenPeerdirs,
		Line:           line,
	}
}

func parseResource(args []ANY, nameTok Token) *ResourceStmt {
	rest := args

	for i := 0; i < 2 && len(rest) > 0; i++ {
		if rest[0] == kwDONT_PARSE.any() || rest[0] == kwDONT_COMPRESS.any() {
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

func splitFlagsByGlobal(args []ANY) (globalFlags, ownFlags []ANY) {
	ng, no := 0, 0

	for i := 0; i < len(args); i++ {
		if args[i] == kwGLOBAL.any() {
			i++

			if i < len(args) {
				ng++
			}
		} else {
			no++
		}
	}

	gw := astArgs.alloc(ng)[:0]

	for i := 0; i < len(args); i++ {
		if args[i] == kwGLOBAL.any() {
			i++

			if i < len(args) {
				gw = append(gw, internStr(unescapeFlag(args[i].string())).any())
			}
		}
	}

	astArgs.commit(ng)

	ow := astArgs.alloc(no)[:0]

	for i := 0; i < len(args); i++ {
		if args[i] == kwGLOBAL.any() {
			i++
		} else {
			ow = append(ow, internStr(unescapeFlag(args[i].string())).any())
		}
	}

	astArgs.commit(no)

	return gw[:ng:ng], ow[:no:no]
}

func splitAddInclPaths(args []ANY) (globalPaths, oneLevelPaths, ownPaths, cythonPaths, asmPaths, protoGlobalPaths, userGlobalPaths, allPaths []ANY) {
	for i := 0; i < len(args); i++ {
		if args[i] == kwONE_LEVEL.any() {
			i++

			if i < len(args) {
				oneLevelPaths = append(oneLevelPaths, args[i])
				ownPaths = append(ownPaths, args[i])
				userGlobalPaths = append(userGlobalPaths, args[i])
				allPaths = append(allPaths, args[i])
			}

			continue
		}

		if args[i] == kwFOR.any() {
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

		if args[i] == kwGLOBAL.any() {
			i++

			if i < len(args) && args[i] == kwFOR.any() {
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

	return astArgs.list(globalPaths...), astArgs.list(oneLevelPaths...), astArgs.list(ownPaths...), astArgs.list(cythonPaths...),
		astArgs.list(asmPaths...), astArgs.list(protoGlobalPaths...), astArgs.list(userGlobalPaths...), astArgs.list(allPaths...)
}

func (p *Parser) parseIf(ifTok Token) *IfStmt {
	condToks := p.readCondTokens(ifTok)

	if len(condToks) == 0 {
		p.lex.throwParse(ifTok.line, ifTok.col, "IF requires a condition expression")
	}

	cond := parseCondExpr(p, ifTok, condToks)
	thenBody, endTok := p.parseStmts(termIfBody)
	node := astOne(astIfs, IfStmt{Cond: cond, Then: thenBody, Line: ifTok.line})

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

		node.Else = astStmts.list(nested)

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

	out := p.condTokBuf[:0]
	depth := 1

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
				p.condTokBuf = out

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

func parseCondExpr(parent *Parser, ifTok Token, toks []Token) []CondNode {
	parent.condScratch = parent.condScratch[:0]

	cp := CondParser{toks: toks, parent: parent, ifTok: ifTok}

	cp.parseOr()

	if cp.pos != len(cp.toks) {
		t := cp.toks[cp.pos]

		parent.lex.throwParse(t.line, t.col, "unexpected %s in IF condition", describeToken(t))
	}

	return astConds.list(parent.condScratch...)
}

func (c *CondParser) emit(n CondNode) int32 {
	c.parent.condScratch = append(c.parent.condScratch, n)

	return int32(len(c.parent.condScratch) - 1)
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

func (c *CondParser) parseOr() int32 {
	left := c.parseAnd()

	for {
		t, ok := c.peek()

		if !ok || !(t.kind == tokIdent && t.val == "OR") {
			return left
		}

		c.consume()

		right := c.parseAnd()

		left = c.emit(CondNode{Kind: ckOr, L: left, R: right})
	}
}

func (c *CondParser) parseAnd() int32 {
	left := c.parseNot()

	for {
		t, ok := c.peek()

		if !ok || !(t.kind == tokIdent && t.val == "AND") {
			return left
		}

		c.consume()

		right := c.parseNot()

		left = c.emit(CondNode{Kind: ckAnd, L: left, R: right})
	}
}

func (c *CondParser) parseNot() int32 {
	t, ok := c.peek()

	if ok && t.kind == tokIdent && t.val == "NOT" {
		c.consume()

		return c.emit(CondNode{Kind: ckNot, L: c.parseNot()})
	}

	if ok && t.kind == tokIdent && t.val == "DEFINED" {
		c.consume()

		return c.emit(CondNode{Kind: ckDefined, L: c.parseAtom()})
	}

	return c.parseCmp()
}

func (c *CondParser) parseCmp() int32 {
	left := c.parseAtom()
	t, ok := c.peek()

	if !ok {
		return left
	}

	if t.kind == tokIdent && t.val == "STARTS_WITH" {
		c.consume()

		right := c.parseAtom()

		c.rejectChainedCmp(t)

		return c.emit(CondNode{Kind: ckStartsWith, L: left, R: right})
	}

	if t.kind == tokIdent && t.val == "MATCHES" {
		c.consume()

		right := c.parseAtom()

		c.rejectChainedCmp(t)

		return c.emit(CondNode{Kind: ckMatches, L: left, R: right})
	}

	if t.kind == tokIdent && strings.HasPrefix(t.val, "VERSION_") {
		c.consume()

		right := c.parseAtom()

		c.rejectChainedCmp(t)

		return c.emit(CondNode{Kind: ckVersionCmp, Name: t.val, L: left, R: right})
	}

	switch t.kind {
	case tokEq:
		c.consume()

		right := c.parseAtom()

		c.rejectChainedCmp(t)

		return c.emit(CondNode{Kind: ckEq, L: left, R: right})
	case tokLt:
		c.consume()

		right := c.parseAtom()

		c.rejectChainedCmp(t)

		return c.emit(CondNode{Kind: ckLt, L: left, R: right})
	case tokNotEq:

		c.consume()

		right := c.parseAtom()

		c.rejectChainedCmp(t)

		eq := c.emit(CondNode{Kind: ckEq, L: left, R: right})

		return c.emit(CondNode{Kind: ckNot, L: eq})
	case tokGt:

		c.consume()

		right := c.parseAtom()

		c.rejectChainedCmp(t)

		return c.emit(CondNode{Kind: ckLt, L: right, R: left})
	case tokGe:

		c.consume()

		right := c.parseAtom()

		c.rejectChainedCmp(t)

		lt := c.emit(CondNode{Kind: ckLt, L: left, R: right})

		return c.emit(CondNode{Kind: ckNot, L: lt})
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

func (c *CondParser) parseAtom() int32 {
	t, ok := c.peek()

	if !ok {
		c.parent.lex.throwParse(c.ifTok.line, c.ifTok.col, "unexpected end of IF condition")
	}

	if t.kind == tokLParen {
		c.consume()

		inner := c.parseOr()
		closer, hasCloser := c.peek()

		if !hasCloser || closer.kind != tokRParen {
			c.parent.lex.throwParse(t.line, t.col, "missing ')' in IF condition")
		}

		c.consume()

		return inner
	}

	if t.kind == tokString {
		c.consume()

		return c.emit(CondNode{Kind: ckString, Name: t.val})
	}

	if t.kind == tokInt {
		c.consume()

		n := 0

		for i := 0; i < len(t.val); i++ {
			n = n*10 + int(t.val[i]-'0')
		}

		return c.emit(CondNode{Kind: ckInt, Ival: n})
	}

	if t.kind == tokIdent || (t.kind == tokWord && isIdentShapedName(t.val)) {
		if t.val == "AND" || t.val == "OR" || t.val == "NOT" {
			c.parent.lex.throwParse(t.line, t.col, "operator %q used as identifier in IF condition", t.val)
		}

		c.consume()

		return c.emit(CondNode{Kind: ckIdent, Name: t.val, Env: internEnv(t.val)})
	}

	if t.kind == tokWord {
		c.consume()

		return c.emit(CondNode{Kind: ckString, Name: t.val})
	}

	c.parent.lex.throwParse(t.line, t.col, "unexpected %s in IF condition", describeToken(t))

	return -1
}

func (p *Parser) expandInclude(nameTok Token) {
	args := p.parseMacroArgs(nameTok)

	if len(args) == 0 {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE expects at least 1 argument (the path)")
	}

	p.expandOneInclude(nameTok, args[0].string())
}

func (p *Parser) expandOneInclude(nameTok Token, rel string) {
	rel = expandScalarVarRef(rel, p.includes.env)

	var target string

	if suffix, ok := strings.CutPrefix(rel, "${ARCADIA_ROOT}/"); ok {
		target = cleanRel(suffix)
	} else if filepath.IsAbs(rel) {
		p.lex.throwParse(nameTok.line, nameTok.col, "INCLUDE(%s): absolute paths escape the source root", rel)
	} else {
		target = cleanRel(joinRel(pathDir(p.name), rel))
	}

	if seen, _ := p.includes.once.get(uint64(internStr(target).strID())); seen {
		return
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

	if present, _ := p.fs.exists(srcRootRel, target); !present {
		return
	}

	bp := readForParse(p.fs, target)

	defer releaseParseBuf(bp)

	included := parseInternalWithState(p.fs, target, *bp, chain, p.includes)

	p.stmtScratch = append(p.stmtScratch, included.Stmts...)
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

func (*FromSandboxStmt) stmtMarker() {
}

func (*SplitCodegenStmt) stmtMarker() {
}

func (*BaseCodegenStmt) stmtMarker() {
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

func (*AllResourceFilesStmt) stmtMarker() {
}
