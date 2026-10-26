package main

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	confScalarRefRe = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}|\$[A-Za-z_][A-Za-z0-9_]*`)
	confVarRefRe    = regexp.MustCompile(`\$\{(?:[^}:]*:)?([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	confInRe        = regexp.MustCompile(`^\(\s*(\$[A-Za-z_][A-Za-z0-9_]*|"[^"]*")\s+in\s+\[(.*)\]\s*\)$`)
	confInItemRe    = regexp.MustCompile(`^\s*("[^"]*")\s*(?:,|$)`)
)

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
	name   string
	value  string
	op     ConfOp
}

type ConfFile struct {
	rel      string
	branches []ConfBranch
	stmts    []ConfStmt
	macros   map[STR]*ConfMacro
	modules  map[string]string
	variants map[string][]string
}

func readConfFile(rel, text string) *ConfFile {
	conf := &ConfFile{rel: rel, branches: []ConfBranch{{}}}
	stack := []int{0}
	selects := []string{""}
	skipped := []bool{false}
	blocks := []string{""}
	owners := []string{""}
	previous := 0
	pending := ""
	lines := strings.Split(text, "\n")
	macroBodies := map[int]*confMacroBody{}

	for _, raw := range lines {
		line := strings.TrimSpace(raw)

		if pending != "" {
			line = pending + line
			pending = ""
		} else if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasSuffix(line, "\\") {
			pending = strings.TrimSuffix(line, "\\") + " "

			continue
		}

		if body := macroBodies[stack[len(stack)-1]]; body != nil && !strings.HasPrefix(line, "}") {
			body.addLine(line)
		}

		for strings.HasPrefix(line, "}") {
			if len(stack) == 1 {
				throwFmt("conf: unmatched } in %s", rel)
			}

			previous = stack[len(stack)-1]

			if body := macroBodies[previous]; body != nil {
				conf.addMacro(body.header, body.calls)
			}

			stack = stack[:len(stack)-1]
			line = strings.TrimSpace(line[1:])
		}

		if strings.HasPrefix(line, "@import") {
			conf.stmts = append(conf.stmts, ConfStmt{value: confImportPath(rel, line)})

			continue
		}

		top := stack[len(stack)-1]

		if name, value, op, ok := confStmtAssignment(line); ok {
			if !skipped[top] && blocks[top] != "macro" {
				conf.stmts = append(conf.stmts, ConfStmt{branch: top, name: name, value: value, op: op})
			}

			continue
		}

		if !strings.HasSuffix(line, "{") {
			continue
		}

		header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		branch := ConfBranch{parent: top, cond: condFalse}
		selectVar := ""
		block := ""
		owner := owners[top]

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
			branch.expr = `($env` + name + ` == "yes")`
			block = "module"
		case strings.HasPrefix(header, "macro "):
			name, _, _ := strings.Cut(strings.TrimPrefix(header, "macro "), "(")

			branch.expr = `($env` + name + ` == "yes")`
			block = "macro"
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
		case selects[top] != "" && strings.HasSuffix(header, "?"):
			branch.previous = previous
			branch.cond = nil

			if alternatives := strings.TrimSpace(strings.TrimSuffix(header, "?")); alternatives != "default" {
				branch.expr = confSelectCondition(selects[top], alternatives)
			}
		}

		skip := skipped[branch.parent] || branch.expr == "" && branch.cond != nil

		if branch.expr != "" {
			branch.cond = nil
		}

		conf.branches = append(conf.branches, branch)
		blocks = append(blocks, block)
		owners = append(owners, owner)
		skipped = append(skipped, skip)
		selects = append(selects, selectVar)
		stack = append(stack, len(conf.branches)-1)

		if strings.HasPrefix(header, "macro ") {
			macroBodies[len(conf.branches)-1] = &confMacroBody{header: header}
		}

		previous = 0
	}

	if len(stack) != 1 || pending != "" {
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
	expr = strings.NewReplacer("&&", " AND ", "||", " OR ", "!=", "!=", "!", " NOT ").Replace(expr)

	p := &Parser{lex: &Lexer{name: rel, src: []byte(expr), line: 1, col: 1}}
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

	return p.condScratch
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

func (c *ConfFile) condVars(b int, add func(string)) {
	for b != 0 {
		for p := b; p != 0; p = c.branches[p].previous {
			for _, n := range c.cond(p) {
				if n.Kind == ckConfIdent {
					add(n.Name)
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
	key := internEnv(s.name)
	bound := env.hasBindingID(key)

	switch s.op {
	case confSet:
		env.setString(key, s.value)
	case confAppend:
		if cur := env.string(key); bound && cur != "" {
			env.setString(key, cur+" "+s.value)
		} else {
			env.setString(key, s.value)
		}
	case confDefine:
		if !bound {
			env.setString(key, s.value)
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
