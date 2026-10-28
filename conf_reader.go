package main

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	confCondReplacer = strings.NewReplacer("&&", " AND ", "||", " OR ", "!=", "!=", "!", " NOT ")
	confCondParser   Parser
	confCondLexer    Lexer
	confCondNodes    = newBumpAllocator[CondNode]()
	confScalarRefRe  = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}|\$[A-Za-z_][A-Za-z0-9_]*`)
	confVarRefRe     = regexp.MustCompile(`\$\{(?:[^}:]*:)?([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	confInRe         = regexp.MustCompile(`^\(\s*(\$[A-Za-z_][A-Za-z0-9_]*|"[^"]*")\s+in\s+\[(.*)\]\s*\)$`)
	confInItemRe     = regexp.MustCompile(`^\s*("[^"]*")\s*(?:,|$)`)
)

var confReadScratch struct {
	frames  []confParseFrame
	pending []byte
}

const (
	confRootRel    = "build/"
	confRootImport = `@import "${CONF_ROOT}/`
)

const (
	confSet ConfOp = iota
	confAppend
	confDefine
	confExclude
)

type ConfOp uint8

type ConfBranch struct {
	parent   int
	previous int
	expr     string
	cond     []CondNode
}

type ConfStmt struct {
	branch int
	name   ENV
	scalar ConfValue
	value  string
	op     ConfOp
}

type ConfFile struct {
	program  *ConfProgram
	rel      string
	branches []ConfBranch
	stmts    []ConfStmt
	macros   map[STR]*ConfMacro
	modules  map[string]string
	variants map[string][]string
	calls    map[string][]string
}

type confParseFrame struct {
	branch    int
	selectVar string
	skipped   bool
	block     string
	owner     string
	guard     string
	macro     bool
	body      confMacroBody
}

func readConfFile(rel, text string) *ConfFile {
	conf := &ConfFile{rel: rel, branches: []ConfBranch{{}}}
	stack := append(confReadScratch.frames[:0], confParseFrame{})
	pending := confReadScratch.pending[:0]
	previous := 0

	defer func() {
		clear(stack)
		confReadScratch.frames = stack[:0]
		confReadScratch.pending = pending[:0]
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
				if conf.calls == nil {
					conf.calls = map[string][]string{}
				}

				conf.calls[top.guard] = append(conf.calls[top.guard], internV("env", strings.TrimSpace(name)).string())
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

		frame := confParseFrame{branch: len(conf.branches) - 1, selectVar: selectVar, skipped: skip, block: block, owner: owner, guard: guard}

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

func parseConfCondition(rel, expr string) []CondNode {
	expr = confInCondition(rel, expr)
	expr = confCondReplacer.Replace(expr)

	confCondLexer = Lexer{name: rel, src: append(confCondLexer.src[:0], expr...), line: 1, col: 1}

	p := &confCondParser

	p.lex = &confCondLexer
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

	block := confCondNodes.alloc(len(p.condScratch))

	copy(block, p.condScratch)
	confCondNodes.commit(len(p.condScratch))

	return block[:len(p.condScratch):len(p.condScratch)]
}

func confInCondition(rel, expr string) string {
	match := confInRe.FindStringSubmatch(strings.TrimSpace(expr))

	if match == nil {
		return expr
	}

	var alternatives []string

	for rest := match[2]; strings.TrimSpace(rest) != ""; {
		item := confInItemRe.FindStringSubmatch(rest)

		if item == nil {
			throwFmt("conf: %s: unsupported condition %s", rel, expr)
		}

		alternatives = append(alternatives, match[1]+" == "+item[1])
		rest = rest[len(item[0]):]
	}

	if len(alternatives) == 0 {
		throwFmt("conf: %s: unsupported condition %s", rel, expr)
	}

	return "(" + strings.Join(alternatives, " || ") + ")"
}

func evalConfAtomString(nodes []CondNode, i int32, env Environment) string {
	n := &nodes[i]

	switch n.Kind {
	case ckConfIdent:
		return env.string(n.Env)
	case ckString:
		return n.Name
	case ckInt:
		return strconv.Itoa(n.Ival)
	}

	throwFmt("macros: unexpected cond kind %d in comparator operand position", n.Kind)

	return ""
}

func evalConfEq(nodes []CondNode, n *CondNode, env Environment) bool {
	if confStringOperand(&nodes[n.L]) && confStringOperand(&nodes[n.R]) {
		return evalConfAtomString(nodes, n.L, env) == evalConfAtomString(nodes, n.R, env)
	}

	return confNumber(nodes, n.L, env) == confNumber(nodes, n.R, env)
}

func evalConfLt(nodes []CondNode, n *CondNode, env Environment) bool {
	if confStringOperand(&nodes[n.L]) && confStringOperand(&nodes[n.R]) {
		return evalConfAtomString(nodes, n.L, env) < evalConfAtomString(nodes, n.R, env)
	}

	return confNumber(nodes, n.L, env) < confNumber(nodes, n.R, env)
}

func confStringOperand(n *CondNode) bool {
	return n.Kind == ckConfIdent || n.Kind == ckString
}

func confNumber(nodes []CondNode, i int32, env Environment) int {
	n := &nodes[i]

	switch n.Kind {
	case ckInt:
		return n.Ival
	case ckConfIdent, ckString:
		if evalConfAtomString(nodes, i, env) != "" {
			return 1
		}

		return 0
	}

	if evalCondAt(nodes, i, env) {
		return 1
	}

	return 0
}

func (c *ConfFile) cond(b int) []CondNode {
	branch := &c.branches[b]

	if branch.expr != "" && branch.cond == nil {
		branch.cond = parseConfCondition(c.rel, branch.expr)
	}

	return branch.cond
}

func (c *ConfFile) condHolds(b int, env Environment) bool {
	cond := c.cond(b)

	return len(cond) == 0 || evalCond(cond, env)
}

func (c *ConfFile) holds(b int, env Environment) bool {
	for b != 0 {
		if !c.condHolds(b, env) {
			return false
		}

		for p := c.branches[b].previous; p != 0; p = c.branches[p].previous {
			if c.condHolds(p, env) {
				return false
			}
		}

		b = c.branches[b].parent
	}

	return true
}

func (c *ConfFile) condVars(b int, add func(ENV)) {
	for b != 0 {
		for p := b; p != 0; p = c.branches[p].previous {
			for _, n := range c.cond(p) {
				if n.Kind == ckConfIdent {
					add(n.Env)
				}
			}
		}

		b = c.branches[b].parent
	}
}

func confValueVars(value string, add func(string)) {
	if !strings.Contains(value, "$") {
		return
	}

	for _, m := range confVarRefRe.FindAllStringSubmatch(value, -1) {
		if m[1] != "" {
			add(m[1])
		} else {
			add(m[2])
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

func expandConfScalar(value string, env Environment) string {
	if !strings.Contains(value, "$") {
		return value
	}

	for iteration := 1; value != ""; iteration++ {
		expanded := confScalarRefRe.ReplaceAllStringFunc(value, func(reference string) string {
			return env.string(internEnv(strings.Trim(reference, "${}")))
		})

		if iteration >= 128 {
			throwFmt("conf: scalar substitution exceeded the limit 128")
		}

		if expanded == value {
			return value
		}

		value = expanded
	}

	return value
}
