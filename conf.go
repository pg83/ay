package main

import (
	"crypto/md5"
	"fmt"
	"slices"
	"strings"
)

const (
	confRootRel            = "build/"
	confRootImport         = `@import "${CONF_ROOT}/`
	confMacroCallStackSize = 100
)

const (
	confSet ConfOp = iota
	confAppend
	confDefine
	confExclude
)

const (
	confKeywordFlag ConfKeywordKind = iota
	confKeywordScalar
	confKeywordArray
)

type ConfState struct {
	parser     Parser
	lexer      Lexer
	nodes      *BumpAllocator[CondNode]
	frames     []confParseFrame
	pending    []byte
	params     []string
	active     []bool
	lengths    []int
	tables     []*confTables
	written    []bool
	original   []bool
	selected   EnvStore
	all        EnvStore
	needed     BitSet
	seen       BitSet
	keys       []ENV
	used       []bool
	args       DenseMap[STR, []ANY]
	bindings   *BumpAllocator[confMacroBinding]
	expansions *BumpAllocator[confMacroExpansion]
	calls      *BumpAllocator[confMacroCallExpansion]
}

func newConfState() *ConfState {
	return &ConfState{
		nodes:      newBumpAllocator[CondNode](),
		bindings:   newBumpAllocator[confMacroBinding](),
		expansions: newBumpAllocator[confMacroExpansion](),
		calls:      newBumpAllocator[confMacroCallExpansion](),
	}
}

type ConfOp uint8

type ConfBranch struct {
	parent   int32
	previous int32
	expr     string
	cond     []CondNode
	chain    []int32
}

type ConfStmt struct {
	branch int32
	name   ENV
	scalar ConfValue
	value  string
	op     ConfOp
}

type ConfFile struct {
	state    *ConfState
	program  *confProgram
	rel      string
	branches []ConfBranch
	stmts    []ConfStmt
	macros   map[STR]*ConfMacro
	modules  map[string]string
	variants map[string][]string
	calls    map[string][]string

	callNames  []string
	callsGuard string
	callsStart int
}

func (f *ConfFile) flushCalls() {
	if f.callsGuard == "" || f.callsStart == len(f.callNames) {
		return
	}

	if f.calls == nil {
		f.calls = map[string][]string{}
	}

	names := f.callNames[f.callsStart:len(f.callNames):len(f.callNames)]

	if previous, ok := f.calls[f.callsGuard]; ok {
		names = append(previous[:len(previous):len(previous)], names...)
	}

	f.calls[f.callsGuard] = names
	f.callsStart = len(f.callNames)
}

type confParseFrame struct {
	branch    int32
	selectVar string
	skipped   bool
	block     string
	owner     string
	guard     string
	macro     bool
	body      confMacroBody
}

func (s *ConfState) readFile(rel, text string) *ConfFile {
	conf := &ConfFile{rel: rel, state: s, branches: []ConfBranch{{}}}
	stack := append(s.frames[:0], confParseFrame{})
	pending := s.pending[:0]
	previous := int32(0)

	defer func() {
		clear(stack)
		s.frames = stack[:0]
		s.pending = pending[:0]
	}()

	for raw := range strings.SplitSeq(text, "\n") {
		line := strings.TrimSpace(raw)

		if len(pending) == 0 && (line == "" || strings.HasPrefix(line, "#")) {
			continue
		}

		if strings.HasSuffix(line, "\\") {
			pending = append(pending, strings.TrimSuffix(line, "\\")...)
			pending = append(pending, ' ')

			continue
		}

		if len(pending) != 0 {
			pending = append(pending, line...)
			line = internOwnedCopy(pending)
			pending = pending[:0]
		}

		if frame := &stack[len(stack)-1]; frame.macro && !strings.HasPrefix(line, "}") {
			frame.body.addLine(line)
		}

		for strings.HasPrefix(line, "}") {
			if len(stack) == 1 {
				throwFmt("conf: unmatched } in %s", rel)
			}

			frame := &stack[len(stack)-1]

			previous = frame.branch

			if frame.macro {
				conf.addMacro(frame.body.header, frame.body.calls)
			}

			*frame = confParseFrame{}
			stack = stack[:len(stack)-1]
			line = strings.TrimSpace(line[1:])
		}

		if strings.HasPrefix(line, "@import") {
			conf.stmts = append(conf.stmts, ConfStmt{value: confImportPath(rel, line)})

			continue
		}

		top := &stack[len(stack)-1]

		if name, value, op, ok := confStmtAssignment(line); ok {
			if !top.skipped && top.block != "macro" {
				conf.stmts = append(conf.stmts, ConfStmt{branch: top.branch, name: internEnv(name), scalar: confStringValue(value), value: value, op: op})
			}

			continue
		}

		if !strings.HasSuffix(line, "{") {
			if name, _, ok := strings.Cut(line, "("); ok && top.guard != "" && !top.skipped && isIdentShapedName(strings.TrimSpace(name)) {
				if top.guard != conf.callsGuard {
					conf.flushCalls()
					conf.callsGuard = top.guard
				}

				conf.callNames = append(conf.callNames, internV("env", strings.TrimSpace(name)).string())
			}

			continue
		}

		header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		branch := ConfBranch{parent: top.branch, cond: condFalse}
		selectVar := ""
		block := ""
		guard := ""
		owner := top.owner

		switch {
		case strings.HasPrefix(header, "multimodule "):
			owner = strings.TrimSpace(strings.TrimPrefix(header, "multimodule "))
			branch.cond = nil
		case strings.HasPrefix(header, "module "):
			name, parent, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(header, "module ")), ":")

			name, parent = strings.TrimSpace(name), strings.TrimSpace(parent)

			if conf.modules == nil {
				conf.modules = map[string]string{}
			}

			if owner != "" {
				if conf.variants == nil {
					conf.variants = map[string][]string{}
				}

				fullName := name + "__from__" + owner

				conf.variants[owner] = append(conf.variants[owner], fullName)
				name = fullName
			}

			conf.modules[name] = parent
			branch.expr = internV(`($env`, name, ` == "yes")`).string()
			block = "module"
			guard = internV("env", name).string()
		case strings.HasPrefix(header, "macro "):
			name, _, _ := strings.Cut(strings.TrimPrefix(header, "macro "), "(")

			branch.expr = internV(`($env`, name, ` == "yes")`).string()
			block = "macro"
			guard = internV("env", strings.TrimSpace(name)).string()
		case strings.HasPrefix(header, "when "), strings.HasPrefix(header, "when("):
			branch.expr = strings.TrimSpace(strings.TrimPrefix(header, "when"))
		case strings.HasPrefix(header, "elsewhen "), strings.HasPrefix(header, "elsewhen("):
			branch.previous = previous
			branch.expr = strings.TrimSpace(strings.TrimPrefix(header, "elsewhen"))
		case header == "otherwise":
			branch.previous = previous
			branch.cond = nil
		case strings.HasPrefix(header, "select "), strings.HasPrefix(header, "select("):
			selectVar = strings.Trim(strings.TrimSpace(strings.TrimPrefix(header, "select")), "()$ ")
			branch.cond = nil
		case top.selectVar != "" && strings.HasSuffix(header, "?"):
			branch.previous = previous
			branch.cond = nil

			if alternatives := strings.TrimSpace(strings.TrimSuffix(header, "?")); alternatives != "default" {
				branch.expr = confSelectCondition(top.selectVar, alternatives)
			}
		}

		skip := top.skipped || branch.expr == "" && branch.cond != nil

		if branch.expr != "" {
			branch.cond = nil
		}

		conf.branches = append(conf.branches, branch)

		frame := confParseFrame{branch: int32(len(conf.branches) - 1), selectVar: selectVar, skipped: skip, block: block, owner: owner, guard: guard}

		if strings.HasPrefix(header, "macro ") {
			frame.macro = true
			frame.body.header = header
		}

		stack = append(stack, frame)
		previous = 0
	}

	if len(stack) != 1 || len(pending) != 0 {
		throwFmt("conf: unfinished block or assignment in %s", rel)
	}

	conf.flushCalls()
	conf.linkChains()

	return conf
}

