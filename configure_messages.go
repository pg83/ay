package main

import (
	"bytes"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
)

var (
	treePluginMacrosOnce sync.Once
	treePluginMacros     map[string]bool
	pluginMacroDef       = regexp.MustCompile(`(?m)^def on([a-z0-9_]+)\(`)
	testSizeTimeouts     = map[string]int{"small": 60, "medium": 600, "large": 3600}
)

var addInclLanguages = func() []ANY {
	var out []ANY

	for _, entry := range strings.Split(addInclLanguageList, "), ") {
		out = append(out, internStr(entry[:strings.IndexByte(entry, ' ')]).any())
	}

	return out
}()

var pythonNoLintPaths = []string{
	"contrib/", "devtools/", "junk/", "taxi/uservices/", "travel/", "market/report/lite/", "market/robotics/contrib/",
	"market/sre", "passport/backend/oauth/", "sdg/sdc/contrib/", "sdg/sdc/third_party/", "smart_devices/third_party/",
	"yt/yt/", "yt/python/", "yt/python_py2/", "cml/comrade_agent/src/contrib/",
}

const addInclLanguageList = "asm (for .asm), c (for .cpp .cc .cxx .c .C .auxcpp .h .hh .hpp .cuh .H .hxx .xh .ipp .ixx ..inl ..vert ..frag ..tesc ..tese ..geom ..comp .cu .S .s .sfdl .m .mm .l .lex .lpp .y .ypp .gperf .asp .f .go), cython (for .pyx .pxd .pxi), flatc (for .fbs .fbs64), idl (for .idl), nlg (for .nlg), other (for .fml .fml2 .fml3 .pln .info .a .lua .sh .bin .py .pyi .pysrc .ts .js .tsx .jsx), proto (for .gzt .gztproto .proto .ev .cfgproto), ragel (for .rl .rh .rli .rl6 .rl5), ros (for .msg .srv .rostopic), sc (for .sc), swig (for .swg), xs (for .xs), xsyn (for .xsyn), ydl (for .ydl)"

func vecDump(args []ANY) string {
	var b strings.Builder

	b.WriteString("vector of size ")
	b.WriteString(strconv.Itoa(len(args)))

	for i, arg := range args {
		b.WriteString("\n  ")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(": ")
		b.WriteString(arg.string())
	}

	return b.String()
}

func ymakeVFS(v VFS) string {
	if rel := v.relString(); v.isSource() {
		return "$S/" + rel
	} else {
		return "$B/" + rel
	}
}

func unresolvedIncludeError(w Warn) string {
	return "could not resolve include file: [[imp]]" + w.target.string() + "[[rst]] included from here: [[imp]]" + ymakeVFS(w.includer) + "[[rst]]"
}

