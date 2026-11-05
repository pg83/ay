package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/zeebo/xxh3"
)

const (
	confRootRel            = "build/"
	confRootImport         = `@import "${CONF_ROOT}/`
	confMacroCallStackSize = 100
	confCommandDirsMemo    = 8
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

const (
	confMacroIgnored confMacroVerdict = 1 << iota
	confMacroRestricted
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
	declared []STR
	modules  map[string]string
	variants map[string][]string
	calls    map[string][]string
	body     map[string][]ConfMacroCall
	inits    map[STR]*ConfMacro
	commands map[string]*confCommand
	selfPeer map[string][]string
	aliases  map[string]map[STR]STR
	limits   map[string]*confMacroLimits

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

func (f *ConfFile) addBodyCall(guard string, name STR, args string) {
	if f.body == nil {
		f.body = map[string][]ConfMacroCall{}
	}

	f.body[guard] = append(f.body[guard], ConfMacroCall{name: name, args: internStr(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(args), ")")))})
}

type confParseFrame struct {
	branch    int32
	selectVar string
	skipped   bool
	block     string
	owner     string
	guard     string
	macro     bool
	enclosing int
	body      confMacroBody
}

func (f *ConfFile) macroConditionParams(macroBranch int32, sig ConfMacroSignature) []string {
	var params []string

	for b := int(macroBranch) + 1; b < len(f.branches); b++ {
		if f.branches[b].parent < macroBranch {
			break
		}

		confTextRefs(f.branches[b].expr, func(key ENV) {
			name := internStr(key.string())

			if (slices.Contains(sig.positional, name) || name == sig.vararg && sig.vararg != 0 || slices.ContainsFunc(sig.keywords, func(k ConfKeyword) bool { return k.name == name })) && !slices.Contains(params, name.string()) {
				params = append(params, name.string())
			}
		})
	}

	return params
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
		lines := [2]string{1: strings.TrimSpace(raw)}
		first := 1

		if len(pending) != 0 && !confContinues(lines[1]) {
			lines[0], first = internOwnedCopy(pending[:len(pending)-1]), 0
			pending = pending[:0]
		}

		for _, line := range lines[first:] {
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
					conf.addMacro(frame.body.header, frame.branch, &frame.body)
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

			if property, ok := strings.CutPrefix(line, ".ALIASES="); ok && top.block == "module" && !top.skipped {
				if conf.aliases == nil {
					conf.aliases = map[string]map[STR]STR{}
				}

				aliases := map[STR]STR{}

				for pair := range strings.FieldsSeq(property) {
					if from, to, ok := strings.Cut(pair, "="); ok {
						aliases[internStr(from)] = internStr(to)
					}
				}

				conf.aliases[top.guard] = aliases

				continue
			}

			if name, list, ok := confLimitProperty(line); ok && top.block == "module" && !top.skipped {
				if conf.limits == nil {
					conf.limits = map[string]*confMacroLimits{}
				}

				limits := conf.limits[top.guard]

				if limits == nil {
					limits = &confMacroLimits{}
					conf.limits[top.guard] = limits
				}

				for macro := range strings.FieldsSeq(list) {
					limits.set(name).add(internStr(macro))
				}

				continue
			}

			if property, ok := strings.CutPrefix(line, ".PEERDIRSELF="); ok && top.block == "module" && !top.skipped {
				if conf.selfPeer == nil {
					conf.selfPeer = map[string][]string{}
				}

				conf.selfPeer[top.guard] = strings.Fields(property)

				continue
			}

			if name, value, op, ok := confStmtAssignment(line); ok {
				if top.block == "macro" {
					if !top.skipped {
						top.body.locals = append(top.body.locals, confMacroLocal{name: internStr(name), value: internStr(value).any()})
					}

					continue
				}

				if !top.skipped {
					branch := top.branch

					if top.enclosing != 0 {
						stack[top.enclosing].body.conditions = true
					}

					conf.stmts = append(conf.stmts, ConfStmt{branch: branch, name: internEnv(name), scalar: confStringValue(value), value: value, op: op})
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

					if top.block == "module" {
						conf.addBodyCall(top.guard, internStr(strings.TrimSpace(name)), line[len(name)+1:])
					}
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
			case strings.HasPrefix(header, "macro "), strings.HasPrefix(header, "multimodule "):
				if len(stack) != 1 {
					throwFmt("conf: %s: %s inside a block", rel, header)
				}
			case strings.HasPrefix(header, "module "):
				if len(stack) != 1 && !(len(stack) == 2 && top.block == "multimodule") {
					throwFmt("conf: %s: %s inside a block", rel, header)
				}
			}

			switch {
			case strings.HasPrefix(header, "multimodule "):
				owner = strings.TrimSpace(strings.TrimPrefix(header, "multimodule "))
				branch.cond = nil
				block = "multimodule"
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

				name = strings.TrimSpace(name)
				conf.declared = append(conf.declared, internStr(name))
				branch.expr = internV(`($env`, name, ` == "yes")`).string()
				block = "macro"
				guard = internV("env", name).string()
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

			frame := confParseFrame{branch: int32(len(conf.branches) - 1), selectVar: selectVar, skipped: skip, block: block, owner: owner, guard: guard, enclosing: top.enclosing}

			if strings.HasPrefix(header, "macro ") {
				frame.macro = true
				frame.enclosing = len(stack)
				frame.body.header = header
			}

			stack = append(stack, frame)
			previous = 0
		}
	}

	if len(stack) != 1 || len(pending) != 0 {
		throwFmt("conf: unfinished block or assignment in %s", rel)
	}

	conf.flushCalls()
	conf.linkChains()

	return conf
}

func confContinues(next string) bool {
	next = strings.TrimLeft(next, " \t\r")

	if next == "" || next[0] == '#' || next[0] == '}' || strings.HasPrefix(next, "@import") {
		return false
	}

	rest := strings.TrimPrefix(next, ".")
	n := confVarNameLen(rest)

	if n == 0 {
		return true
	}

	rest = strings.TrimLeft(rest[n:], " \t")

	for _, op := range [...]string{"=", ":=", "+=", "-=", "(", "{"} {
		if strings.HasPrefix(rest, op) {
			return false
		}
	}

	return true
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

	return name, strings.TrimSpace(value), op, ok && name != "" && confVarNameLen(name) == len(name)
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
		end = 1 + confVarNameLen(text[1:])

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

func (c *ConfFile) condInputs(b int32, add func(ENV), literal *bool) {
	for _, at := range c.conditionChain(b) {
		if at < 0 {
			at = -at
		}

		for _, node := range c.cond(at) {
			switch node.Kind {
			case ckConfIdent, ckIdent, ckDefined, ckIsNum:
				add(node.Env)
			case ckString:
				*literal = *literal || strings.IndexByte(node.Name, '$') >= 0
			}
		}
	}
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

func confTextRefs(text string, visit func(ENV)) {
	for ref := range macroRefs(text) {
		if !ref.raw {
			visit(internEnv(ref.name))
		}
	}
}

func confValueVars(value string, add func(string)) {
	for ref := range macroRefs(value) {
		if !ref.raw {
			add(ref.name)
		}
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
	calls     []string
	body      []ConfMacroCall
	dirs      []ConfMacroCall
	owner     STR
	sub       STR
	selfPeers []STR
	stmts     []ConfMacroCall
	stmtsDone bool
	aliases   map[STR]STR
	own       *confMacroLimits
	limits    *confMacroLimits
	renames   map[STR]STR
	verdicts  map[STR]confMacroVerdict
	order     int
	steps     []confInitStep
	outputs   []ENV
	originals []ENV
	read      []bool
	planned   bool
}

type confInitStep struct {
	variable ENV
	source   ENV
	value    STR
	fallback bool
	appended bool
	expand   bool
	original bool
}

type ConfAssignment struct {
	key         ENV
	value       ConfValue
	clearInputs bool
	stored      bool
	original    STR
}

type confProgram struct {
	stmts       []ConfStmtRef
	macros      map[STR]*ConfMacro
	inits       map[STR]*ConfMacro
	commands    map[string]*confCommand
	commandVars map[string]bool
}

type confCommand struct {
	cmd     string
	peerdir string
	addincl string
}

type confTables struct {
	state       *ConfState
	macros      map[STR]*ConfMacro
	inits       map[STR]*ConfMacro
	source      []ConfStmtRef
	byVariable  DenseMap[ENV, []ENV]
	byTarget    DenseMap[ENV, []ConfStmtRef]
	targetReads DenseMap[ENV, []ENV]
	recalcReads DenseMap[ENV, []ENV]
	modules     DenseMap[STR, *confModule]
	variants    DenseMap[STR, []ConfVariant]
	peers       DenseMap[STR, []confPeerVariant]
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

func (s *ConfState) tablesFor(program *confProgram) *confTables {
	stmts := program.stmts

	for _, t := range s.tables {
		if t.matches(stmts) {
			return t
		}
	}

	t := &confTables{state: s, macros: program.macros, inits: program.inits}

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

	p := &confProgram{macros: map[STR]*ConfMacro{}, inits: map[STR]*ConfMacro{}, commands: map[string]*confCommand{}}

	for name, macro := range f.macros {
		p.macros[name] = macro
	}

	for name, command := range f.commands {
		p.commands[name] = command
	}

	for name, macro := range f.inits {
		p.inits[name] = macro
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

		for name, macro := range imported.inits {
			if previous, ok := p.inits[name]; !ok || previous != nil {
				p.inits[name] = macro
			}
		}

		for name, command := range imported.commands {
			p.commands[name] = command
		}
	}

	p.markDirBodies()
	p.markCommandVars()

	f.program = p

	return p
}

func (p *confProgram) markCommandVars() {
	refs := map[string][]string{}

	p.commandVars = map[string]bool{}

	for _, ref := range p.stmts {
		name := ref.stmt.name.string()

		scanCommandRefs(ref.stmt.value, func(ident string, call bool) {
			if !call {
				refs[name] = append(refs[name], ident)
			} else if p.commands[ident] != nil {
				p.commandVars[name] = true
			}
		})
	}

	for grown := true; grown; {
		grown = false

		for name, used := range refs {
			if !p.commandVars[name] && slices.ContainsFunc(used, func(v string) bool { return p.commandVars[v] }) {
				p.commandVars[name] = true
				grown = true
			}
		}
	}
}

func scanCommandRefs(text string, visit func(ident string, call bool)) {
	for i := 0; i < len(text); i++ {
		if text[i] != '$' {
			continue
		}

		rest := text[i+1:]

		if strings.HasPrefix(rest, "{") {
			end := strings.IndexByte(rest, '}')

			if end < 0 {
				return
			}

			if n := cmdIdentLen(rest[1:end]); n > 0 && n == end-1 {
				visit(rest[1:end], false)
			}

			i += end

			continue
		}

		if n := cmdIdentLen(rest); n > 0 {
			visit(rest[:n], n < len(rest) && rest[n] == '(')
			i += n
		}
	}
}

func (p *confProgram) markDirBodies() {
	for _, macro := range p.macros {
		if macro == nil {
			continue
		}

		macro.dirs = slices.ContainsFunc(macro.calls, func(call ConfMacroCall) bool { return dirStatement(call.name) })

		for i := range macro.calls {
			macro.calls[i].macro = p.macros[macro.calls[i].name]
		}
	}

	for grown := true; grown; {
		grown = false

		for _, macro := range p.macros {
			if macro == nil || macro.dirs {
				continue
			}

			for _, call := range macro.calls {
				if nested := call.macro; nested != nil && nested.dirs {
					macro.dirs, grown = true, true

					break
				}
			}
		}
	}
}

func (t *confTables) build() {
	seen := map[*ConfFile]bool{}
	calls := map[string][]string{}
	body := map[string][]ConfMacroCall{}
	selfPeers := map[string][]string{}
	aliases := map[string]map[STR]STR{}
	limits := map[string]*confMacroLimits{}

	var owners []*ConfFile

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

			for guard, calls := range r.file.body {
				body[guard] = calls
			}

			for guard, tags := range r.file.selfPeer {
				selfPeers[guard] = tags
			}

			for guard, pairs := range r.file.aliases {
				aliases[guard] = pairs
			}

			for guard, own := range r.file.limits {
				limits[guard] = own
			}

			if len(r.file.variants) != 0 {
				owners = append(owners, r.file)
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

	counts.idx.each(func(key, _ uint32) {
		names = append(names, ENV(key))
	})

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

	counts.idx.each(func(key, slot uint32) {
		n := counts.vals.s[slot]

		t.byTarget.put(ENV(key), refs[:0:n])
		refs = refs[n:]
	})

	for _, r := range t.source {
		if r.stmt.branch != 0 {
			stmts, _ := t.byTarget.get(r.stmt.name)

			t.byTarget.put(r.stmt.name, append(stmts, r))
		}
	}

	reads := make([]ENV, 0, len(dependencies))

	t.byTarget.idx.each(func(key, slot uint32) {
		start := len(reads)

		for _, r := range t.byTarget.vals.s[slot] {
			r.file.condVars(r.stmt.branch, func(variable ENV) {
				if !slices.Contains(reads[start:], variable) {
					reads = append(reads, variable)
				}
			})
		}

		t.targetReads.put(ENV(key), reads[start:len(reads):len(reads)])
	})

	t.byTarget.idx.each(func(key, slot uint32) {
		target := ENV(key)

		var all []ENV

		literal := false

		for _, r := range t.byTarget.vals.s[slot] {
			r.file.condInputs(r.stmt.branch, func(variable ENV) {
				if variable != target && !slices.Contains(all, variable) {
					all = append(all, variable)
				}
			}, &literal)
		}

		if !literal {
			t.recalcReads.put(target, all)
		}
	})

	for _, module := range t.modules.vals.s {
		if module != nil {
			module.calls = calls[module.variable.string()]
			module.body = body[module.variable.string()]
			module.aliases = aliases[module.variable.string()]
			module.own = limits[module.variable.string()]
		}
	}

	for _, module := range t.modules.vals.s {
		if module != nil {
			t.inheritLimits(module)
		}
	}

	for _, file := range owners {
		for _, variants := range file.variants {
			t.uniteLimits(variants)
		}
	}

	for _, module := range t.modules.vals.s {
		if module != nil {
			module.verdicts = module.limits.verdicts(module.renames)
		}
	}

	for _, module := range t.modules.vals.s {
		if module != nil {
			module.dirs = t.bodyDirs(module)
		}
	}

	for _, file := range owners {
		for owner, variants := range file.variants {
			t.orderVariants(owner, variants, selfPeers)
		}
	}

	for _, file := range owners {
		for owner, variants := range file.variants {
			t.peers.put(internStr(owner), t.peerVariants(owner, variants))

			for _, name := range variants {
				if module, _ := t.modules.get(internStr(name)); module != nil {
					module.owner = internStr(owner)
					module.sub = internStr(strings.TrimSuffix(name, "__from__"+owner))

					for _, sub := range selfPeers[module.variable.string()] {
						module.selfPeers = append(module.selfPeers, internStr(sub))
					}
				}
			}
		}
	}
}

type confPeerVariant struct {
	sub STR
	tag STR
}

func (t *confTables) peerVariants(owner string, variants []string) []confPeerVariant {
	type ordered struct {
		variant confPeerVariant
		order   int
	}

	list := make([]ordered, 0, len(variants))

	for _, name := range variants {
		module, _ := t.modules.get(internStr(name))
		sub := internStr(strings.TrimSuffix(name, "__from__"+owner))
		tag := sub

		var steps []confInitStep

		for _, call := range module.body {
			steps, _ = t.callSteps(call.name, call.args.string(), steps, 0)
		}

		for _, step := range steps {
			if step.variable == envMODULE_TAG && step.value != 0 && !step.expand && !step.appended {
				tag = step.value
			}
		}

		list = append(list, ordered{confPeerVariant{sub: sub, tag: tag}, module.order})
	}

	slices.SortStableFunc(list, func(a, b ordered) int { return cmp.Compare(a.order, b.order) })

	out := make([]confPeerVariant, len(list))

	for i, entry := range list {
		out[i] = entry.variant
	}

	return out
}

func (c *Conf) peerVariants(owner STR) []confPeerVariant {
	variants, _ := c.tables.peers.get(owner)

	return variants
}

func (t *confTables) bodyDirs(module *confModule) []ConfMacroCall {
	var chain []*confModule

	for base := module; base != nil; base = t.module(base.parent, "") {
		chain = append(chain, base)
	}

	var dirs []ConfMacroCall

	for i := len(chain) - 1; i >= 0; i-- {
		for _, call := range chain[i].body {
			switch {
			case call.name == tokPyProgramPlugin.str():
				dirs = append(dirs, call)
			case call.name == strGhostPeerdir || module.limits.ignored.has(call.name):
			case dirStatement(call.name):
				dirs = append(dirs, call)
			default:
				if macro := t.macros[call.name]; macro != nil && macro.dirs {
					call.macro = macro
					dirs = append(dirs, call)
				}
			}
		}
	}

	return dirs
}

type confMacroLimits struct {
	restricted, ignored, allowed confNameSet
}

type confNameSet []STR

func (s confNameSet) has(name STR) bool {
	return slices.Contains(s, name)
}

func (s *confNameSet) add(name STR) {
	if !s.has(name) {
		*s = append(*s, name)
	}
}

type confMacroVerdict uint8

func (l *confMacroLimits) verdicts(renames map[STR]STR) map[STR]confMacroVerdict {
	verdict := func(name STR) confMacroVerdict {
		var v confMacroVerdict

		if l.ignored.has(name) {
			v |= confMacroIgnored
		}

		if l.restricted.has(name) && !l.allowed.has(name) {
			v |= confMacroRestricted
		}

		return v
	}

	out := map[STR]confMacroVerdict{}

	for _, names := range [...]confNameSet{l.ignored, l.restricted} {
		for _, name := range names {
			if _, renamed := renames[name]; !renamed {
				if v := verdict(name); v != 0 {
					out[name] = v
				}
			}
		}
	}

	for from, to := range renames {
		if v := verdict(to); v != 0 {
			out[from] = v
		} else {
			delete(out, from)
		}
	}

	return out
}

func confLimitProperty(line string) (string, string, bool) {
	for _, name := range [...]string{".RESTRICTED=", ".IGNORED=", ".ALLOWED="} {
		if list, ok := strings.CutPrefix(line, name); ok {
			return name, list, true
		}
	}

	return "", "", false
}

func (l *confMacroLimits) set(property string) *confNameSet {
	switch property {
	case ".RESTRICTED=":
		return &l.restricted
	case ".IGNORED=":
		return &l.ignored
	}

	return &l.allowed
}

func (t *confTables) inheritLimits(module *confModule) *confMacroLimits {
	if module.limits != nil {
		return module.limits
	}

	limits := &confMacroLimits{}

	module.renames = map[STR]STR{}

	for from, to := range module.aliases {
		module.renames[from] = to
	}

	if module.own != nil {
		*limits = confMacroLimits{restricted: slices.Clone(module.own.restricted), ignored: slices.Clone(module.own.ignored), allowed: slices.Clone(module.own.allowed)}
	}

	module.limits = limits

	if parent := t.module(module.parent, ""); parent != nil && parent != module {
		inherited := t.inheritLimits(parent)

		for from, to := range parent.renames {
			if _, ok := module.renames[from]; !ok {
				module.renames[from] = to
			}
		}

		for _, name := range inherited.allowed {
			if !limits.restricted.has(name) {
				limits.allowed.add(name)
			}
		}

		for _, name := range inherited.restricted {
			if !limits.allowed.has(name) {
				limits.restricted.add(name)
			}
		}

		for _, name := range inherited.ignored {
			if !limits.allowed.has(name) {
				limits.ignored.add(name)
			}
		}
	}

	return limits
}

func (t *confTables) uniteLimits(variants []string) {
	var all confNameSet

	for i, variant := range variants {
		module, _ := t.modules.get(internStr(variant))

		if module == nil {
			return
		}

		restricted := t.inheritLimits(module).restricted

		if i == 0 {
			all = slices.Clone(restricted)

			continue
		}

		if len(restricted) == 0 {
			all = nil

			break
		}

		all = slices.DeleteFunc(all, func(name STR) bool { return !restricted.has(name) })
	}

	for _, variant := range variants {
		module, _ := t.modules.get(internStr(variant))

		for _, name := range module.limits.restricted {
			if !all.has(name) {
				module.limits.ignored.add(name)
			}
		}
	}
}

func (t *confTables) orderVariants(owner string, variants []string, selfPeers map[string][]string) {
	suffix := "__from__" + owner
	tags := make([]string, len(variants))

	for i, variant := range variants {
		tags[i] = strings.TrimSuffix(variant, suffix)
	}

	slices.Sort(tags)

	done := map[string]bool{}
	order := 0

	var visit func(tag string)

	visit = func(tag string) {
		if done[tag] || !slices.Contains(tags, tag) {
			return
		}

		done[tag] = true

		for _, peer := range selfPeers[internV("env", tag, suffix).string()] {
			visit(peer)
		}

		if module, _ := t.modules.get(internV(tag, suffix)); module != nil {
			module.order = order
		}

		order++
	}

	for _, tag := range tags {
		visit(tag)
	}
}

func (t *confTables) varSteps(name STR, depth int) bool {
	macro := t.inits[name]

	if macro == nil || depth > 8 {
		return false
	}

	for _, call := range macro.calls {
		switch call.name {
		case tokEnable.str(), tokDisable.str(), kwSET, kwDEFAULT, tokSetAppend.str():
			if strings.IndexByte(call.args.string(), '"') >= 0 || strings.HasPrefix(call.args.string(), "$") {
				return false
			}
		default:
			if !t.varSteps(call.name, depth+1) {
				return false
			}
		}
	}

	return true
}

func (t *confTables) callSteps(name STR, args string, out []confInitStep, depth int) ([]confInitStep, bool) {
	words := strings.Fields(args)

	switch name {
	case tokEnable.str(), tokDisable.str():
		if len(words) != 1 || strings.IndexByte(words[0], '$') >= 0 {
			return out, false
		}

		value := strYes

		if name == tokDisable.str() {
			value = strNo
		}

		return append(out, confInitStep{variable: internEnv(words[0]), value: value, original: true}), true
	case kwSET, kwDEFAULT:
		if len(words) == 0 || strings.IndexByte(args, '"') >= 0 || strings.IndexByte(words[0], '$') >= 0 {
			return out, false
		}

		step := confInitStep{variable: internEnv(words[0]), value: internStr(strings.Join(words[1:], " ")), original: true, fallback: name == kwDEFAULT}

		step.expand = step.fallback || step.value == 0 || strings.IndexByte(step.value.string(), '$') >= 0

		return append(out, step), true
	case tokSetAppend.str():
		if len(words) < 2 || strings.IndexByte(args, '"') >= 0 || strings.IndexByte(words[0], '$') >= 0 {
			return out, false
		}

		value := internStr(strings.Join(words[1:], " "))

		return append(out, confInitStep{variable: internEnv(words[0]), value: value, appended: true, original: true, expand: strings.IndexByte(value.string(), '$') >= 0}), true
	}

	macro := t.inits[name]

	if macro == nil || depth > 8 || len(macro.sig.keywords) == 0 && len(words) > len(macro.sig.positional) && macro.sig.vararg == 0 {
		return out, false
	}

	var locals []confMacroBinding

	bound := false

	for _, call := range macro.calls {
		text := call.args.string()

		if strings.IndexByte(text, '$') >= 0 {
			if !bound {
				locals, _ = macro.sig.bind(internAnys(words), t.state.bindings)
				bound = true
			}

			text = substMacroDeeply(text, locals, Environment{}, nil, substVariables)
		}

		if next, ok := t.callSteps(call.name, text, out, depth+1); ok {
			out = next
		}
	}

	return out, true
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

	var chain []*confModule

	for base := module; base != nil; base = t.module(base.parent, "") {
		chain = append(chain, base)
	}

	for i := len(chain) - 1; i >= 0; i-- {
		module.steps = append(module.steps, confInitStep{variable: chain[i].variable})
	}

	for i := len(chain) - 1; i >= 0; i-- {
		if chain[i].sub != 0 {
			module.steps = append(module.steps, confInitStep{variable: envMODULE_TAG, value: chain[i].sub, original: true})
		}

		for _, call := range chain[i].body {
			module.steps, _ = t.callSteps(call.name, call.args.string(), module.steps, 0)
		}
	}

	for _, step := range module.steps {
		if step.expand {
			confTextRefs(step.value.string(), func(ENV) {})
		}
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

		if step.fallback || step.appended {
			readVar(step.variable)
		}

		if step.expand {
			confTextRefs(step.value.string(), readVar)
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
	platformEnv Environment
	inherited   DenseMap[ENV, ConfValue]
	base        *EnvStore
	bases       [][]ConfAssignment
	deepReads   [][]bool
	sysinclList []string
	recalcs     map[uint64][]confRecalc
	inputs      []uint64
	keys        DenseMap[STR, confStatementKey]
	dirs        map[string][]*confCommandDirs
	argMemo     map[STR][]*confArgsMemo
	argVolatile map[STR]bool
}

type confArgsMemo struct {
	vars   []ENV
	values []uint64
	args   []ANY
}

func confBinding(env Environment, key ENV) (uint64, STR) {
	kind, raw := env.s.lookup(key)

	if kind == envAbsent {
		return 0, 0
	}

	if kind != envStr {
		return uint64(kind)<<32 | uint64(raw), 0
	}

	return uint64(kind)<<32 | uint64(raw), raw
}

type confStatementKey struct {
	name  STR
	prio  uint64
	multi bool
	parse bool
}

func (c *Conf) statementKey(name STR) confStatementKey {
	if key, ok := c.keys.get(name); ok {
		return key
	}

	key, found := c.carrier(name, 0)

	key.prio = statementPriority(key.name)
	key.parse = !found

	c.keys.put(name, key)

	return key
}

func (c *Conf) carrier(name STR, depth int) (confStatementKey, bool) {
	if pluginMacroNames[name] {
		return confStatementKey{name: name, multi: true}, true
	}

	if builtinStatement(name) {
		return confStatementKey{name: name, multi: multiStatement(name)}, true
	}

	macro, present := c.program.macros[name]

	if present && macro == nil {
		return confStatementKey{name: name, multi: true}, true
	}

	if macro == nil {
		return confStatementKey{name: name}, true
	}

	if depth < confMacroCallStackSize {
		for _, call := range macro.calls {
			if baseMacro(call.name) || dirStatement(call.name) {
				continue
			}

			if key, ok := c.carrier(call.name, depth+1); ok {
				return key, true
			}
		}
	}

	if macro.command || macro.foreach {
		return confStatementKey{name: name, multi: macro.command}, true
	}

	return confStatementKey{name: name}, false
}

func baseMacro(name STR) bool {
	switch name {
	case kwSET, kwDEFAULT, strSetAppend, strSetAppendWithGlobal, strEnable, strDisable,
		strGlob, strLateGlob, strMessage, strNevercache:
		return true
	}

	return false
}

func dirStatement(name STR) bool {
	switch name {
	case kwPEERDIR, strGhostPeerdir, kwSRCDIR, kwADDINCL:
		return true
	}

	return false
}

func builtinStatement(name STR) bool {
	switch name {
	case kwPEERDIR, strGhostPeerdir, kwSRCDIR, kwADDINCL, kwINDUCED_DEPS, kwDECLARE_EXTERNAL_RESOURCE,
		kwDECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE, strDeclareExternalHostResourcesPack, strLateGlob,
		strPeerExport, strPeerQuery:
		return true
	}

	return false
}

type confRecalc struct {
	target   ENV
	original STR
	inputs   []uint64
	value    ConfValue
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
		if key := r.stmt.name; r.stmt.name != 0 && (r.file == root && r.stmt.branch != 0 || !platformEnv.hasBindingID(key)) && all.hasBindingID(key) {
			confStringBinding(all, key).apply(platformEnv, key)
		}
	}

	for _, key := range commands {
		clear(scratch.seen.words.s)
		bindConfCommandVariable(key, env, platformEnv, &scratch.seen)
	}

	tables := pm.cache.conf.tablesFor(program)
	c := &Conf{state: pm.cache.conf, program: program, tables: tables, platformEnv: platformEnv, bases: make([][]ConfAssignment, tables.moduleCount), deepReads: make([][]bool, tables.moduleCount)}

	c.sysinclList = strings.Fields(env.expandVariable(env.string(envSYSINCL)))

	tables.byTarget.idx.each(func(key, _ uint32) {
		c.inherited.put(ENV(key), confStringBinding(platformEnv, ENV(key)))
	})

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

func (c *Conf) setGuard(env Environment, macro *ConfMacro, originals *SetVars) {
	if macro != nil && macro.conditions && !env.bool(macro.guard) {
		env.setBool(macro.guard, true)
		c.set(env, macro.guard, originals)
	}
}

func (c *Conf) alias(name STR) *ConfMacro {
	return c.program.macros[name]
}

func (c *Conf) set(env Environment, variable ENV, originals *SetVars) {
	c.recalcInto(env, variable, originals, nil)
}

func (c *Conf) recalcInto(env Environment, variable ENV, originals *SetVars, recording []ConfAssignment) {
	targets, _ := c.tables.byVariable.get(variable)

	for _, target := range targets {
		original := originals.get(target.str())
		reads, memo := c.tables.recalcReads.get(target)

		var key uint64

		if memo {
			key = c.recalcKey(env, target, original, reads)

			if value, ok := c.recalcHit(key, target, original); ok {
				env.unbind(target)
				value.apply(env, target)

				if recording != nil {
					recordConfAssignment(recording, target, env)
				}

				continue
			}
		}

		env.unbind(target)

		stmts, _ := c.tables.byTarget.get(target)
		base, _ := c.inherited.get(target)

		if original != 0 {
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

		if memo {
			c.recalcPut(key, target, original, confEnvValue(env, target))
		}

		if recording != nil {
			recordConfAssignment(recording, target, env)
		}
	}
}

func (c *Conf) recalcKey(env Environment, target ENV, original STR, reads []ENV) uint64 {
	c.inputs = c.inputs[:0]

	for _, variable := range reads {
		kind, value := env.s.lookup(variable)

		if kind == envAbsent {
			value = 0
		}

		c.inputs = append(c.inputs, uint64(kind)<<32|uint64(value))
	}

	h := uint64(14695981039346656037)

	for _, x := range append(c.inputs, uint64(target)<<32|uint64(original)) {
		h = (h ^ x) * 1099511628211
	}

	return h
}

func (c *Conf) recalcHit(key uint64, target ENV, original STR) (ConfValue, bool) {
	for _, r := range c.recalcs[key] {
		if r.target == target && r.original == original && slices.Equal(r.inputs, c.inputs) {
			return r.value, true
		}
	}

	return ConfValue{}, false
}

func (c *Conf) recalcPut(key uint64, target ENV, original STR, value ConfValue) {
	if c.recalcs == nil {
		c.recalcs = map[uint64][]confRecalc{}
	}

	c.recalcs[key] = append(c.recalcs[key], confRecalc{target: target, original: original, inputs: slices.Clone(c.inputs), value: value})
}

func (c *Conf) module(name STR, tag string) *confModule {
	return c.tables.module(name, tag)
}

func (c *Conf) moduleAlias(module *confModule, name STR) STR {
	for ; module != nil; module = c.tables.module(module.parent, "") {
		if to, ok := module.aliases[name]; ok {
			return to
		}
	}

	return name
}

func (c *Conf) bodyCalls(module *confModule) []ConfMacroCall {
	if module.stmtsDone {
		return module.stmts
	}

	module.stmtsDone = true

	var chain []*confModule

	for base := module; base != nil; base = c.tables.module(base.parent, "") {
		chain = append(chain, base)
	}

	for i := len(chain) - 1; i >= 0; i-- {
		for _, call := range chain[i].body {
			if !baseMacro(call.name) && call.name != strGhostPeerdir && !module.limits.ignored.has(call.name) && !c.tables.varSteps(call.name, 0) {
				module.stmts = append(module.stmts, call)
			}
		}
	}

	return module.stmts
}

func (c *Conf) expandArgs(env Environment, text STR) []ANY {
	if !strHasDollar(text.any()) {
		return c.state.macroArgs(text)
	}

	if c.argVolatile[text] {
		return c.state.macroArgs(internStr(env.expandVariable(text.string())))
	}

	for _, memo := range c.argMemo[text] {
		hit := true

		for i, key := range memo.vars {
			if binding, _ := confBinding(env, key); binding != memo.values[i] {
				hit = false

				break
			}
		}

		if hit {
			return memo.args
		}
	}

	memo := &confArgsMemo{args: c.state.macroArgs(internStr(env.expandVariable(text.string())))}

	var visit func(ENV)

	visit = func(key ENV) {
		if slices.Contains(memo.vars, key) {
			return
		}

		binding, text := confBinding(env, key)

		memo.vars = append(memo.vars, key)
		memo.values = append(memo.values, binding)

		if text != 0 {
			confTextRefs(text.string(), visit)
		}
	}

	confTextRefs(text.string(), visit)

	if c.argMemo == nil {
		c.argMemo = map[STR][]*confArgsMemo{}
	}

	memos := c.argMemo[text]

	if len(memos) == confCommandDirsMemo {
		if c.argVolatile == nil {
			c.argVolatile = map[STR]bool{}
		}

		c.argVolatile[text] = true

		delete(c.argMemo, text)

		return memo.args
	}

	c.argMemo[text] = append(memos, memo)

	return memo.args
}

func (c *Conf) moduleTag(name STR) STR {
	module := c.tables.module(name, "")

	if module == nil {
		return 0
	}

	tag := STR(0)

	for _, step := range c.tables.plan(module).steps {
		if step.variable == envMODULE_TAG && step.value != 0 && !step.expand && !step.appended {
			tag = step.value
		}
	}

	return tag
}

func (c *Conf) initModule(env Environment, module *confModule, originals *SetVars) {
	defer func() { env.s.confBase = nil }()

	if module == nil {
		return
	}

	plan := c.tables.plan(module)
	base := env.s.confBase
	clean := base != nil && base.confBase == base

	if clean && c.base == nil {
		c.base = base
	}

	clean = clean && base == c.base

	if clean {
		read := c.deepRead(plan, base)

		for _, key := range env.s.confWrites {
			if int(key) < len(read) && read[key] {
				clean = false

				break
			}
		}
	}

	if clean && originals.len() != 0 {
		for _, key := range plan.originals {
			if originals.has(key.str()) {
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

			if assignment.stored {
				originals.set(assignment.key.str(), assignment.original)
			}

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

func (c *Conf) deepRead(plan *confModule, base *EnvStore) []bool {
	if read := c.deepReads[plan.index]; read != nil {
		return read
	}

	read := slices.Clone(plan.read)

	var visit func(ENV)

	visit = func(key ENV) {
		if int(key) >= len(read) {
			read = append(read, make([]bool, int(key)+1-len(read))...)
		}

		if read[key] {
			return
		}

		read[key] = true

		if _, raw := base.lookup(key); raw != 0 {
			confTextRefs(raw.string(), visit)
		}
	}

	for key, direct := range plan.read {
		if direct {
			if _, raw := base.lookup(ENV(key)); raw != 0 {
				confTextRefs(raw.string(), visit)
			}
		}
	}

	c.deepReads[plan.index] = read

	return read
}

func (c *Conf) runInit(plan *confModule, env Environment, originals *SetVars, recording []ConfAssignment) {
	for _, step := range plan.steps {
		switch {
		case step.fallback && env.string(step.variable) != "":
			if recording != nil {
				recordConfAssignment(recording, step.variable, env)
			}

			c.recalcInto(env, step.variable, originals, recording)

			continue
		case step.appended:
			value := step.value.string()

			if step.expand {
				value = env.expandVariable(value)
			}

			if old := env.string(step.variable); old != "" {
				value = old + " " + value
			}

			env.setStringID(step.variable, internStr(value))
		case step.expand:
			env.setStringID(step.variable, internStr(env.expandVariable(step.value.string())))
		case step.value != 0:
			env.setStringID(step.variable, step.value)
		case step.source != 0:
			env.setString(step.variable, env.string(step.source))
		default:
			env.setBool(step.variable, true)
		}

		if step.original {
			value := internStr(env.string(step.variable))

			originals.set(step.variable.str(), value)

			if recording != nil {
				recordConfOriginal(recording, step.variable, value)
			}
		}

		if recording != nil {
			recordConfAssignment(recording, step.variable, env)
		}

		c.recalcInto(env, step.variable, originals, recording)
	}
}

func recordConfOriginal(recording []ConfAssignment, key ENV, value STR) {
	for i := range recording {
		if recording[i].key == key {
			recording[i].stored, recording[i].original = true, value

			return
		}
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
	name  STR
	args  STR
	macro *ConfMacro
}

type ConfMacro struct {
	file       string
	sig        ConfMacroSignature
	calls      []ConfMacroCall
	conditions bool
	dirs       bool
	command    bool
	foreach    bool
	guard      ENV
	locals     []confMacroLocal
	expansions *IntMap[*confMacroExpansion]
}

type confMacroLocal struct {
	name  STR
	value ANY
}

type confMacroBody struct {
	header     string
	locals     []confMacroLocal
	calls      []ConfMacroCall
	cmd        string
	peerdir    string
	addincl    string
	conditions bool
	command    bool
	foreach    bool
}

func (f *ConfFile) addMacro(header string, branch int32, body *confMacroBody) {
	name, params, opened := strings.Cut(strings.TrimPrefix(header, "macro "), "(")
	params, closed := strings.CutSuffix(strings.TrimSpace(params), ")")

	if !opened || !closed {
		throwFmt("conf: %s: malformed macro header %q", f.rel, header)
	}

	sig, specialized := f.state.parseMacroSignature(f.rel, params)
	key := internStr(strings.TrimSpace(name))
	calls := body.calls

	if body.cmd != "" || body.peerdir != "" || body.addincl != "" {
		command := key.string()

		if specialized {
			command += ":" + strings.Trim(splitConfMacroParams(params, nil)[0], `"`)
		}

		if f.commands == nil {
			f.commands = map[string]*confCommand{}
		}

		if _, ok := f.commands[command]; !ok {
			f.commands[command] = &confCommand{cmd: body.cmd, peerdir: body.peerdir, addincl: body.addincl}
		}
	}

	if !specialized && len(calls) != 0 {
		if f.inits == nil {
			f.inits = map[STR]*ConfMacro{}
		}

		if _, ok := f.inits[key]; !ok {
			f.inits[key] = &ConfMacro{file: f.rel, sig: sig, calls: calls, locals: body.locals}
		}
	}

	for _, param := range f.macroConditionParams(branch, sig) {
		calls = append(calls[:len(calls):len(calls)], ConfMacroCall{name: kwSET, args: internV(param, " $", param)})
	}

	if !specialized && len(calls) == 0 && !body.conditions && !body.command && !body.foreach {
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
		f.macros[key] = &ConfMacro{file: f.rel, sig: sig, calls: calls, locals: body.locals, conditions: body.conditions, command: body.command, foreach: body.foreach, guard: internEnv("env" + key.string())}
	}
}

func (body *confMacroBody) addLine(line string) {
	if strings.HasPrefix(line, ".") {
		body.command = body.command || strings.HasPrefix(line, ".CMD=") || strings.HasPrefix(line, ".CMD =")

		if property, value, ok := strings.Cut(line, "="); ok {
			switch strings.TrimSpace(property) {
			case ".CMD":
				body.cmd = strings.TrimSpace(value)
			case ".PEERDIR":
				body.peerdir = strings.TrimSpace(value)
			case ".ADDINCL":
				body.addincl = strings.TrimSpace(value)
			}
		}

		return
	}

	if strings.HasPrefix(line, "foreach") {
		body.foreach = true

		return
	}

	name, args, opened := strings.Cut(line, "(")

	name = strings.TrimSpace(name)

	args, closed := strings.CutSuffix(args, ")")

	if opened && closed && isIdentShapedName(name) {
		body.calls = append(body.calls, ConfMacroCall{name: internStr(name), args: internStr(strings.TrimSpace(args))})
	}
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

	panic(exceptionf("conf: %s: unterminated macro parameter value %q", rel, value))
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
	key := uint64(1)

	if len(args) > 0 {
		key = xxh3.Hash(sliceBytes(args)) | 1
	}

	if macro.expansions == nil {
		macro.expansions = newIntMap[*confMacroExpansion](4)
	}

	cell, _ := macro.expansions.cell(key)

	for expansion := *cell; expansion != nil; expansion = expansion.next {
		if slices.Equal(expansion.args, args) {
			return expansion
		}
	}

	expansion := s.expansions.one()

	expansion.next = *cell
	expansion.args = astArgs.list(args...)
	expansion.locals, expansion.failure = macro.sig.bind(expansion.args, s.bindings)

	if len(macro.locals) > 0 {
		n := len(expansion.locals) + len(macro.locals)
		locals := s.bindings.alloc(n)[:n:n]

		s.bindings.commit(n)
		copy(locals, expansion.locals)

		for i, local := range macro.locals {
			locals[len(expansion.locals)+i] = confMacroBinding{name: local.name, values: astArgs.list(local.value), bound: true}
		}

		expansion.locals = locals
	}

	expansion.calls = s.calls.alloc(len(macro.calls))[:len(macro.calls):len(macro.calls)]
	s.calls.commit(len(macro.calls))
	*cell = expansion

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

func expandConfMacro(ctx *GenCtx, modulePath string, kind ModuleKind, language Language, v UnknownStmt, handled bool, env Environment, d *ModuleData, onWarn func(Warn), platform *Platform) {
	macro := platform.conf.alias(v.Raw)

	switch {
	case macro != nil:
	case handled:
		return
	case v.Name == tokInvalid:
		onWarn(Warn{Kind: WarnUnknownMacro, Message: fmt.Sprintf("%s: macro %q is not in the modelled set (line %d); skipped", modulePath, v.Raw.string(), v.Line)})

		return
	default:
		warnUnmodelledMacro(onWarn, modulePath, v.Name)

		return
	}

	if handled && !macro.dirs {
		d.deferBodyGuards(platform, macro, 0)

		return
	}

	platform.conf.macroCalls(macro, v, env, &d.confMacroDepth, onWarn, modulePath, func(name STR, args []ANY) {
		if baseMacro(name) && (handled || d.interpreting) || dirStatement(name) && d.interpreting {
			return
		}

		if handled && !baseMacro(name) && !dirStatement(name) {
			d.deferGuard(platform, name)
			expandConfMacro(ctx, modulePath, kind, language, UnknownStmt{Name: internTokMaybeSTR(name), Raw: name, Args: args, Line: v.Line}, true, env, d, onWarn, platform)

			return
		}

		var scratch [1]Stmt

		stmts := appendMacroCallStmts(scratch[:0], name, args, args, v.Line, func(format string, a ...any) {
			throwFmt("%s: %s: "+format, append([]any{macro.file, name.string()}, a...)...)
		})

		markImplicitPeers(stmts)
		collectStmts(ctx, modulePath, kind, language, stmts, env, d, onWarn, platform)
	})
}

func (d *ModuleData) deferBodyGuards(platform *Platform, macro *ConfMacro, depth int) {
	if depth == confMacroCallStackSize {
		throwFmt("config error: recursion in the calls of macro %s", macro.file)
	}

	for _, call := range macro.calls {
		if baseMacro(call.name) {
			continue
		}

		d.deferGuardOf(platform, call.name, call.macro)

		if call.macro != nil {
			d.deferBodyGuards(platform, call.macro, depth+1)
		}
	}
}

func markImplicitPeers(stmts []Stmt) {
	for _, s := range stmts {
		if p, ok := s.(*PeerdirStmt); ok {
			p.Implicit = true
		}
	}
}

func (c *Conf) macroCalls(macro *ConfMacro, v UnknownStmt, env Environment, depth *int, onWarn func(Warn), modulePath string, visit func(name STR, args []ANY)) {
	if *depth == confMacroCallStackSize {
		throwFmt("%s: config error: recursion in the calls of macro %s", modulePath, v.Raw.string())
	}

	state := c.state
	expansion := state.bind(macro, v.Args)
	locals, failure := expansion.locals, expansion.failure

	if failure != "" {
		onWarn(Warn{Kind: WarnBadMacroArgs, Message: fmt.Sprintf("%s: macro '%s': %s\n\tArgs: %s", modulePath, v.Raw.string(), failure, strings.Join(anyStrs(v.Args), ", "))})
	}

	*depth++

	defer func() { *depth-- }()

	for i, call := range macro.calls {
		cached := &expansion.calls[i]
		args := cached.args

		if !cached.constant {
			text := call.args
			constant := true

			if strHasDollar(text.any()) {
				text = internStr(env.subst(text.string(), locals, &constant))
			}

			args = state.macroArgs(text)

			if constant {
				cached.args, cached.constant = args, true
			}
		}

		visit(call.name, args)
	}
}

func (s *ConfState) macroArgs(text STR) []ANY {
	if args, ok := s.args.get(text); ok {
		return args
	}

	value := text.string()
	block := astArgs.alloc(strings.Count(value, " ") + 1)
	n := 0

	for arg := range macroArgsSeq(value) {
		block[n] = internStr(arg).any()
		n++
	}

	astArgs.commit(n)

	args := block[:n:n]

	s.args.put(text, args)

	return args
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

type confCommandDirs struct {
	vars     []ENV
	values   []confRawValue
	peerdirs []string
	addincls []string
}

type confRawValue struct {
	kind  EnvKind
	value STR
}

func (c *Conf) commandDirs(name string, env Environment) *confCommandDirs {
	for _, memo := range c.dirs[name] {
		if memo.valid(env) {
			return memo
		}
	}

	memo := &confCommandDirs{}
	seen := map[string]bool{}

	var walk func(text string, depth int)

	call := func(macro string, depth int) {
		command := c.program.commands[macro]

		if command == nil || seen["("+macro] || depth > confMacroCallStackSize {
			return
		}

		seen["("+macro] = true

		if command.peerdir != "" {
			memo.peerdirs = append(memo.peerdirs, strings.Fields(memo.expand(command.peerdir, env))...)
		}

		if command.addincl != "" {
			memo.addincls = append(memo.addincls, strings.Fields(memo.expand(command.addincl, env))...)
		}

		walk(command.cmd, depth+1)
	}

	walk = func(text string, depth int) {
		scanCommandRefs(text, func(ident string, isCall bool) {
			switch {
			case isCall:
				call(ident, depth)
			case c.program.commandVars[ident]:
				walk(memo.read(ident, env, seen), depth+1)
			}
		})
	}

	call(name, 0)

	if c.dirs == nil {
		c.dirs = map[string][]*confCommandDirs{}
	}

	memos := c.dirs[name]

	if len(memos) == confCommandDirsMemo {
		memos = memos[1:]
	}

	c.dirs[name] = append(memos, memo)

	return memo
}

func (m *confCommandDirs) read(name string, env Environment, seen map[string]bool) string {
	if seen[name] {
		return ""
	}

	seen[name] = true

	key := internEnv(name)

	m.record(key, env)

	value := env.string(key)

	if strings.IndexByte(value, '$') < 0 {
		return ""
	}

	return value
}

func (m *confCommandDirs) record(key ENV, env Environment) {
	kind, value := env.s.lookup(key)

	m.vars = append(m.vars, key)
	m.values = append(m.values, confRawValue{kind, value})
}

func (m *confCommandDirs) expand(text string, env Environment) string {
	confTextRefs(text, func(key ENV) {
		m.record(key, env)
	})

	return env.expandVariable(text)
}

func (m *confCommandDirs) valid(env Environment) bool {
	for i, key := range m.vars {
		if kind, value := env.s.lookup(key); kind != m.values[i].kind || value != m.values[i].value {
			return false
		}
	}

	return true
}