func confSelectCondition(variable, alternatives string) string {
	var b strings.Builder

	b.WriteByte('(')

	for i, alt := range strings.Split(alternatives, "|") {
		if i > 0 {
			b.WriteString(" || ")
		}

		b.WriteString("$" + variable + " == " + strings.TrimSpace(alt))
	}

	b.WriteByte(')')

	return b.String()
}

func confImportPath(rel, line string) string {
	path, rooted := strings.CutPrefix(line, confRootImport)
	path, quoted := strings.CutSuffix(path, `"`)

	if !rooted || !quoted {
		throwFmt("conf: %s: unsupported %s", rel, line)
	}

	return confRootRel + path
}

func confStmtAssignment(line string) (name, value string, op ConfOp, ok bool) {
	name, value, ok = strings.Cut(line, "=")
	name = strings.TrimSpace(name)

	switch {
	case strings.HasSuffix(name, "+"):
		op = confAppend
	case strings.HasSuffix(name, ":"):
		op = confDefine
	case strings.HasSuffix(name, "-"):
		op = confExclude
	}

	if op != confSet {
		name = strings.TrimSpace(name[:len(name)-1])
	}

	return name, strings.TrimSpace(value), op, ok && name != "" && isConfVarName(name)
}

type ConfStmtRef struct {
	file *ConfFile
	stmt *ConfStmt
}

func (pm *IncludeParserManager) confFile(rel string) *ConfFile {
	key := internStr(rel)

	if f, ok := pm.cache.confFiles.get(key); ok {
		return f
	}

	if !pm.fs.isFile(srcRootRel, rel) {
		throwFmt("conf: @import %s: no such file", rel)
	}

	f := pm.cache.conf.readFile(rel, string(concatChunks(pm.fs.readPath(key))))

	pm.cache.confFiles.put(key, f)

	return f
}

func appendCondOperators(dst []byte, expr string) []byte {
	for i := 0; i < len(expr); {
		switch {
		case strings.HasPrefix(expr[i:], "&&"):
			dst = append(dst, " AND "...)
			i += 2
		case strings.HasPrefix(expr[i:], "||"):
			dst = append(dst, " OR "...)
			i += 2
		case strings.HasPrefix(expr[i:], "!="):
			dst = append(dst, "!="...)
			i += 2
		case expr[i] == '!':
			dst = append(dst, " NOT "...)
			i++
		default:
			dst = append(dst, expr[i])
			i++
		}
	}

	return dst
}

func (s *ConfState) parseCondition(rel, expr string) []CondNode {
	expr = confInCondition(rel, expr)
	s.lexer = Lexer{name: rel, src: appendCondOperators(s.lexer.src[:0], expr), line: 1, col: 1}

	p := &s.parser

	p.lex = &s.lexer
	p.condScratch = p.condScratch[:0]

	tok := Token{line: 1, col: 1}
	tokens := p.readCondTokens(tok)

	for i := range tokens {
		if tokens[i].kind == tokString {
			continue
		}

		if name, reference := strings.CutPrefix(tokens[i].string(), "$"); reference && isIdentShapedName(name) {
			tokens[i].kind = tokIdent
		}
	}

	cp := CondParser{parent: p, ifTok: tok, toks: tokens}

	cp.parseOr()

	if cp.pos != len(cp.toks) {
		throwFmt("conf: %s: unsupported condition %s", rel, expr)
	}

	for i := range p.condScratch {
		switch p.condScratch[i].Kind {
		case ckIdent:
			if name, reference := strings.CutPrefix(p.condScratch[i].Name, "$"); reference {
				p.condScratch[i].Kind = ckConfIdent
				p.condScratch[i].Name = name
				p.condScratch[i].Env = internEnv(name)
			} else if p.condScratch[i].Name == "yes" || p.condScratch[i].Name == "no" {
				p.condScratch[i].Kind = ckString
			} else {
				throwFmt("conf: %s: bare configuration variable %q needs $", rel, p.condScratch[i].Name)
			}
		case ckEq:
			p.condScratch[i].Kind = ckConfEq
		case ckGt:
			p.condScratch[i].L, p.condScratch[i].R = p.condScratch[i].R, p.condScratch[i].L

			fallthrough
		case ckLt:
			p.condScratch[i].Kind = ckConfLt
		}
	}

	block := s.nodes.alloc(len(p.condScratch))

	copy(block, p.condScratch)
	s.nodes.commit(len(p.condScratch))

	return block[:len(p.condScratch):len(p.condScratch)]
}

func confInCondition(rel, expr string) string {
	text := strings.TrimSpace(expr)

	if len(text) < 2 || text[0] != '(' || text[len(text)-1] != ')' {
		return expr
	}

	text = strings.TrimLeft(text[1:len(text)-1], " \t\r\n\f")

	end := 0

	if strings.HasPrefix(text, "$") {
		end = confNameEnd(text, 1)

		if end == 1 {
			return expr
		}
	} else if strings.HasPrefix(text, `"`) {
		quote := strings.IndexByte(text[1:], '"')

		if quote < 0 {
			return expr
		}

		end = quote + 2
	} else {
		return expr
	}

	left := text[:end]
	rest := strings.TrimLeft(text[end:], " \t\r\n\f")

	if len(rest) == len(text)-end || !strings.HasPrefix(rest, "in") {
		return expr
	}

	afterIn := strings.TrimLeft(rest[2:], " \t\r\n\f")

	if len(afterIn) == len(rest)-2 {
		return expr
	}

	rest = strings.TrimRight(afterIn, " \t\r\n\f")

	if len(rest) < 2 || rest[0] != '[' || rest[len(rest)-1] != ']' || strings.ContainsRune(rest, '\n') {
		return expr
	}

	rest = rest[1 : len(rest)-1]

	var out strings.Builder

	out.WriteByte('(')

	count := 0

	for strings.TrimSpace(rest) != "" {
		rest = strings.TrimLeft(rest, " \t\r\n\f")

		if !strings.HasPrefix(rest, `"`) {
			throwFmt("conf: %s: unsupported condition %s", rel, expr)
		}

		end := strings.IndexByte(rest[1:], '"') + 2

		if end == 1 {
			throwFmt("conf: %s: unsupported condition %s", rel, expr)
		}

		item := rest[:end]

		rest = strings.TrimLeft(rest[end:], " \t\r\n\f")

		if rest != "" {
			if rest[0] != ',' {
				throwFmt("conf: %s: unsupported condition %s", rel, expr)
			}

			rest = rest[1:]
		}

		if count > 0 {
			out.WriteString(" || ")
		}

		out.WriteString(left)
		out.WriteString(" == ")
		out.WriteString(item)
		count++
	}

	if count == 0 {
		throwFmt("conf: %s: unsupported condition %s", rel, expr)
	}

	out.WriteByte(')')

	return out.String()
}