func validateTest(meta TestMeta) []string {
	var errs []string

	size := meta.size

	if size == "" {
		size = "small"
	}

	tags := func(tag string) bool { return slices.Contains(meta.tags, tag) }
	requirements := map[string]bool{}

	for _, req := range meta.requirements {
		name, _, ok := strings.Cut(req, ":")

		switch {
		case req == "kvm":
			requirements[req] = true
		case ok:
			requirements[name] = true
		default:
			errs = append(errs, "Invalid requirement syntax [[imp]]"+req+"[[rst]]: expect <requirement>:<value>")
		}
	}

	for _, tag := range meta.tags {
		if prefix, _, ok := strings.Cut(tag, ":"); ok && prefix != "sb" && prefix != "ya" && prefix != "ynmt_benchmark" && prefix != "bert_models" && prefix != "zeliboba_map" {
			errs = append(errs, "Only [[imp]]sb:[[rst]] and [[imp]]ya:[[rst]] prefixes are allowed in system tags: "+tag)
		}
	}

	fat, inAutocheck := tags("ya:fat"), !tags("ya:not_autocheck") && !tags("ya:manual")

	switch {
	case fat && size != "large":
		errs = append(errs, "Only LARGE test may have ya:fat tag")
	case !fat && tags("ya:privileged"):
		errs = append(errs, "ya:privileged can be used with LARGE tests only")
	}

	if !fat && inAutocheck && size == "large" {
		errs = append(errs, "LARGE test must have ya:fat tag")
	}

	if requirements["container"] && requirements["porto_layers"] {
		errs = append(errs, "Only one of 'container', 'porto_layers' can be set, not both")
	}

	if tags("ya:privileged") && !requirements["container"] && !requirements["porto_layers"] {
		errs = append(errs, "Only tests with 'container' or 'porto_layers' requirement can have 'ya:privileged' tag")
	}

	if limit, known := testSizeTimeouts[size]; !known {
		errs = append(errs, "Unknown test size: [[imp]]"+strings.ToUpper(size)+"[[rst]], choose from [[imp]]SMALL, MEDIUM, LARGE[[rst]]")
	} else if timeout := meta.timeout; timeout < 0 {
		errs = append(errs, "Error when parsing test timeout: [[bad]]Timeout must be > 0[[rst]]")
	} else if timeout > limit && inAutocheck {
		suggested := ""

		for _, candidate := range [...]string{"small", "medium", "large"} {
			if timeout <= testSizeTimeouts[candidate] {
				suggested = ", suggested size: [[imp]]" + strings.ToUpper(candidate) + "[[rst]]"

				break
			}
		}

		errs = append(errs, "Max allowed timeout for test size [[imp]]"+strings.ToUpper(size)+"[[rst]] is [[imp]]"+strconv.Itoa(limit)+" sec[[rst]]"+suggested)
	}

	if meta.splitFactor != 0 {
		if meta.forkMode == "" {
			errs = append(errs, "SPLIT_FACTOR must be use with FORK_TESTS() or FORK_SUBTESTS() macro")
		}

		switch {
		case meta.splitFactor < 0:
			errs = append(errs, "Incorrect SPLIT_FACTOR value: must be > 0")
		case meta.splitFactor > 1000:
			errs = append(errs, "Incorrect SPLIT_FACTOR value: the maximum allowed value is 1000")
		}
	}

	if !fat && tags("ya:noretries") && !tags("ya:yt") {
		errs = append(errs, "Only LARGE tests can have 'ya:noretries' tag")
	}

	return errs
}

func treePluginMacro(fs FS, name STR) bool {
	if pluginMacroNames[name] {
		return true
	}

	treePluginMacrosOnce.Do(func() {
		treePluginMacros = map[string]bool{}

		for _, dir := range [...]string{"build/plugins", "build/internal/plugins"} {
			for _, packed := range fs.listdir(dirKey(dir)).names {
				file := STR(packed >> 1).string()

				if packed&1 != 0 || !strings.HasSuffix(file, ".py") {
					continue
				}

				for _, m := range pluginMacroDef.FindAllSubmatch(concatChunks(fs.read(dir+"/"+file)), -1) {
					treePluginMacros[strings.ToUpper(string(m[1]))] = true
				}
			}
		}
	})

	return treePluginMacros[name.string()]
}

func (ctx *GenCtx) missingDirectory(dir STR) string {
	if present, isDir := ctx.fs.existsID(srcRootRel, dir); present && isDir {
		return "directory without ya.make: "
	}

	return "missing directory: "
}

func missingIncludeError(v *MissingIncludeStmt) string {
	return "file [[alt1]]$S/" + v.Path + "[[rst]] specified in INCLUDE command (in $S/" + v.File + ") doesn't exist."
}

func makefileParseError(fs FS, pe *ParseError) string {
	if pe.IncludeLoop != nil {
		var b strings.Builder

		b.WriteString("include loop detected: ")

		for i, file := range pe.IncludeLoop {
			if i > 0 {
				b.WriteString("\n -> ")
			}

			b.WriteString("$S/")
			b.WriteString(file)
		}

		return b.String()
	}

	src := concatChunks(fs.read(pe.File))
	where := pe.File + ":" + strconv.Itoa(pe.Line) + ":" + strconv.Itoa(pe.Col)

	return strings.TrimRight("devtools/ymake/lang/makefile_reader.cpp:88: lexer error at "+where+": \n"+lexerContext(src, sourceOffset(src, pe.Line, pe.Col), 200), " \t\n")
}

