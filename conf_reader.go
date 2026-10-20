package main

import "strings"

const (
	confRootRel    = "build/"
	confRootImport = `@import "${CONF_ROOT}/`
)

type ConfBranch struct {
	parent   int
	previous int
	cond     []CondNode
}

type ConfStmt struct {
	branch int
	name   string
	value  string
	append bool
}

type ConfFile struct {
	branches []ConfBranch
	stmts    []ConfStmt
}

func readConfFile(rel, text string, names map[string]bool) *ConfFile {
	conf := &ConfFile{branches: []ConfBranch{{}}}
	stack := []int{0}
	expressions := []string{""}
	needed := []bool{true}
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

		if name, value, appendOp, ok := confStmtAssignment(line); ok {
			if names[name] && !skipped[stack[len(stack)-1]] {
				needed[stack[len(stack)-1]] = true

				conf.stmts = append(conf.stmts, ConfStmt{
					branch: stack[len(stack)-1], name: name, value: value, append: appendOp,
				})
			}

			continue
		}

		if !strings.HasSuffix(line, "{") {
			continue
		}

		header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		branch := ConfBranch{parent: stack[len(stack)-1], cond: condFalse}
		expr := ""

		switch {
		case strings.HasPrefix(header, "when "):
			expr = strings.TrimSpace(strings.TrimPrefix(header, "when"))
		case strings.HasPrefix(header, "elsewhen "):
			branch.previous = previous
			expr = strings.TrimSpace(strings.TrimPrefix(header, "elsewhen"))
		case header == "otherwise":
			branch.previous = previous
			branch.cond = nil
		}

		conf.branches = append(conf.branches, branch)
		expressions = append(expressions, expr)
		needed = append(needed, false)
		skipped = append(skipped, skipped[branch.parent] || expr == "" && branch.cond != nil)
		stack = append(stack, len(conf.branches)-1)
	}

	if len(stack) != 1 || pending != "" {
		throwFmt("conf: unfinished block or assignment in %s", rel)
	}

	for i := len(conf.branches) - 1; i > 0; i-- {
		if needed[i] {
			needed[conf.branches[i].parent] = true
			needed[conf.branches[i].previous] = true
		}
	}

	for i, expr := range expressions {
		if needed[i] && expr != "" {
			conf.branches[i].cond = parseConfCondition(rel, expr)
		}
	}

	return conf
}

func confImportPath(rel, line string) string {
	path, rooted := strings.CutPrefix(line, confRootImport)
	path, quoted := strings.CutSuffix(path, `"`)

	if !rooted || !quoted {
		throwFmt("conf: %s: unsupported %s", rel, line)
	}

	return confRootRel + path
}

func confStmtAssignment(line string) (name, value string, appendOp, ok bool) {
	name, value, ok = strings.Cut(line, "=")
	name = strings.TrimSpace(name)
	name, appendOp = strings.CutSuffix(name, "+")
	name = strings.TrimSpace(name)

	return name, strings.TrimSpace(value), appendOp, ok && name != "" && isConfVarName(name)
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

func (c *ConfFile) active(env Environment) []bool {
	active := make([]bool, len(c.branches))
	matched := make([]bool, len(c.branches))

	active[0] = true

	for i, b := range c.branches[1:] {
		active[i+1] = active[b.parent] && !matched[b.previous] && (len(b.cond) == 0 || evalCond(b.cond, env))
		matched[i+1] = active[i+1] || matched[b.previous]
	}

	return active
}