func (c *ConfFile) cond(b int32) []CondNode {
	branch := &c.branches[b]

	if branch.expr != "" && branch.cond == nil {
		branch.cond = c.state.parseCondition(c.rel, branch.expr)
	}

	return branch.cond
}

func (c *ConfFile) linkChains() {
	branches := c.branches
	lengths := slices.Grow(c.state.lengths[:0], len(branches))[:len(branches)]
	total := 0

	for b := 1; b < len(branches); b++ {
		n := 1 + lengths[branches[b].parent]

		for p := branches[b].previous; p != 0; p = branches[p].previous {
			n++
		}

		lengths[b] = n
		total += n
	}

	c.state.lengths = lengths

	block := make([]int32, total)

	for b := 1; b < len(branches); b++ {
		n := lengths[b]
		chain := append(block[:0:n], int32(b))

		for p := branches[b].previous; p != 0; p = branches[p].previous {
			chain = append(chain, -p)
		}

		branches[b].chain = append(chain, branches[branches[b].parent].chain...)
		block = block[n:]
	}
}

func (c *ConfFile) conditionChain(b int32) []int32 {
	return c.branches[b].chain
}

func (c *ConfFile) condHolds(b int32, env Environment) bool {
	cond := c.cond(b)

	return len(cond) == 0 || evalCond(cond, env)
}

func (c *ConfFile) holds(b int32, env Environment) bool {
	for _, at := range c.conditionChain(b) {
		if at > 0 {
			if !c.condHolds(at, env) {
				return false
			}
		} else if c.condHolds(-at, env) {
			return false
		}
	}

	return true
}

func (c *ConfFile) condVars(b int32, add func(ENV)) {
	for _, at := range c.conditionChain(b) {
		if at < 0 {
			at = -at
		}

		for _, node := range c.cond(at) {
			if node.Kind == ckConfIdent {
				add(node.Env)
			}
		}
	}
}

func confNameEnd(text string, start int) int {
	end := start

	if end < len(text) && macroVariableLetter(text[end]) {
		end++

		for end < len(text) && (macroVariableLetter(text[end]) || text[end] >= '0' && text[end] <= '9') {
			end++
		}
	}

	return end
}

func confValueVars(value string, add func(string)) {
	for from := 0; ; {
		start, end, name := nextConfReference(value, from, true)

		if start == end {
			return
		}

		add(name)
		from = end
	}
}

func applyConfStmt(env Environment, s ConfStmt, conditional bool) {
	key := s.name
	bound := env.hasBindingID(key)

	switch s.op {
	case confSet:
		s.scalar.apply(env, key)
	case confAppend:
		if cur := env.string(key); bound && cur != "" {
			confStringValueID(internV(cur, " ", s.value)).apply(env, key)
		} else {
			s.scalar.apply(env, key)
		}
	case confDefine:
		if !bound {
			s.scalar.apply(env, key)
		}
	case confExclude:
		if conditional {
			return
		}

		value := env.string(key)

		if s.value != "" {
			for {
				pos := strings.Index(value, s.value)

				if pos < 0 {
					break
				}

				value = value[:pos] + value[pos+len(s.value):]
			}
		}

		env.setString(key, value)
	}
}

type ConfValue struct {
	value STR
	kind  EnvKind
}

func confStringValue(value string) ConfValue {
	if vfsHasPrefix(value) {
		return ConfValue{value: STR(intern(value)), kind: envVFS}
	}

	return ConfValue{value: internStr(value), kind: envStr}
}

func confStringValueID(value STR) ConfValue {
	if vfsHasPrefix(value.string()) {
		return ConfValue{value: STR(intern(value.string())), kind: envVFS}
	}

	return ConfValue{value: value, kind: envStr}
}

func confEnvValue(env Environment, key ENV) ConfValue {
	kind, value := env.s.lookup(key)

	if kind == envAbsent {
		value = 0
	}

	return ConfValue{value: value, kind: kind}
}

func confStringBinding(env Environment, key ENV) ConfValue {
	v := confEnvValue(env, key)

	if v.kind != envAbsent && v.kind != envVFS {
		v = confStringValueID(v.value)
	}

	return v
}

func (v ConfValue) apply(env Environment, key ENV) {
	switch v.kind {
	case envAbsent:
		env.unbind(key)
	case envVFS:
		env.setVFS(key, VFS(v.value))
	default:
		env.setStringID(key, v.value)
		env.s.kind[key] = v.kind
	}
}

type ConfVariant struct {
	name  STR
	value string
}

type confModule struct {
	index     int
	parent    STR
	variable  ENV
	builder   []ENV
	steps     []confInitStep
	outputs   []ENV
	originals []ENV
	read      []bool
	planned   bool
}

type confInitStep struct {
	variable ENV
	source   ENV
}

type ConfAssignment struct {
	key         ENV
	value       ConfValue
	clearInputs bool
}

type confProgram struct {
	stmts  []ConfStmtRef
	macros map[STR]*ConfMacro
}

type confTables struct {
	state       *ConfState
	source      []ConfStmtRef
	byVariable  DenseMap[ENV, []ENV]
	byTarget    DenseMap[ENV, []ConfStmtRef]
	targetReads DenseMap[ENV, []ENV]
	modules     DenseMap[STR, *confModule]
	variants    DenseMap[STR, []ConfVariant]
	moduleCount int
}

func confTableInput(r ConfStmtRef) bool {
	return r.stmt.branch != 0 || len(r.file.modules) != 0 || len(r.file.variants) != 0 || len(r.file.calls) != 0
}

func (t *confTables) matches(stmts []ConfStmtRef) bool {
	i := 0

	for _, r := range stmts {
		if confTableInput(r) {
			if i == len(t.source) || t.source[i] != r {
				return false
			}

			i++
		}
	}

	return i == len(t.source)
}