func sourceOffset(src []byte, line, col int) int {
	pos := 0

	for ; line > 1 && pos < len(src); pos++ {
		if src[pos] == '\n' {
			line--
		}
	}

	return min(pos+col-1, len(src))
}

func lexerContext(src []byte, pos, context int) string {
	end := min(pos+context, pos+len(src)-1, len(src))

	return string(src[max(pos-context, 0):pos]) + "[ <- HERE ]" + string(src[pos:max(end, pos)])
}

func statementEnd(src []byte, line int, name string) (int, int) {
	pos, row, col := sourceOffset(src, line, 1), line, 1

	if i := bytes.Index(src[pos:], []byte(name)); i >= 0 {
		for _, b := range src[pos : pos+i+len(name)] {
			if b == '\n' {
				row, col = row+1, 1
			} else {
				col++
			}
		}

		pos += i + len(name)
	}

	quoted := false

	for ; pos < len(src); pos++ {
		switch b := src[pos]; {
		case b == '"' && (pos == 0 || src[pos-1] != '\\'):
			quoted = !quoted
		case b == ')' && !quoted:
			return row, col
		case b == '\n':
			row, col = row+1, 0
		}

		col++
	}

	return 0, 0
}

func (ctx *GenCtx) statementEnd(dir string, line int, name string) (int, int) {
	return statementEnd(concatChunks(ctx.fs.read(joinRel(dir, "ya.make"))), line, name)
}

func (ctx *GenCtx) recurseError(parent, dir VFS) Warn {
	w := Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(parent.relString())}

	if present, isDir := ctx.fs.existsID(srcRootRel, dir.rel()); present && !isDir {
		src := concatChunks(ctx.fs.read(joinRel(parent.relString(), "ya.make")))
		arg := strings.TrimPrefix(dir.relString(), parent.relString()+"/")

		if i := bytes.Index(src, []byte(arg)); i >= 0 {
			w.Row, w.Column = statementEnd(src, 1+bytes.Count(src[:i], []byte{'\n'}), arg)
		}

		w.Error = "[[imp]]RECURSE[[rst]] to non-directory [[imp]]$S/" + dir.relString() + "[[rst]]"

		return w
	}

	w.Error = "[[alt1]]RECURSE[[rst]] to " + ctx.missingDirectory(dir.rel()) + "$S/" + dir.relString()

	return w
}

func stmtLine(s Stmt) int {
	if line := reflect.ValueOf(s).Elem().FieldByName("Line"); line.IsValid() {
		return int(line.Int())
	}

	return 0
}

type addInclDiag struct {
	errors []string
	dirs   []ANY
}

func diagnoseAddIncl(args []ANY) *addInclDiag {
	var d addInclDiag
	var dirs []ANY

	var lang ANY

	scoped, expectLang := false, false

	for _, arg := range args {
		switch {
		case arg == kwFOR.any():
			if lang != 0 {
				d.errors = append(d.errors, "Two different languages specified for [[alt1]]ADDINCL[[rst]]: previous was \""+lang.string()+"\"")
			}

			expectLang = true
		case expectLang:
			lang, expectLang = arg, false

			if !slices.Contains(addInclLanguages, arg) {
				lang = 0
				d.errors = append(d.errors, "Bad language argument in [[alt1]]ADDINCL[[rst]]: \""+arg.string()+"\". Possible languages: "+addInclLanguageList)
			}
		case arg == kwGLOBAL.any() || arg == kwONE_LEVEL.any():
			scoped = true
		case arg == kwLOCAL.any():
		default:
			dirs = append(dirs, arg)
			lang, scoped = 0, false
		}
	}

	if scoped || expectLang || lang != 0 {
		d.errors = append(d.errors, "Tail arguments in [[alt1]]ADDINCL[[rst]] without a dir to apply will be ignored")
	}

	if d.errors == nil {
		return nil
	}

	d.dirs = dirs

	return &d
}

