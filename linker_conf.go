package main

import "strings"

var linkerConfNames = []string{
	"_START_GROUP", "_END_GROUP", "BEFORE_PEERS", "AFTER_PEERS",
	"_LD_SRCS_GLOBALS", "_ROOT_FLAGS", "_SONAME_OPTION", "_SONAME_FLAG", "_SHARED_FLAG",
	"LD_STRIP_FLAG", "_LD_DCE_FLAG_GC_SECTIONS", "_LD_DCE_FLAG_PRINT_SECTIONS", "LD_DCE_FLAG",
	"_LD_ICF_FLAG", "_LD_ICF_FLAG_PRINT_SECTIONS", "LD_ICF_FLAG", "_LD_NO_PIE_FLAG",
	"_LD_USE_STDLIB", "_C_SYSTEM_LIBRARIES", "C_SYSTEM_LIBRARIES", "C_SYSTEM_LIBRARIES_INTERCEPT",
	"LD_EXPORT_ALL_DYNAMIC_SYMBOLS_FLAG", "EXPORTS_VALUE",
}

type LinkerConfBranch struct {
	parent   int
	previous int
	cond     []CondNode
}

type LinkerConfAssignment struct {
	branch int
	name   string
	value  string
	append bool
}

type LinkerConf struct {
	branches    []LinkerConfBranch
	assignments []LinkerConfAssignment
}

type LinkerFlags struct {
	StartGroup, EndGroup, BeforePeers, AfterPeers []ANY
	Globals, Roots, Shared, Soname, Exports       []ANY
	SystemLibs, Intercept, Strip, DCE, ICF, NoPie []ANY
	ArgCount                                      int
}

func readLinkerConf(fs FS) *LinkerConf {
	conf := &LinkerConf{branches: []LinkerConfBranch{{}}}

	if !fs.isFile(srcRootRel, ldConfRel) {
		throwFmt("linker conf: %s is missing", ldConfRel)
	}

	names := make(map[string]bool, len(linkerConfNames))

	for _, name := range linkerConfNames {
		names[name] = true
	}

	stack := []int{0}
	expressions := []string{""}
	needed := []bool{true}
	skipped := []bool{false}
	previous := 0
	pending := ""

	for _, raw := range strings.Split(string(concatChunks(fs.read(ldConfRel))), "\n") {
		line := strings.TrimSpace(raw)

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = pending + line
		pending = ""

		if strings.HasSuffix(line, "\\") {
			pending = strings.TrimSuffix(line, "\\") + " "

			continue
		}

		for strings.HasPrefix(line, "}") {
			if len(stack) == 1 {
				throwFmt("linker conf: unmatched } in %s", ldConfRel)
			}

			previous = stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			line = strings.TrimSpace(line[1:])
		}

		if name, value, appendOp, ok := linkerConfAssignment(line); ok {
			if names[name] && !skipped[stack[len(stack)-1]] {
				needed[stack[len(stack)-1]] = true

				conf.assignments = append(conf.assignments, LinkerConfAssignment{
					branch: stack[len(stack)-1], name: name, value: value, append: appendOp,
				})
			}

			continue
		}

		if !strings.HasSuffix(line, "{") {
			continue
		}

		header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		branch := LinkerConfBranch{parent: stack[len(stack)-1], cond: condFalse}
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
		throwFmt("linker conf: unfinished block or assignment in %s", ldConfRel)
	}

	for i := len(conf.branches) - 1; i > 0; i-- {
		if needed[i] {
			needed[conf.branches[i].parent] = true
			needed[conf.branches[i].previous] = true
		}
	}

	for i, expr := range expressions {
		if needed[i] && expr != "" {
			conf.branches[i].cond = parseLinkerConfCondition(expr)
		}
	}

	return conf
}

func linkerConfAssignment(line string) (name, value string, appendOp, ok bool) {
	name, value, ok = strings.Cut(line, "=")
	name = strings.TrimSpace(name)
	name, appendOp = strings.CutSuffix(name, "+")
	name = strings.TrimSpace(name)

	return name, strings.TrimSpace(value), appendOp, ok && name != "" && isConfVarName(name)
}

