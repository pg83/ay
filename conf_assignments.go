package main

import "strings"

type ConfBranch struct {
	parent   int
	previous int
	cond     []CondNode
}

type ConfAssignment struct {
	imported *ConfAssignments
	branch   int
	name     string
	value    string
	append   bool
}

type ConfAssignments struct {
	branches    []ConfBranch
	assignments []ConfAssignment
}

func (cache *SharedParseCache) confFile(fs FS, filename string) *ConfAssignments {
	key := internStr(filename)

	if conf, ok := cache.confFiles.get(key); ok {
		if conf == nil {
			throwFmt("conf: import cycle at %s", filename)
		}

		return conf
	}

	if !fs.isFile(srcRootRel, filename) {
		throwFmt("conf: imported configuration %s is missing", filename)
	}

	cache.confFiles.put(key, nil)

	conf := readConfAssignments(fs, filename, concatChunks(fs.read(filename)), cache)

	cache.confFiles.put(key, conf)

	return conf
}

func readConfAssignments(fs FS, filename string, data []byte, cache *SharedParseCache) *ConfAssignments {
	conf := &ConfAssignments{branches: []ConfBranch{{}}}
	selected := append([]string{"SYSINCL"}, linkerConfNames...)
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
			if len(stack) != 1 {
				throwFmt("conf: @import inside a condition in %s", filename)
			}

			conf.assignments = append(conf.assignments, ConfAssignment{imported: cache.confFile(fs, imported)})

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

func (c *ConfAssignments) apply(env Environment, only string) {
	c.applyPass(env, false, only)
	c.applyPass(env, true, only)
}

func (c *ConfAssignments) applyPass(env Environment, conditional bool, only string) {
	for _, a := range c.assignments {
		if a.imported != nil {
			a.imported.applyPass(env, conditional, only)

			continue
		}

		if only != "" && a.name != only || conditional != (a.branch != 0) || !c.branchActive(a.branch, env) {
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

func (c *ConfAssignments) branchActive(index int, env Environment) bool {
	if index == 0 {
		return true
	}

	branch := c.branches[index]

	return c.branchActive(branch.parent, env) && !c.priorBranchMatched(branch.previous, env) && (len(branch.cond) == 0 || evalCond(branch.cond, env))
}

func (c *ConfAssignments) priorBranchMatched(index int, env Environment) bool {
	if index == 0 {
		return false
	}

	branch := c.branches[index]

	return len(branch.cond) == 0 || evalCond(branch.cond, env) || c.priorBranchMatched(branch.previous, env)
}