func (ctx *GenCtx) checkAddInclDirs(dirs []VFS, modulePath string, onWarn func(Warn)) {
	for _, dir := range dirs {
		if !dir.isSource() || dir == srcRootDirVFS {
			continue
		}

		exists, known := ctx.addInclDirExists[dir]

		if !known {
			present, isDir := ctx.fs.existsID(srcRootRel, dir.rel())

			exists = present && isDir

			if ctx.addInclDirExists == nil {
				ctx.addInclDirExists = map[VFS]bool{}
			}

			ctx.addInclDirExists[dir] = exists
		}

		if !exists {
			onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: "[[alt1]]ADDINCL[[rst]] to non existent source directory " + ymakeVFS(dir)})
		}
	}
}

func (ctx *GenCtx) reportAddInclDiag(v *AddInclStmt, env Environment, modulePath string, onWarn func(Warn)) {
	for _, message := range v.diag.errors {
		onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: message})
	}

	ctx.checkAddInclDirs(expandConfigVFSPaths(v.diag.dirs, env), modulePath, onWarn)
}

func resourceDeclarations(stmt *DeclareResourceStmt, report func(string)) []*DeclareResourceStmt {
	args := stmt.Args

	switch stmt.Macro {
	case tokDeclareExternalResource:
		if len(args) == 0 {
			report("Missed arguments in [[alt1]]DECLARE_EXTERNAL_RESOURCE[[rst]]")

			return nil
		}

		var pairs []ANY

		for n := 0; n < len(args); n += 2 {
			if n+1 >= len(args) {
				report("Incorrect number or arguments in [[alt1]]DECLARE_EXTERNAL_RESOURCE[[rst]]: " + vecDump(args))

				break
			}

			if n+2 < len(args) && args[n+2] == kwSTRIP_PREFIX.any() {
				if n+3 >= len(args) {
					report("Missed argument after [[alt1]]STRIP_PREFIX[[rst]] in [[alt1]]DECLARE_EXTERNAL_RESOURCE[[rst]]: " + vecDump(args))

					break
				}

				if _, err := strconv.ParseUint(args[n+3].string(), 10, 32); err != nil {
					report("[[alt1]]STRIP_PREFIX[[rst]] must be integral in [[alt1]]DECLARE_EXTERNAL_RESOURCE[[rst]]: " + vecDump(args))

					break
				}

				pairs = append(pairs, args[n], args[n+1])
				n += 2

				continue
			}

			pairs = append(pairs, args[n], args[n+1])
		}

		if pairs == nil {
			return nil
		}

		return []*DeclareResourceStmt{{Macro: stmt.Macro, Args: pairs, Line: stmt.Line}}
	case tokDeclareExternalHostResourcesBundle:
		if !hostResourcesValid("DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE", args, report) {
			return nil
		}
	case tokDeclareExternalHostResourcesPack:
		if len(args) == 0 || args[0] != kwRESOURCE_NAME.any() {
			report("Incorrect arguments in DECLARE_EXTERNAL_HOST_RESOURCES_PACK: " + vecDump(args))

			return nil
		}

		var out []*DeclareResourceStmt

		group := 1

		for i := 1; i <= len(args); i++ {
			if i < len(args) && args[i] != kwRESOURCE_NAME.any() {
				continue
			}

			if hostResourcesValid("DECLARE_EXTERNAL_HOST_RESOURCES_PACK", args[group:i], report) {
				out = append(out, &DeclareResourceStmt{Macro: tokDeclareExternalHostResourcesBundle, Args: args[group:i], Line: stmt.Line})
			}

			group = i + 1
		}

		return out
	}

	return []*DeclareResourceStmt{stmt}
}