func (s *ConfState) tablesFor(stmts []ConfStmtRef) *confTables {
	for _, t := range s.tables {
		if t.matches(stmts) {
			return t
		}
	}

	t := &confTables{state: s}

	for _, r := range stmts {
		if confTableInput(r) {
			t.source = append(t.source, r)
		}
	}

	t.build()
	s.tables = append(s.tables, t)

	return t
}

type confDependency struct{ variable, target ENV }

func (pm *IncludeParserManager) confProgram(f *ConfFile, walking []string) *confProgram {
	if f.program != nil {
		return f.program
	}

	p := &confProgram{macros: map[STR]*ConfMacro{}}

	for name, macro := range f.macros {
		p.macros[name] = macro
	}

	count := 0

	for i := range f.stmts {
		if f.stmts[i].name != 0 {
			count++
		} else if imported := pm.confFile(f.stmts[i].value).program; imported != nil {
			count += len(imported.stmts)
		}
	}

	p.stmts = make([]ConfStmtRef, 0, count)

	for i := range f.stmts {
		s := &f.stmts[i]

		if s.name != 0 {
			p.stmts = append(p.stmts, ConfStmtRef{file: f, stmt: s})

			continue
		}

		if slices.Contains(walking, s.value) {
			throwFmt("conf: @import loop: %s -> %s", strings.Join(walking, " -> "), s.value)
		}

		imported := pm.confProgram(pm.confFile(s.value), append(walking, s.value))

		p.stmts = append(p.stmts, imported.stmts...)

		for name, macro := range imported.macros {
			if previous, ok := p.macros[name]; !ok || previous != nil {
				p.macros[name] = macro
			}
		}
	}

	f.program = p

	return p
}

func (t *confTables) build() {
	seen := map[*ConfFile]bool{}
	calls := map[string][]string{}

	var counts DenseMap[ENV, int]
	var dependencies []confDependency

	conditionalCount := 0

	for _, r := range t.source {
		if !seen[r.file] {
			for name, parent := range r.file.modules {
				module := &confModule{index: t.moduleCount, parent: internStr(parent), variable: internEnvSTR(internV("env", name))}

				t.moduleCount++
				t.modules.put(internStr(name), module)
			}

			for name, variants := range r.file.variants {
				ids := make([]ConfVariant, len(variants))

				for i, variant := range variants {
					ids[i] = ConfVariant{name: internStr(variant), value: variant}
				}

				t.variants.put(internStr(name), ids)
			}

			for guard, names := range r.file.calls {
				calls[guard] = names
			}

			seen[r.file] = true
		}

		if r.stmt.branch == 0 {
			continue
		}

		conditionalCount++

		target := r.stmt.name
		count, _ := counts.get(target)

		counts.put(target, count+1)

		r.file.condVars(r.stmt.branch, func(variable ENV) {
			dependencies = append(dependencies, confDependency{variable, target})
		})
	}

	names := make([]ENV, 0, len(counts.vals.s))

	for key := range counts.idx.each() {
		names = append(names, ENV(key))
	}

	slices.SortFunc(names, func(a, b ENV) int { return strings.Compare(a.string(), b.string()) })

	var ranks DenseMap[ENV, int32]

	for i, name := range names {
		ranks.put(name, int32(i))
	}

	slices.SortFunc(dependencies, func(a, b confDependency) int {
		if a.variable != b.variable {
			return int(a.variable) - int(b.variable)
		}

		ra, _ := ranks.get(a.target)
		rb, _ := ranks.get(b.target)

		return int(ra) - int(rb)
	})

	dependencies = slices.Compact(dependencies)

	targets := make([]ENV, len(dependencies))

	for i := 0; i < len(dependencies); {
		start, variable := i, dependencies[i].variable

		for i < len(dependencies) && dependencies[i].variable == variable {
			targets[i] = dependencies[i].target
			i++
		}

		t.byVariable.put(variable, targets[start:i:i])
	}

	refs := make([]ConfStmtRef, conditionalCount)

	for key, slot := range counts.idx.each() {
		n := counts.vals.s[slot]

		t.byTarget.put(ENV(key), refs[:0:n])
		refs = refs[n:]
	}

	for _, r := range t.source {
		if r.stmt.branch != 0 {
			stmts, _ := t.byTarget.get(r.stmt.name)

			t.byTarget.put(r.stmt.name, append(stmts, r))
		}
	}

	reads := make([]ENV, 0, len(dependencies))

	for key, slot := range t.byTarget.idx.each() {
		start := len(reads)

		for _, r := range t.byTarget.vals.s[slot] {
			r.file.condVars(r.stmt.branch, func(variable ENV) {
				if !slices.Contains(reads[start:], variable) {
					reads = append(reads, variable)
				}
			})
		}

		t.targetReads.put(ENV(key), reads[start:len(reads):len(reads)])
	}

	for _, module := range t.modules.vals.s {
		if module != nil {
			module.builder = t.builderMacros(calls, module.variable.string(), map[string]bool{}, nil)
		}
	}
}

func (t *confTables) builderMacros(calls map[string][]string, guard string, seen map[string]bool, out []ENV) []ENV {
	for _, call := range calls[guard] {
		if seen[call] {
			continue
		}

		seen[call] = true

		variable := internEnv(call)

		if targets, _ := t.byVariable.get(variable); len(targets) > 0 {
			out = append(out, variable)
		}

		out = t.builderMacros(calls, call, seen, out)
	}

	return out
}

func (t *confTables) module(name STR, tag string) *confModule {
	if variants, _ := t.variants.get(name); len(variants) > 0 {
		for _, variant := range variants {
			if strings.HasPrefix(variant.value, tag) && strings.HasPrefix(variant.value[len(tag):], "__from__") {
				name = variant.name

				break
			}
		}
	}

	module, _ := t.modules.get(name)

	return module
}

func (t *confTables) plan(module *confModule) *confModule {
	if module.planned {
		return module
	}

	module.planned = true

	if parent := t.module(module.parent, ""); parent != nil {
		module.steps = append(module.steps, t.plan(parent).steps...)
	}

	module.steps = append(module.steps, confInitStep{variable: module.variable})

	switch module.variable.string() {
	case "env_BASE_UNIT":
		module.steps = append(module.steps, confInitStep{variable: envBisonFlexSetDefaults})
	case "env_LINK_UNIT", "envGO_PROGRAM":
		module.steps = append(module.steps, confInitStep{variable: envLinkerId, source: envDefaultLinkerId}, confInitStep{variable: envCommonLinkSettings})
	}

	n := int(envTable.count)
	written := slices.Grow(t.state.written[:0], n)[:n]
	original := slices.Grow(t.state.original[:0], n)[:n]
	read := make([]bool, n)

	clear(written)
	clear(original)
	t.state.written, t.state.original = written, original

	readVar := func(key ENV) {
		if !written[key] {
			read[key] = true
		}
	}

	writeVar := func(key ENV) {
		if !written[key] {
			written[key] = true
			module.outputs = append(module.outputs, key)
		}
	}

	for _, step := range module.steps {
		if step.source != 0 {
			readVar(step.source)
		}

		writeVar(step.variable)

		targets, _ := t.byVariable.get(step.variable)

		for _, target := range targets {
			writeVar(target)

			if !original[target] {
				original[target] = true
				module.originals = append(module.originals, target)
			}

			reads, _ := t.targetReads.get(target)

			for _, key := range reads {
				readVar(key)
			}
		}
	}

	module.read = read

	return module
}

