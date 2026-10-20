package main

import (
	"regexp"
	"strings"
)

var confVarRef = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z_0-9]*)`)

type ConfBranch struct {
	parent   int
	previous int
	cond     []CondNode
	expr     string
	refs     []string
}

type ConfAssignment struct {
	imported *ConfAssignments
	branch   int
	name     string
	value    string
	append   bool
	refs     []string
}

type ConfAssignments struct {
	filename    string
	branches    []ConfBranch
	assignments []ConfAssignment
	sysinclVars map[string]bool
	linkerVars  map[string]bool
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
	conf := &ConfAssignments{filename: filename, branches: []ConfBranch{{}}}
	stack := []int{0}
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
			if !skipped[stack[len(stack)-1]] {
				conf.assignments = append(conf.assignments, ConfAssignment{
					branch: stack[len(stack)-1], name: name, value: value, append: appendOp, refs: confValueRefs(value),
				})
			}

			continue
		}

		if !strings.HasSuffix(line, "{") {
			continue
		}

		header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		branch := ConfBranch{parent: stack[len(stack)-1]}
		unsupported := false
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
		default:
			unsupported = true
		}

		branch.expr = expr
		conf.branches = append(conf.branches, branch)
		skipped = append(skipped, skipped[branch.parent] || unsupported)
		stack = append(stack, len(conf.branches)-1)
	}

	if len(stack) != 1 || pending != "" {
		throwFmt("conf: unfinished block or assignment in %s", filename)
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

func confValueRefs(value string) []string {
	var refs []string

	for _, match := range confVarRef.FindAllStringSubmatch(value, -1) {
		refs = append(refs, match[1])
	}

	return refs
}

type ConfAssignmentRef struct {
	conf       *ConfAssignments
	assignment *ConfAssignment
}

func (c *ConfAssignments) selectVars(only string) map[string]bool {
	selected := &c.linkerVars
	roots := linkerConfNames

	if only == "SYSINCL" {
		selected = &c.sysinclVars
		roots = []string{only}
	}

	if *selected != nil {
		return *selected
	}

	byName := make(map[string][]ConfAssignmentRef)

	var collect func(*ConfAssignments)

	collect = func(conf *ConfAssignments) {
		for i := range conf.assignments {
			a := &conf.assignments[i]

			if a.imported != nil {
				collect(a.imported)
			} else {
				byName[a.name] = append(byName[a.name], ConfAssignmentRef{conf, a})
			}
		}
	}

	collect(c)

	names := make(map[string]bool)
	queue := append([]string(nil), roots...)

	for len(queue) != 0 {
		name := queue[len(queue)-1]

		queue = queue[:len(queue)-1]

		if names[name] {
			continue
		}

		names[name] = true

		for _, ref := range byName[name] {
			queue = append(queue, ref.assignment.refs...)
			queue = ref.conf.branchVars(ref.assignment.branch, queue)
		}
	}

	*selected = names

	return names
}

func (c *ConfAssignments) branchVars(index int, vars []string) []string {
	if index == 0 {
		return vars
	}

	b := &c.branches[index]

	if b.expr != "" && b.cond == nil {
		b.cond = parseConfBranchCondition(c.filename, b.expr)

		for _, node := range b.cond {
			if node.Kind == ckIdent {
				b.refs = append(b.refs, node.Name)
			}
		}
	}

	vars = append(vars, b.refs...)
	vars = c.branchVars(b.parent, vars)

	return c.branchVars(b.previous, vars)
}

func (c *ConfAssignments) apply(env Environment, only string) {
	names := c.selectVars(only)

	c.applyPass(env, false, names)
	c.applyPass(env, true, names)
}

func (c *ConfAssignments) applyPass(env Environment, conditional bool, names map[string]bool) {
	for _, a := range c.assignments {
		if a.imported != nil {
			a.imported.applyPass(env, conditional, names)

			continue
		}

		if !names[a.name] || conditional != (a.branch != 0) || !c.branchActive(a.branch, env) {
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