func hostResourcesValid(macro string, args []ANY, report func(string)) bool {
	n := 1

	for ; n+2 < len(args); n += 3 {
		if n+4 < len(args) && args[n+3] == kwSTRIP_PREFIX.any() {
			if _, err := strconv.ParseUint(args[n+4].string(), 10, 32); err != nil {
				report("[[alt1]]STRIP_PREFIX[[rst]] must be integral in [[alt1]]" + macro + "[[rst]]: " + vecDump(args))
			}

			n += 2
		}
	}

	if n != len(args) {
		report("Incorrect number or arguments in [[alt1]]" + macro + "[[rst]]: " + vecDump(args))

		return false
	}

	return true
}

func pythonNoLintAllowed(modulePath string) bool {
	return slices.ContainsFunc(pythonNoLintPaths, func(prefix string) bool { return strings.HasPrefix(modulePath, prefix) })
}

func reportPythonVersion(modulePath, macro string, env Environment, onWarn func(Warn)) {
	if version := env.string(envPythonVer); version == "" || version == "unset" {
		onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: "[[alt1]]" + macro + "[[rst]]: Unknown Python version, select it using [[alt1]]USE_PYTHONx[[rst]] macro"})
	}
}

func duplicates(values []string) []string {
	seen, dups := map[string]bool{}, []string(nil)

	for _, value := range values {
		if seen[value] && !slices.Contains(dups, value) {
			dups = append(dups, value)
		}

		seen[value] = true
	}

	slices.Sort(dups)

	return dups
}

func pyRepr(s string) string {
	quote := byte('\'')

	if strings.IndexByte(s, '\'') >= 0 && strings.IndexByte(s, '"') < 0 {
		quote = '"'
	}

	var b strings.Builder

	b.WriteByte(quote)

	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == quote || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}

	b.WriteByte(quote)

	return b.String()
}

func pySetRepr(values []string) string {
	quoted := make([]string, len(values))

	for i, value := range values {
		quoted[i] = pyRepr(value)
	}

	return "{" + strings.Join(quoted, ", ") + "}"
}

func restrictPath(modulePath string, args []ANY, onWarn func(Warn)) {
	if len(args) == 0 {
		return
	}

	paths, message := args, "forbidden"

	if i := slices.IndexFunc(args, func(arg ANY) bool { return arg.string() == "MSG" }); i >= 0 {
		paths, message = args[:i], joinAnys(args[i+1:], " ")
	}

	for _, prefix := range paths {
		if strings.HasPrefix(modulePath, prefix.string()) {
			return
		}
	}

	onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: "Path '[[imp]]$S/" + modulePath + "[[rst]]' is restricted - [[bad]]" + message + "[[rst]]. Valid path prefixes are: [[unimp]]" + joinAnys(paths, ", ") + "[[rst]]"})
}

func assertMacro(modulePath string, args []ANY, env Environment, onWarn func(Warn)) {
	if len(args) > 0 && strings.EqualFold(env.string(internEnvSTR(args[0].str())), "no") {
		onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: joinAnys(args[1:], " ")})
	}
}

func validateInDirs(modulePath, pattern, srcdir string, dirs []string, onWarn func(Warn)) bool {
	valid := true

	report := func(message string) {
		onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: "[[imp]]DECLARE_IN_DIRS[[rst]]: " + message})
		valid = false
	}

	if strings.Contains(pattern, "**") {
		report("'**' in files mask is prohibited. Seen [[unimp]]" + pattern + "[[rst]]")
	}

	for _, mask := range [...]string{"*", "?"} {
		if strings.Contains(srcdir, mask) {
			report("'" + mask + "' in [[imp]]SRCDIR[[rst]] argument is prohibited. Seen [[unimp]]" + srcdir + "[[rst]]")
		}
	}

	for _, dir := range dirs {
		for _, mask := range [...]string{"*", "?", "$", ".."} {
			if strings.Contains(dir, mask) {
				report("'" + mask + "' in [[imp]]DIRS[[rst]] argument is prohibited. Seen [[unimp]]" + dir + "[[rst]]")
			}
		}
	}

	return valid
}