type Conf struct {
	state       *ConfState
	program     *confProgram
	tables      *confTables
	tags        DenseMap[STR, confExpansion]
	platformEnv Environment
	inherited   DenseMap[ENV, ConfValue]
	base        *EnvStore
	bases       [][]ConfAssignment
	sysinclList []string
}

func loadConf(pm *IncludeParserManager, root *ConfFile, platformEnv Environment) *Conf {
	program := pm.confProgram(root, nil)
	stmts := program.stmts
	scratch := pm.cache.conf

	commands := [...]ENV{
		envComma, internEnv(importMaskVariable), envDefaultAllocator,
		envCAsCxxFlags, envYqlPythonDir, envPmScript,
		envSetTsInputsBaseExcludes, envNotsTool, envPeerdirsRulesPath,
	}

	needed := &scratch.needed

	clear(needed.words.s)

	neededKeys := append(scratch.keys[:0], envSYSINCL)

	neededKeys = append(neededKeys, commands[:]...)

	for _, key := range neededKeys {
		needed.add(uint32(key))
	}

	scratch.used = slices.Grow(scratch.used[:0], len(stmts))[:len(stmts)]

	used := scratch.used

	clear(used)

	need := func(key ENV) {
		if !needed.has(uint32(key)) {
			needed.add(uint32(key))
			neededKeys = append(neededKeys, key)
		}
	}

	for grown := true; grown; {
		grown = false

		for i, r := range stmts {
			if used[i] || !needed.has(uint32(r.stmt.name)) {
				continue
			}

			used[i], grown = true, true
			confValueVars(r.stmt.value, func(name string) { need(internEnv(name)) })
			r.file.condVars(r.stmt.branch, need)
		}
	}

	scratch.keys = neededKeys

	env := platformEnv.cloneInto(&scratch.selected)

	for _, key := range neededKeys {
		env.unbind(key)
	}

	for i, r := range stmts {
		if used[i] && r.stmt.branch == 0 {
			applyConfStmt(env, *r.stmt, false)
		}
	}

	for i, r := range stmts {
		if used[i] && r.stmt.branch != 0 && r.file.holds(r.stmt.branch, env) {
			applyConfStmt(env, *r.stmt, true)
		}
	}

	all := platformEnv.cloneInto(&scratch.all)

	for _, r := range stmts {
		if r.stmt.branch == 0 {
			applyConfStmt(all, *r.stmt, false)
		}
	}

	for _, r := range stmts {
		if r.stmt.branch != 0 && r.file.holds(r.stmt.branch, all) {
			applyConfStmt(all, *r.stmt, true)
		}
	}

	for _, r := range stmts {
		if key := r.stmt.name; r.stmt.name != 0 && !platformEnv.hasBindingID(key) && all.hasBindingID(key) {
			confStringBinding(all, key).apply(platformEnv, key)
		}
	}

	for _, key := range commands {
		clear(scratch.seen.words.s)
		bindConfCommandVariable(key, env, platformEnv, &scratch.seen)
	}

	tables := pm.cache.conf.tablesFor(program.stmts)
	c := &Conf{state: pm.cache.conf, program: program, tables: tables, platformEnv: platformEnv, bases: make([][]ConfAssignment, tables.moduleCount)}

	c.sysinclList = strings.Fields(env.expand(env.string(envSYSINCL)))

	for key := range tables.byTarget.idx.each() {
		c.inherited.put(ENV(key), confStringBinding(platformEnv, ENV(key)))
	}

	return c
}

func bindConfCommandVariable(key ENV, values, target Environment, seen *BitSet) {
	if seen.has(uint32(key)) {
		return
	}

	seen.add(uint32(key))

	if values.hasBindingID(key) {
		confStringBinding(values, key).apply(target, key)
		confValueVars(values.string(key), func(dependency string) { bindConfCommandVariable(internEnv(dependency), values, target, seen) })
	}
}

func (c *Conf) sysincl() []string {
	return c.sysinclList
}

func (c *Conf) alias(name STR) *ConfMacro {
	return c.program.macros[name]
}

type confExpansion struct {
	keys   []ENV
	values []ConfValue
	result STR
}

func (c *Conf) peerTags(env Environment, template string) STR {
	key := internStr(template)

	if memo, ok := c.tags.get(key); ok {
		valid := true

		for i, k := range memo.keys {
			if confEnvValue(env, k) != memo.values[i] {
				valid = false

				break
			}
		}

		if valid {
			return memo.result
		}
	}

	result := internStr(expandAssignedScalarVarRef(template, env))
	memo := confExpansion{result: result}
	nested := false

	confValueVars(template, func(name string) {
		k := internEnv(name)
		v := confEnvValue(env, k)

		nested = nested || v.kind != envAbsent && strings.IndexByte(v.value.string(), '$') >= 0

		memo.keys = append(memo.keys, k)
		memo.values = append(memo.values, v)
	})

	if !nested {
		c.tags.put(key, memo)
	}

	return result
}

func (c *Conf) set(env Environment, variable ENV, originals map[STR]STR) {
	c.recalcInto(env, variable, originals, nil)
}

func (c *Conf) recalcInto(env Environment, variable ENV, originals map[STR]STR, recording []ConfAssignment) {
	targets, _ := c.tables.byVariable.get(variable)

	for _, target := range targets {
		env.unbind(target)

		stmts, _ := c.tables.byTarget.get(target)
		base, _ := c.inherited.get(target)

		if original, ok := originals[target.str()]; ok {
			if inherited := c.platformEnv.string(target); base.kind != envAbsent && inherited != "" {
				confStringValueID(internV(inherited, " ", original.string())).apply(env, target)
			} else {
				env.setStringID(target, original)
			}
		} else if base.kind != envAbsent {
			base.apply(env, target)
		}

		for _, r := range stmts {
			if r.file.holds(r.stmt.branch, env) {
				applyConfStmt(env, *r.stmt, true)
			}
		}

		if recording != nil {
			recordConfAssignment(recording, target, env)
		}
	}
}

