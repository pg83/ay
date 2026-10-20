package main

import "strings"

type ConfBranch struct {
	parent   int
	previous int
	cond     []CondNode
}

type ConfAssignment struct {
	branch int
	name   string
	value  string
	append bool
}

type ConfAssignments struct {
	branches    []ConfBranch
	assignments []ConfAssignment
}

func readConfAssignments(fs FS, filename string, data []byte, selected []string) *ConfAssignments {
	conf := &ConfAssignments{branches: []ConfBranch{{}}}
	names := make(map[string]bool, len(selected))

	for _, name := range selected {
		names[name] = true
	}

	stack := []int{0}
	expressions := []string{""}
	needed := []bool{true}
	skipped := []bool{false}
	previous := 0
	pending := ""

	var read func(string, []byte)

	reading := map[string]bool{}

	read = func(filename string, data []byte) {
		if reading[filename] {
			throwFmt("conf: import cycle at %s", filename)
		}

		reading[filename] = true

		defer delete(reading, filename)

		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)

			line = pending + line
			pending = ""

			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			if strings.HasSuffix(line, "\\") {
				pending = strings.TrimSuffix(line, "\\") + " "

				continue
			}

			for strings.HasPrefix(line, "}") {
				if len(stack) == 1 {
					throwFmt("conf: unmatched } in %s", filename)
				}

				previous = stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				line = strings.TrimSpace(line[1:])
			}

			if imported, ok := confImport(line); ok && !skipped[stack[len(stack)-1]] {
				if fs.isFile(srcRootRel, imported) {
					read(imported, concatChunks(fs.read(imported)))
				}

				continue
			}

			if name, value, appendOp, ok := confBranchAssignment(line); ok {
				if names[name] && !skipped[stack[len(stack)-1]] {
					needed[stack[len(stack)-1]] = true

					conf.assignments = append(conf.assignments, ConfAssignment{
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
	}

	read(filename, data)

	if len(stack) != 1 || pending != "" {
		throwFmt("conf: unfinished block or assignment in %s", filename)
	}

	for i := len(conf.branches) - 1; i > 0; i-- {
		if needed[i] {
			needed[conf.branches[i].parent] = true
			needed[conf.branches[i].previous] = true
		}
	}

	for i, expr := range expressions {
		if needed[i] && expr != "" {
			conf.branches[i].cond = parseConfBranchCondition(filename, expr)
		}
	}

	return conf
}

func confBranchAssignment(line string) (name, value string, appendOp, ok bool) {
	name, value, ok = strings.Cut(line, "=")
	name = strings.TrimSpace(name)
	name, appendOp = strings.CutSuffix(name, "+")
	name = strings.TrimSpace(name)

	return name, strings.TrimSpace(value), appendOp, ok && name != "" && isConfVarName(name)
}

func parseConfBranchCondition(filename, expr string) []CondNode {
	expr = strings.NewReplacer("$", "", "&&", " AND ", "||", " OR ", "!=", "!=", "!", " NOT ").Replace(expr)

	p := &Parser{lex: &Lexer{name: filename, src: []byte(expr), line: 1, col: 1}}
	tok := Token{line: 1, col: 1}
	cp := CondParser{parent: p, ifTok: tok, toks: p.readCondTokens(tok)}

	cp.parseOr()

	if cp.pos != len(cp.toks) {
		throwFmt("conf: unsupported condition %s", expr)
	}

	return p.condScratch
}

func confImport(line string) (string, bool) {
	value, ok := strings.CutPrefix(line, "@import ")

	if !ok {
		return "", false
	}

	value = strings.Trim(strings.TrimSpace(value), "\"")
	value, ok = strings.CutPrefix(value, "${CONF_ROOT}/")

	return "build/" + value, ok
}

func (c *ConfAssignments) apply(env Environment) {
	active := make([]bool, len(c.branches))
	matched := make([]bool, len(c.branches))

	active[0] = true

	for i, b := range c.branches[1:] {
		active[i+1] = active[b.parent] && !matched[b.previous] && (len(b.cond) == 0 || evalCond(b.cond, env))
		matched[i+1] = active[i+1] || matched[b.previous]
	}

	for _, a := range c.assignments {
		if !active[a.branch] {
			continue
		}

		key := internEnv(a.name)
		value := a.value

		if a.append {
			value = env.string(key) + " " + value
		}

		env.setString(key, value)
	}
}
