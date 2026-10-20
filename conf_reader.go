package main

import (
	"regexp"
	"strings"
)

var confVarRefRe = regexp.MustCompile(`\$\{(?:[^}:]*:)?([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

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
}

func readConfFile(rel, text string) *ConfFile {
	conf := &ConfFile{rel: rel, branches: []ConfBranch{{}}}
	stack := []int{0}
	selects := []string{""}
	skipped := []bool{false}
	previous := 0
	pending := ""

	for _, raw := range strings.Split(text, "\n") {
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

		for strings.HasPrefix(line, "}") {
			if len(stack) == 1 {
				throwFmt("conf: unmatched } in %s", rel)
			}

			previous = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			line = strings.TrimSpace(line[1:])
		}

		if strings.HasPrefix(line, "@import") {
			conf.stmts = append(conf.stmts, ConfStmt{value: confImportPath(rel, line)})

			continue
		}

		top := stack[len(stack)-1]

		if name, value, op, ok := confStmtAssignment(line); ok {
			if !skipped[top] {
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

		switch {
		case strings.HasPrefix(header, "when "):
			branch.expr = strings.TrimSpace(strings.TrimPrefix(header, "when"))
		case strings.HasPrefix(header, "elsewhen "):
			branch.previous = previous
			branch.expr = strings.TrimSpace(strings.TrimPrefix(header, "elsewhen"))
		case header == "otherwise":
			branch.previous = previous
			branch.cond = nil
		case strings.HasPrefix(header, "select "):
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
		skipped = append(skipped, skip)
		selects = append(selects, selectVar)
		stack = append(stack, len(conf.branches)-1)
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
	expr = strings.NewReplacer("$", "", "&&", " AND ", "||", " OR ", "!=", "!=", "!", " NOT ").Replace(expr)

	p := &Parser{lex: &Lexer{name: rel, src: []byte(expr), line: 1, col: 1}}
	tok := Token{line: 1, col: 1}
	cp := CondParser{parent: p, ifTok: tok, toks: p.readCondTokens(tok)}

	cp.parseOr()

	if cp.pos != len(cp.toks) {
		throwFmt("conf: %s: unsupported condition %s", rel, expr)
	}

	return p.condScratch
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
				if n.Kind == ckIdent && n.Name != "yes" && n.Name != "no" {
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