func (c *Conf) initModule(env Environment, name STR, tag string, originals map[STR]STR) {
	defer func() { env.s.confBase = nil }()

	module := c.tables.module(name, tag)

	if module == nil {
		return
	}

	plan := c.tables.plan(module)

	if env.resourceUses != nil {
		env.s.confBase = nil
		c.runInit(plan, env, originals, nil)

		return
	}

	base := env.s.confBase
	clean := base != nil && base.confBase == base

	if clean && c.base == nil {
		c.base = base
	}

	clean = clean && base == c.base

	if clean {
		for _, key := range env.s.confWrites {
			if int(key) < len(plan.read) && plan.read[key] {
				clean = false

				break
			}
		}
	}

	if clean && len(originals) != 0 {
		for _, key := range plan.originals {
			if originals[key.str()] != 0 {
				clean = false

				break
			}
		}
	}

	env.s.confBase = nil

	if !clean {
		c.runInit(plan, env, originals, nil)

		return
	}

	if recorded := c.bases[module.index]; recorded != nil {
		for _, assignment := range recorded {
			assignment.value.apply(env, assignment.key)

			if assignment.clearInputs && assignment.value.kind == envAbsent {
				delete(env.s.textInputs, assignment.key)

				env.s.clearResourceBinding(assignment.key)
			}
		}

		return
	}

	recording := make([]ConfAssignment, len(plan.outputs))

	for i, key := range plan.outputs {
		recording[i] = ConfAssignment{key: key}
	}

	c.runInit(plan, env, originals, recording)
	c.bases[module.index] = recording
}

func (c *Conf) runInit(plan *confModule, env Environment, originals map[STR]STR, recording []ConfAssignment) {
	for _, step := range plan.steps {
		if step.source != 0 {
			env.setString(step.variable, env.string(step.source))
		} else {
			env.setBool(step.variable, true)
		}

		if recording != nil {
			recordConfAssignment(recording, step.variable, env)
		}

		c.recalcInto(env, step.variable, originals, recording)
	}
}

func recordConfAssignment(recording []ConfAssignment, key ENV, env Environment) {
	for i := range recording {
		if recording[i].key == key {
			v := confEnvValue(env, key)

			recording[i].value = v
			recording[i].clearInputs = recording[i].clearInputs || v.kind != envAbsent

			return
		}
	}

	panic("conf initialization wrote an undeclared output")
}

func (c *Conf) finishModule(env Environment, name STR, tag string, originals map[STR]STR) {
	if module := c.tables.module(name, tag); module != nil {
		c.finishModuleParent(env, module, originals)
	}
}

func (c *Conf) finishModuleParent(env Environment, module *confModule, originals map[STR]STR) {
	if parent := c.tables.module(module.parent, ""); parent != nil {
		c.finishModuleParent(env, parent, originals)
	}

	for _, variable := range module.builder {
		env.setBool(variable, true)

		targets, _ := c.tables.byVariable.get(variable)

		if slices.ContainsFunc(targets, func(target ENV) bool { return originals[target.str()] != 0 }) {
			c.set(env, variable, originals)
		}
	}
}

func (f *ConfFile) activeStatements(env Environment, wanted func(ENV) bool, visit func(ConfStmt)) {
	active := slices.Grow(f.state.active[:0], len(f.stmts))[:len(f.stmts)]

	for i := range f.stmts {
		s := &f.stmts[i]

		active[i] = s.name != 0 && wanted(s.name) && f.holds(s.branch, env)
	}

	f.state.active = active

	for i := range f.stmts {
		if active[i] {
			visit(f.stmts[i])
		}
	}
}

type ConfKeywordKind uint8

type ConfKeyword struct {
	name    STR
	kind    ConfKeywordKind
	present []ANY
	missing []ANY
}

type ConfMacroSignature struct {
	keywords   []ConfKeyword
	positional []STR
	vararg     STR
}

type ConfMacroCall struct {
	name STR
	args STR
}

type ConfMacro struct {
	file       string
	sig        ConfMacroSignature
	calls      []ConfMacroCall
	expansions *confMacroExpansion
}

type confMacroBody struct {
	header      string
	calls       []ConfMacroCall
	unsupported bool
}

func (f *ConfFile) addMacro(header string, calls []ConfMacroCall) {
	name, params, opened := strings.Cut(strings.TrimPrefix(header, "macro "), "(")
	params, closed := strings.CutSuffix(strings.TrimSpace(params), ")")

	if !opened || !closed {
		throwFmt("conf: %s: malformed macro header %q", f.rel, header)
	}

	sig, specialized := f.state.parseMacroSignature(f.rel, params)
	key := internStr(strings.TrimSpace(name))

	if !specialized && len(calls) == 0 {
		return
	}

	if f.macros == nil {
		f.macros = map[STR]*ConfMacro{}
	}

	if specialized {
		f.macros[key] = nil

		return
	}

	if _, ok := f.macros[key]; !ok {
		f.macros[key] = &ConfMacro{file: f.rel, sig: sig, calls: calls}
	}
}

func (body *confMacroBody) addLine(line string) {
	if body.unsupported {
		return
	}

	if property, ok := strings.CutPrefix(line, "."); ok {
		key, _, _ := strings.Cut(property, "=")

		switch strings.TrimSpace(key) {
		case "ALLOWED_IN_LINTERS_MAKE", "SEM", "STRUCT_SEM":
			return
		}
	} else {
		name, args, opened := strings.Cut(line, "(")

		name = strings.TrimSpace(name)

		args, closed := strings.CutSuffix(args, ")")

		if opened && closed && isIdentShapedName(name) {
			body.calls = append(body.calls, ConfMacroCall{name: internStr(name), args: internStr(strings.TrimSpace(args))})

			return
		}
	}

	body.calls = nil
	body.unsupported = true
}

func (s *ConfState) parseMacroSignature(rel, params string) (sig ConfMacroSignature, specialized bool) {
	split := splitConfMacroParams(params, s.params[:0])

	defer func() { s.params = split[:0] }()

	for _, param := range split {
		if strings.HasPrefix(param, `"`) {
			specialized = true

			continue
		}

		if vararg, ok := strings.CutSuffix(param, "..."); ok {
			sig.vararg = internStr(vararg)

			continue
		}

		n := 0

		for n < len(param) && (isConfIdentByte(param[n], n == 0) || n > 0 && param[n] == '-') {
			n++
		}

		name, rest := param[:n], strings.TrimSpace(param[n:])

		if strings.HasPrefix(rest, "{") {
			end := strings.IndexByte(rest, '}')

			if end < 0 {
				throwFmt("conf: %s: malformed macro parameter %q", rel, param)
			}

			rest = strings.TrimSpace(rest[end+1:])
		}

		switch {
		case rest == "":
			sig.positional = append(sig.positional, internStr(name))
		case rest == "[]":
			sig.keywords = append(sig.keywords, ConfKeyword{name: internStr(name), kind: confKeywordArray})
		case strings.HasPrefix(rest, "?"):
			keyword := ConfKeyword{name: internStr(name), kind: confKeywordFlag, present: []ANY{internStr(name).any()}}

			if init := strings.TrimSpace(rest[1:]); init != "" {
				onTrue, rest := cutConfQuoted(rel, init)
				onFalse, rest := cutConfQuoted(rel, strings.TrimPrefix(strings.TrimSpace(rest), ":"))

				if strings.TrimSpace(rest) != "" {
					throwFmt("conf: %s: malformed macro parameter %q", rel, param)
				}

				if onTrue != "" {
					keyword.present = []ANY{internStr(onTrue).any()}
				}

				if onFalse != "" {
					keyword.missing = []ANY{internStr(onFalse).any()}
				}
			}

			sig.keywords = append(sig.keywords, keyword)
		case strings.HasPrefix(rest, "="):
			keyword := ConfKeyword{name: internStr(name), kind: confKeywordScalar}
			value, tail := cutConfQuoted(rel, rest[1:])

			if strings.TrimSpace(tail) != "" {
				throwFmt("conf: %s: malformed macro parameter %q", rel, param)
			}

			if value != "" {
				keyword.missing = []ANY{internStr(value).any()}
			}

			sig.keywords = append(sig.keywords, keyword)
		default:
			throwFmt("conf: %s: malformed macro parameter %q", rel, param)
		}
	}

	return sig, specialized
}