func parseLinkerConfCondition(expr string) []CondNode {
	expr = strings.NewReplacer("$", "", "&&", " AND ", "||", " OR ", "!=", "!=", "!", " NOT ").Replace(expr)

	p := &Parser{lex: &Lexer{name: ldConfRel, src: []byte(expr), line: 1, col: 1}}
	tok := Token{line: 1, col: 1}
	cp := CondParser{parent: p, ifTok: tok, toks: p.readCondTokens(tok)}

	cp.parseOr()

	if cp.pos != len(cp.toks) {
		throwFmt("linker conf: unsupported condition %s", expr)
	}

	return p.condScratch
}

func (c *LinkerConf) flags(p *Platform) LinkerFlags {
	env := p.ifEnv.clone()

	for _, name := range linkerConfNames {
		env.setString(internEnv(name), "")
	}

	if !env.hasBindingID(envLinkerId) {
		env.setString(envLinkerId, env.string(envDefaultLinkerId))
	}

	env.setString(envExportsFile, "${input:EXPORTS_FILE}")
	env.setString(envSoname, "$_SONAME")

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

	for _, name := range linkerConfNames {
		key := internEnv(name)

		if value, ok := p.Flags[key]; ok {
			env.setStringID(key, value)
		}
	}

	var result LinkerFlags

	read := func(name string) []ANY {
		value := expandStmtToken(env.string(internEnv(name)), env)

		value = strings.ReplaceAll(value, "$(BUILD_ROOT)", "$(B)")
		value = strings.ReplaceAll(value, "$(SOURCE_ROOT)", "$(S)")

		args := internAnys(splitShellWords(value))

		result.ArgCount += len(args)

		return args
	}

	result.StartGroup = read("_START_GROUP")
	result.EndGroup = read("_END_GROUP")
	result.BeforePeers = read("BEFORE_PEERS")
	result.AfterPeers = read("AFTER_PEERS")
	result.Globals = read("_LD_SRCS_GLOBALS")
	result.Roots = read("_ROOT_FLAGS")
	result.Shared = read("_SHARED_FLAG")
	result.Soname = read("_SONAME_FLAG")
	result.Exports = read("EXPORTS_VALUE")
	result.SystemLibs = read("C_SYSTEM_LIBRARIES")
	result.Intercept = read("C_SYSTEM_LIBRARIES_INTERCEPT")
	result.Strip = read("LD_STRIP_FLAG")
	result.NoPie = read("_LD_NO_PIE_FLAG")

	if !env.bool(envNoLinkerDce) {
		result.DCE = read("LD_DCE_FLAG")
	}

	if env.bool(envBtMinsizerel) || env.bool(envLinkerIcf) {
		result.ICF = read("LD_ICF_FLAG")
	}

	return result
}

func appendLinkerGlobals(args []ANY, flags LinkerFlags, archives, objects []VFS) []ANY {
	for _, arg := range flags.Globals {
		var paths []VFS

		switch arg.string() {
		case "${rootrel;ext=.a:SRCS_GLOBAL}":
			paths = archives
		case "${rootrel;ext=.o:SRCS_GLOBAL}":
			paths = objects
		case "${rootrel;ext=.supp:SRCS_GLOBAL}":
		default:
			args = append(args, arg)
		}

		for _, path := range paths {
			args = append(args, path.rel().any())
		}
	}

	return args
}

func appendLinkerTemplate(args, template []ANY, variable, value string) []ANY {
	for _, arg := range template {
		if strings.Contains(arg.string(), variable) {
			if value != "" {
				args = append(args, internAny(strings.ReplaceAll(arg.string(), variable, value)))
			}
		} else {
			args = append(args, arg)
		}
	}

	return args
}

func (f LinkerFlags) registerOwned() {
	for _, args := range [][]ANY{f.StartGroup, f.EndGroup, f.BeforePeers, f.AfterPeers, f.Globals, f.Roots, f.Shared, f.Soname, f.Exports, f.SystemLibs, f.Intercept, f.Strip, f.DCE, f.ICF, f.NoPie} {
		registerOwnedSlice(args)
	}
}