func splitConfMacroParams(params string, out []string) []string {
	quoted, start := false, 0

	for i := 0; i <= len(params); i++ {
		if i < len(params) && params[i] == '"' {
			quoted = !quoted
		}

		if i == len(params) || params[i] == ',' && !quoted {
			if param := strings.TrimSpace(params[start:i]); param != "" {
				out = append(out, param)
			}

			start = i + 1
		}
	}

	return out
}

func cutConfQuoted(rel, value string) (string, string) {
	value = strings.TrimSpace(value)

	if value == "" || value[0] != '"' {
		throwFmt("conf: %s: expected a quoted macro parameter value, got %q", rel, value)
	}

	for i := 1; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++
		case '"':
			return value[1:i], value[i+1:]
		}
	}

	throwFmt("conf: %s: unterminated macro parameter value %q", rel, value)

	return "", ""
}

type confMacroBinding struct {
	name   STR
	values []ANY
	bound  bool
}

type confMacroCallExpansion struct {
	args     []ANY
	constant bool
}

type confMacroExpansion struct {
	next    *confMacroExpansion
	args    []ANY
	locals  []confMacroBinding
	calls   []confMacroCallExpansion
	failure string
}

func (s *ConfState) bind(macro *ConfMacro, args []ANY) *confMacroExpansion {
	for expansion := macro.expansions; expansion != nil; expansion = expansion.next {
		if slices.Equal(expansion.args, args) {
			return expansion
		}
	}

	expansion := s.expansions.one()

	expansion.next = macro.expansions
	expansion.args = astArgs.list(args...)
	expansion.locals, expansion.failure = macro.sig.bind(expansion.args, s.bindings)
	expansion.calls = s.calls.alloc(len(macro.calls))[:len(macro.calls):len(macro.calls)]
	s.calls.commit(len(macro.calls))
	macro.expansions = expansion

	return expansion
}

func (sig *ConfMacroSignature) keyword(arg ANY) int {
	for i := range sig.keywords {
		if sig.keywords[i].name.any() == arg {
			return i
		}
	}

	return -1
}

func (sig *ConfMacroSignature) arrayEnd(args []ANY, i int) int {
	for i < len(args) && sig.keyword(args[i]) < 0 {
		i++
	}

	return i
}

func (binding *confMacroBinding) append(values []ANY) {
	if len(binding.values) == 0 {
		binding.values = values
	} else if len(values) > 0 {
		block := astArgs.alloc(len(binding.values) + len(values))
		n := copy(block, binding.values)

		n += copy(block[n:], values)
		astArgs.commit(n)
		binding.values = block[:n:n]
	}

	binding.bound = true
}

func (sig *ConfMacroSignature) bind(args []ANY, arena *BumpAllocator[confMacroBinding]) ([]confMacroBinding, string) {
	n := len(sig.keywords) + len(sig.positional)

	if sig.vararg != 0 {
		n++
	}

	locals := arena.alloc(n)[:n:n]

	defer arena.commit(n)

	for i, keyword := range sig.keywords {
		locals[i].name = keyword.name
	}

	for i, name := range sig.positional {
		locals[len(sig.keywords)+i].name = name
	}

	if sig.vararg != 0 {
		locals[n-1].name = sig.vararg
	}

	next, array := 0, STR(0)

	for i := 0; i < len(args) || next < len(sig.positional); {
		if i == len(args) {
			return locals, fmt.Sprintf("Value for '%s' argument is missing.", sig.positional[next].string())
		}

		if k := sig.keyword(args[i]); k >= 0 {
			keyword := sig.keywords[k]
			binding := &locals[k]

			if binding.bound && keyword.kind != confKeywordArray {
				return locals, fmt.Sprintf("Keyword '%s' appears more than once and is not an array.", keyword.name.string())
			}

			i++

			switch keyword.kind {
			case confKeywordFlag:
				binding.append(keyword.present)
			case confKeywordScalar:
				if i == len(args) || sig.keyword(args[i]) >= 0 {
					return locals, fmt.Sprintf("Missing value for scalar keyword '%s'.", keyword.name.string())
				}

				binding.append(args[i : i+1])
				i++
			case confKeywordArray:
				end := sig.arrayEnd(args, i)

				array = keyword.name
				binding.append(args[i:end])
				i = end
			}

			continue
		}

		if array != 0 {
			return locals, fmt.Sprintf("Mixing positional arguments with named arrays is error prone and forbidden.\n\tGot positional arg '%s' after named array '%s' has been started.", args[i].string(), array.string())
		}

		if next < len(sig.positional) {
			locals[len(sig.keywords)+next].append(args[i : i+1])
			next++
			i++

			continue
		}

		if sig.vararg == 0 {
			return locals, fmt.Sprintf("More positional arguments passed than expected. Can't handle arg: '%s'.", args[i].string())
		}

		end := sig.arrayEnd(args, i)

		locals[n-1].append(args[i:end])
		i = end
	}

	for i, keyword := range sig.keywords {
		if !locals[i].bound {
			locals[i].append(keyword.missing)
		}
	}

	if sig.vararg != 0 {
		locals[n-1].bound = true
	}

	return locals, ""
}

func expandConfMacro(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, v UnknownStmt, env Environment, d *ModuleData, onWarn func(Warn), platform *Platform) {
	macro := platform.conf.alias(v.Raw)

	switch {
	case macro != nil:
	case v.Name == tokInvalid:
		onWarn(Warn{Kind: WarnUnknownMacro, Message: fmt.Sprintf("%s: macro %q is not in the modelled set (line %d); skipped", modulePath, v.Raw.string(), v.Line)})

		return
	default:
		warnUnmodelledMacro(onWarn, modulePath, v.Name)

		return
	}

	if d.confMacroDepth == confMacroCallStackSize {
		throwFmt("%s: config error: recursion in the calls of macro %s", modulePath, v.Raw.string())
	}

	state := platform.conf.state
	expansion := state.bind(macro, v.Args)
	locals, failure := expansion.locals, expansion.failure

	if failure != "" {
		onWarn(Warn{Kind: WarnBadMacroArgs, Message: fmt.Sprintf("%s: macro '%s': %s\n\tArgs: %s", modulePath, v.Raw.string(), failure, strings.Join(anyStrs(v.Args), ", "))})
	}

	d.confMacroDepth++

	defer func() { d.confMacroDepth-- }()

	for i, call := range macro.calls {
		cached := &expansion.calls[i]
		args := cached.args

		if !cached.constant {
			text := call.args
			constant := true

			if strHasDollar(text.any()) {
				text = internStr(substConfMacroArgs(text.string(), locals, env, &constant))
			}

			args = state.macroArgs(text)

			if constant {
				cached.args, cached.constant = args, true
			}
		}

		var scratch [1]Stmt

		stmts := appendMacroCallStmts(scratch[:0], call.name, args, args, v.Line, func(format string, a ...any) {
			throwFmt("%s: %s: "+format, append([]any{macro.file, call.name.string()}, a...)...)
		})

		collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
	}
}

func (s *ConfState) macroArgs(text STR) []ANY {
	if args, ok := s.args.get(text); ok {
		return args
	}

	value := text.string()
	block := astArgs.alloc(strings.Count(value, " ") + 1)
	n := 0

	for arg := range strings.SplitSeq(value, " ") {
		if arg != "" {
			block[n] = internStr(arg).any()
			n++
		}
	}

	astArgs.commit(n)

	args := block[:n:n]

	s.args.put(text, args)

	return args
}

func substConfMacroArgs(text string, locals []confMacroBinding, env Environment, constant *bool) string {
	for i := 0; strings.IndexByte(text, '$') >= 0; i++ {
		next := substConfMacroPass(text, locals, env, constant)

		if next == text {
			break
		}

		if i == 127 {
			throwFmt("Macro subst exceeded the limit 128")
		}

		text = next
	}

	return text
}

func substConfMacroPass(text string, locals []confMacroBinding, env Environment, constant *bool) string {
	var out strings.Builder

	copied := 0

	replace := func(start, end int, value string) {
		if value != text[start:end] {
			out.WriteString(text[copied:start])
			out.WriteString(value)
			copied = end
		}
	}

	for i := 0; i < len(text); {
		switch {
		case text[i] == '\\' && i+1 < len(text):
			i += 2
		case text[i] != '$':
			i++
		case i+1 < len(text) && text[i+1] == '{':
			end := confMacroRefEnd(text, i+2)

			if end < 0 {
				i = len(text)

				continue
			}

			inner := text[i+2 : end]

			switch {
			case strings.IndexByte(inner, '"') >= 0:
			case strings.IndexByte(inner, '$') >= 0:
				replace(i, end+1, "${"+substConfMacroPass(inner, locals, env, constant)+"}")
			default:
				replace(i, end+1, substConfMacroRef(text[i:end+1], inner, locals, env, constant))
			}

			i = end + 1
		default:
			n := confVarNameLen(text[i+1:])

			if n == 0 {
				i++

				continue
			}

			replace(i, i+1+n, substConfMacroRef(text[i:i+1+n], text[i+1:i+1+n], locals, env, constant))
			i += 1 + n
		}
	}

	if copied == 0 {
		return text
	}

	out.WriteString(text[copied:])

	return out.String()
}

func confMacroRefEnd(text string, i int) int {
	for depth := 1; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--

			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

func confVarNameLen(s string) int {
	if s == "" || !isConfIdentByte(s[0], true) {
		return 0
	}

	n := 1

	for n < len(s) {
		if isConfIdentByte(s[n], false) {
			n++

			continue
		}

		dashes := n

		for dashes < len(s) && s[dashes] == '-' {
			dashes++
		}

		if dashes == n || dashes == len(s) || !isConfIdentByte(s[dashes], false) {
			break
		}

		n = dashes
	}

	return n
}

func isConfIdentByte(b byte, first bool) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_' || !first && b >= '0' && b <= '9'
}

func substConfMacroRef(fragment, ref string, locals []confMacroBinding, env Environment, constant *bool) string {
	mods, name := "", ref

	if k := strings.LastIndexByte(ref, ':'); k >= 0 {
		mods, name = ref[:k], ref[k+1:]
	}

	if confVarNameLen(name) != len(name) {
		return fragment
	}

	var values []ANY

	bound := false
	id := interned(name)

	for _, binding := range locals {
		if binding.name == id && binding.bound {
			values, bound = binding.values, true

			break
		}
	}

	if !bound {
		switch name {
		case envBINDIR.string(), envCURDIR.string(), envARCADIA_ROOT.string(), envARCADIA_BUILD_ROOT.string():
			return fragment
		}

		*constant = false

		value, bound := env.lookup(name)

		if !bound {
			return fragment
		}

		values = []ANY{internStr(value).any()}
	}

	return applyConfMacroMods(fragment, mods, values)
}

func applyConfMacroMods(fragment, mods string, values []ANY) string {
	if mods == "" {
		return internAnyWords(values).string()
	}

	var prefix, suffix, join string

	var joined, hash, upper, lower, noExt, noAllExt, noPath, lastExt bool

	for _, mod := range strings.Split(mods, ";") {
		key, value, _ := strings.Cut(mod, "=")

		switch key {
		case "":
		case "pre":
			prefix = value
		case "suf":
			suffix = value
		case "join":
			join, joined = value, true
		case "hash":
			hash = true
		case "toupper":
			upper = true
		case "tolower":
			lower = true
		case "noext":
			noExt = true
		case "noallext":
			noAllExt = true
		case "nopath":
			noPath = true
		case "lastext":
			lastExt = true
		default:
			return fragment
		}
	}

	parts := make([]string, len(values))

	for i, arg := range values {
		value := arg.string()
		slash := strings.LastIndexByte(value, '/') + 1

		if noExt || noAllExt {
			dot := strings.LastIndexByte(value[slash:], '.')

			if noAllExt {
				dot = strings.IndexByte(value[slash:], '.')
			}

			if dot >= 0 {
				value = value[:slash+dot]
			}
		}

		if lastExt {
			if dot := strings.LastIndexByte(value[slash:], '.'); dot >= 0 {
				value = value[slash+dot+1:]
			} else {
				value = ""
			}
		}

		if noPath {
			value = value[strings.LastIndexByte(value, '/')+1:]
		}

		value = prefix + value + suffix

		if upper {
			value = strings.ToUpper(value)
		}

		if lower {
			value = strings.ToLower(value)
		}

		parts[i] = value
	}

	separator := " "

	if joined {
		separator = join
	}

	result := strings.Join(parts, separator)

	if hash {
		result = fmt.Sprintf("%x", md5.Sum([]byte(result)))
	}

	return result
}
