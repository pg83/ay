package main

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
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
	protoModelKeywords   = []string{"FILE", "OUTPUT_INCLUDES", "PROTODESC", "PROTODESC_NAME"}
	usrvProtoKeywords    = []string{"FILE", "FILES", "IMPORTS", "OPTS_FILE", "ORIGINS", "OUTPUT_INCLUDES", "PROTODESC", "PROTODESC_NAME"}
	libraryModuleTypes   = map[TOK]bool{tokLibrary: true, tokPy3Library: true, tokGoLibrary: true, tokProtoLibrary: true}
	tsColors             = strings.NewReplacer("{COLORS.red}", tsRed, "{COLORS.green}", tsGreen, "{COLORS.yellow}", tsYell, "{COLORS.cyan}", tsCyan, "{COLORS.reset}", tsReset)
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

const (
	addInclLanguageList = "asm (for .asm), c (for .cpp .cc .cxx .c .C .auxcpp .h .hh .hpp .cuh .H .hxx .xh .ipp .ixx ..inl ..vert ..frag ..tesc ..tese ..geom ..comp .cu .S .s .sfdl .m .mm .l .lex .lpp .y .ypp .gperf .asp .f .go), cython (for .pyx .pxd .pxi), flatc (for .fbs .fbs64), idl (for .idl), nlg (for .nlg), other (for .fml .fml2 .fml3 .pln .info .a .lua .sh .bin .py .pyi .pysrc .ts .js .tsx .jsx), proto (for .gzt .gztproto .proto .ev .cfgproto), ragel (for .rl .rh .rli .rl6 .rl5), ros (for .msg .srv .rostopic), sc (for .sc), swig (for .swg), xs (for .xs), xsyn (for .xsyn), ydl (for .ydl)"
	offersProtoRoot     = "api/proto/yandex/market/b2b/offers_catalog/"
	tsRed               = "\x1b[0;31m"
	tsGreen             = "\x1b[0;32m"
	tsYell              = "\x1b[0;33m"
	tsCyan              = "\x1b[0;36m"
	tsReset             = "\x1b[0;49m"
)

func vecDump(args []ANY) string {
	var b strings.Builder

	b.WriteString("vector of size ")
	b.WriteString(strconv.Itoa(len(args)))

	for i, arg := range args {
		b.WriteString("\n  ")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(": ")
		b.WriteString(pluginArgLiteral(arg))
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

func pluginArgLiteral(arg ANY) string {
	v := arg.vfs()

	switch {
	case v == 0:
		return arg.string()
	case v.isSource():
		return "${ARCADIA_ROOT}/" + v.relString()
	default:
		return "${ARCADIA_BUILD_ROOT}/" + v.relString()
	}
}

func unresolvedIncludeError(w Warn) string {
	if w.includeText != "" {
		return w.includeText
	}

	if w.ts {
		if message, ok := strings.CutPrefix(w.target.string(), tsParseErrorPrefix); ok {
			return "Failed to parse import in " + ymakeVFS(w.includer) + ": " + message
		}

		return "Failed to resolve import in " + ymakeVFS(w.includer) + ": " + w.target.string()
	}

	target := w.target.string()

	if v := w.target.vfs(); v != 0 {
		target = ymakeVFS(v)
	}

	return "could not resolve include file: [[imp]]" + target + "[[rst]] included from here: [[imp]]" + ymakeVFS(w.includer) + "[[rst]]"
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

func (e *EmitContext) declaredPeer(peer VFS) bool {
	ctx, where := e.ctx, makeFileWhere(e.instance.Path.relString())

	if !peerYaMakeExists(ctx.fs, peer) {
		ctx.onWarn(Warn{Kind: WarnModuleFailed, Where: where, Error: "[[alt1]]PEERDIR[[rst]] to " + ctx.missingDirectory(peer.rel()) + "[[imp]]$S/" + peer.relString() + "[[rst]]"})

		return false
	}

	if ctx.moduleNameVFS(peer) == 0 {
		ctx.onWarn(Warn{Kind: WarnModuleFailed, Where: where, Error: "No modules for [[alt1]]PEERDIR[[rst]] in [[imp]]$S/" + peer.relString() + "[[rst]]"})

		return false
	}

	return true
}

func (e *EmitContext) incompatiblePeer(peer string) {
	tag := Environment{s: &e.frame.ifEnv}.string(envMODULE_TAG)

	e.ctx.onWarn(Warn{Kind: WarnModuleFailed, Where: makeFileWhere(e.instance.Path.relString()), Error: "[[alt1]]PEERDIR[[rst]] from module tagged [[alt1]]" + tag + "[[rst]] to [[imp]]$S/" + peer + "[[rst]] is prohibited by incompatible tags and skipped"})
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
		case arg.string() == "" || arg.string() == `""` || arg.string() == `''`:
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

func (ctx *GenCtx) checkAddInclDirs(dirs []VFS, env Environment, modulePath string, onWarn func(Warn)) {
	if env.bool(internedEnv("NON_FATAL_ADDINCL_TO_MISSING")) {
		return
	}

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

	ctx.checkAddInclDirs(expandConfigVFSPaths(v.diag.dirs, env), env, modulePath, onWarn)
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

				if !utilUint32(args[n+3].string()) {
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
			if !utilUint32(args[n+4].string()) {
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

func duplicateSTRs(values []STR) []string {
	slices.Sort(values)

	var dups []string

	for i := 1; i < len(values); i++ {
		if values[i] == values[i-1] && (i == 1 || values[i-1] != values[i-2]) {
			dups = append(dups, values[i].string())
		}
	}

	slices.Sort(dups)

	return dups
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

func goResourceEntries(args []string, report func(string)) ([]ResourceEntry, bool) {
	if len(args)%2 != 0 {
		report("last file " + args[len(args)-1] + " is missing resource key")
	}

	entries := make([]ResourceEntry, 0, len(args)/2)

	for i := 0; i+1 < len(args); i += 2 {
		file, key := args[i], args[i+1]

		switch {
		case key == "":
			report("file key must be non empty")

			return nil, false
		case file == "-" && !strings.Contains(key, "="):
			report("key \"" + key + "\" must contain = sign")

			return nil, false
		}

		entries = append(entries, ResourceEntry{Path: resourceWordPath(file), Key: resourceWordPath(key)})
	}

	return entries, true
}

func goProcessSrcsErrors(d *ModuleData, modulePath string, env Environment, report func(string)) {
	export := false

	var goFiles []string

	for _, meta := range d.srcs {
		if meta.Global || meta.Compile.Variant != 0 {
			continue
		}

		file := meta.Source.string()

		switch ext := path.Ext(file); ext {
		case ".c", ".cc", ".cpp", ".cxx", ".C":
			export = false
		case ".ev", ".fbs", ".go", ".in", ".proto", ".s", ".syso", ".S":
			if export {
				report("Unmatched CGO_EXPORT keyword in SRCS() macro")
			}

			export = false

			if ext == ".go" {
				goFiles = append(goFiles, file)
			}
		default:
			if file == "CGO_EXPORT" {
				export = true
			}
		}
	}

	if export {
		report("Unmatched CGO_EXPORT keyword in SRCS() macro")
	}

	for _, file := range goFiles {
		if strings.HasSuffix(file, "_test.go") {
			report("file " + file + " must be listed in GO_TEST_SRCS() or GO_XTEST_SRCS() macros")
		}
	}

	for _, file := range slices.Concat(d.goTestSrcs, d.goXTestSrcs) {
		if !strings.HasSuffix(file.string(), "_test.go") {
			report("file " + file.string() + " should not be listed in GO_TEST_SRCS() or GO_XTEST_SRCS() macros")
		}
	}

	if !stringIsTruthy(env.string(envGoFmtAddCheck)) && !goFmtSkipAllowed(modulePath, strings.Fields(env.string(envGoFmtAllowSkip))) {
		report("Disabling gofmt is prohibited, please contact devtools")
	}

	if len(d.cgoSrcs) == 0 {
		return
	}

	if !env.bool(envCGO_ENABLED) {
		report("trying to build with CGO (CGO_SRCS is non-empty) when CGO is disabled")
	}

	for _, file := range d.cgoSrcs {
		if name := file.string(); !strings.HasSuffix(name, ".go") {
			report("cannot find source file: " + strings.TrimSuffix(name, path.Ext(name)) + ".cgo2.c.o")
			report("file " + name + " should not be listed in CGO_SRCS() macros")
		}
	}
}

func goFmtSkipAllowed(modulePath string, allowed []string) bool {
	for _, item := range allowed {
		if modulePath == item || strings.HasPrefix(modulePath, strings.TrimSuffix(item, "/")+"/") {
			return true
		}
	}

	return false
}

func runJavaProgramClasspathError(dir string, program JavaRunProgram) string {
	if len(parseJavaRunProgram(dir, program).classpath) == 1 {
		return ""
	}

	return "multiple CLASSPATH elements in RUN_JAVA_PROGRAM invocation no more supported. Use JAVA_RUNTIME_PEERDIR on the JAVA_PROGRAM module instead"
}

type pyJSONScanner struct {
	s   []rune
	msg string
	pos int
}

func pyJSONDecodeError(data []byte) string {
	p := &pyJSONScanner{s: []rune(string(data))}

	if len(p.s) > 0 && p.s[0] == '\ufeff' {
		return p.repr("Unexpected UTF-8 BOM (decode using utf-8-sig)", 0)
	}

	end, ok := p.value(p.ws(0))

	switch {
	case !ok:
		return p.repr(p.msg, p.pos)
	case p.ws(end) != len(p.s):
		return p.repr("Extra data", p.ws(end))
	}

	return ""
}

func (p *pyJSONScanner) repr(msg string, pos int) string {
	line, col := 1, pos+1

	for i, r := range p.s[:pos] {
		if r == '\n' {
			line, col = line+1, pos-i
		}
	}

	return "JSONDecodeError(" + pyRepr(fmt.Sprintf("%s: line %d column %d (char %d)", msg, line, col, pos)) + ")"
}

func (p *pyJSONScanner) fail(msg string, pos int) (int, bool) {
	p.msg, p.pos = msg, pos

	return 0, false
}

func (p *pyJSONScanner) ws(i int) int {
	for i < len(p.s) && (p.s[i] == ' ' || p.s[i] == '\t' || p.s[i] == '\n' || p.s[i] == '\r') {
		i++
	}

	return i
}

func (p *pyJSONScanner) at(i int, word string) bool {
	return i+len(word) <= len(p.s) && string(p.s[i:i+len(word)]) == word
}

func (p *pyJSONScanner) value(i int) (int, bool) {
	if i >= len(p.s) {
		return p.fail("Expecting value", i)
	}

	switch c := p.s[i]; {
	case c == '"':
		return p.str(i + 1)
	case c == '{':
		return p.object(i + 1)
	case c == '[':
		return p.array(i + 1)
	}

	for _, word := range [...]string{"null", "true", "false", "NaN", "Infinity", "-Infinity"} {
		if p.at(i, word) {
			return i + len([]rune(word)), true
		}
	}

	return p.number(i)
}

func (p *pyJSONScanner) digit(i int) bool {
	return i < len(p.s) && p.s[i] >= '0' && p.s[i] <= '9'
}

func (p *pyJSONScanner) number(start int) (int, bool) {
	i := start

	if p.s[i] == '-' {
		if i++; i >= len(p.s) {
			return p.fail("Expecting value", start)
		}
	}

	switch {
	case p.s[i] >= '1' && p.s[i] <= '9':
		for i++; p.digit(i); i++ {
		}
	case p.s[i] == '0':
		i++
	default:
		return p.fail("Expecting value", start)
	}

	if i+1 < len(p.s) && p.s[i] == '.' && p.digit(i+1) {
		for i += 2; p.digit(i); i++ {
		}
	}

	if i+1 < len(p.s) && (p.s[i] == 'e' || p.s[i] == 'E') {
		exp := i

		if i++; i+1 < len(p.s) && (p.s[i] == '-' || p.s[i] == '+') {
			i++
		}

		for ; p.digit(i); i++ {
		}

		if !p.digit(i - 1) {
			i = exp
		}
	}

	return i, true
}

func (p *pyJSONScanner) str(end int) (int, bool) {
	begin := end - 1

	for {
		next := end

		for ; next < len(p.s) && p.s[next] != '"' && p.s[next] != '\\'; next++ {
			if p.s[next] <= 0x1f {
				return p.fail("Invalid control character at", next)
			}
		}

		if next == len(p.s) {
			return p.fail("Unterminated string starting at", begin)
		}

		if p.s[next] == '"' {
			return next + 1, true
		}

		if next++; next == len(p.s) {
			return p.fail("Unterminated string starting at", begin)
		}

		if p.s[next] != 'u' {
			if !strings.ContainsRune(`"\\/bfnrt`, p.s[next]) {
				return p.fail("Invalid \\escape", next-1)
			}

			end = next + 1

			continue
		}

		if next+5 >= len(p.s) {
			return p.fail("Invalid \\uXXXX escape", next)
		}

		code, err := strconv.ParseUint(string(p.s[next+1:next+5]), 16, 32)

		if err != nil || strings.ContainsAny(string(p.s[next+1:next+5]), "+-_xX") {
			return p.fail("Invalid \\uXXXX escape", next)
		}

		end = next + 5

		if code >= 0xd800 && code <= 0xdbff && end+6 < len(p.s) && p.s[end] == '\\' && p.s[end+1] == 'u' {
			if _, err := strconv.ParseUint(string(p.s[end+2:end+6]), 16, 32); err != nil || strings.ContainsAny(string(p.s[end+2:end+6]), "+-_xX") {
				return p.fail("Invalid \\uXXXX escape", end+1)
			}

			end += 6
		}
	}
}

func (p *pyJSONScanner) object(i int) (int, bool) {
	if i = p.ws(i); i < len(p.s) && p.s[i] == '}' {
		return i + 1, true
	}

	for {
		if i >= len(p.s) || p.s[i] != '"' {
			return p.fail("Expecting property name enclosed in double quotes", i)
		}

		end, ok := p.str(i + 1)

		if !ok {
			return 0, false
		}

		if i = p.ws(end); i >= len(p.s) || p.s[i] != ':' {
			return p.fail("Expecting ':' delimiter", i)
		}

		if i, ok = p.value(p.ws(i + 1)); !ok {
			return 0, false
		}

		if i = p.ws(i); i < len(p.s) && p.s[i] == '}' {
			return i + 1, true
		}

		if i >= len(p.s) || p.s[i] != ',' {
			return p.fail("Expecting ',' delimiter", i)
		}

		if comma := i; p.ws(i+1) < len(p.s) && p.s[p.ws(i+1)] == '}' {
			return p.fail("Illegal trailing comma before end of object", comma)
		}

		i = p.ws(i + 1)
	}
}

func (p *pyJSONScanner) array(i int) (int, bool) {
	if i = p.ws(i); i < len(p.s) && p.s[i] == ']' {
		return i + 1, true
	}

	for {
		end, ok := p.value(i)

		if !ok {
			return 0, false
		}

		if i = p.ws(end); i < len(p.s) && p.s[i] == ']' {
			return i + 1, true
		}

		if i >= len(p.s) || p.s[i] != ',' {
			return p.fail("Expecting ',' delimiter", i)
		}

		if comma := i; p.ws(i+1) < len(p.s) && p.s[p.ws(i+1)] == ']' {
			return p.fail("Illegal trailing comma before end of array", comma)
		}

		i = p.ws(i + 1)
	}
}

func pyListRepr(items []string) string {
	quoted := make([]string, len(items))

	for i, item := range items {
		quoted[i] = pyRepr(item)
	}

	return "[" + strings.Join(quoted, ", ") + "]"
}

func pluginArgLiterals(args []ANY) []string {
	out := make([]string, len(args))

	for i, arg := range args {
		out[i] = pluginArgLiteral(arg)
	}

	return out
}

func protoModelParseError(args []string, namespace string, keywords []string) string {
	usrv := len(keywords) == len(usrvProtoKeywords)
	sep := ""

	if namespace != "" {
		sep = namespace + "/"
	}

	i := 0

	next := func() (string, bool) {
		if i == len(args) {
			return "", false
		}

		i++

		return args[i-1], true
	}

	keyword := func(arg string) bool { return slices.Contains(keywords, arg) }

	value := func(name string) (string, string) {
		if arg, ok := next(); ok && !keyword(arg) {
			return arg, ""
		}

		return "", "Value for arg '" + name + "' is missing"
	}

	parsePath := func(arg string) string {
		switch {
		case strings.Contains(arg, "$"):
			return "Path '" + arg + "' must not contain '$'"
		case strings.HasPrefix(arg, "/"):
			return "Path '" + arg + "' must not start with '/'"
		}

		return ""
	}

	parseProto := func(arg string) string {
		if !strings.HasSuffix(arg, ".proto") {
			return "Proto file '" + arg + "' must have ext '.proto'"
		}

		if msg := parsePath(arg); msg != "" {
			return msg
		}

		if file := strings.TrimRight(arg, "/"); sep != "" && !strings.HasPrefix(file, sep) {
			return "Invalid namespace '" + namespace + "' for file '" + file + "'"
		}

		return ""
	}

	untilKeyword := func(each func(string) string) (string, bool, string) {
		for arg, ok := next(); ok; arg, ok = next() {
			if keyword(arg) {
				return arg, true, ""
			}

			if msg := each(arg); msg != "" {
				return "", false, msg
			}
		}

		return "", false, ""
	}

	skip := func(string) string { return "" }
	protodesc := false

	for arg, ok := next(); ok; arg, ok = next() {
		for pending := true; pending; {
			var msg string

			pending = false

			switch {
			case arg == "PROTODESC" || arg == "PROTODESC_NAME":
				if _, msg = value(arg); msg == "" && protodesc {
					msg = "Args 'PROTODESC' and 'PROTODESC_NAME' are mutually exclusive"
				}

				protodesc = true
			case arg == "FILE":
				var file string

				if file, msg = value(arg); msg == "" {
					msg = parseProto(file)
				}

				for msg == "" {
					var more bool

					if arg, more = next(); !more {
						break
					}

					for more && (arg == "OUTPUT_INCLUDES" || usrv && arg == "IMPORTS") {
						arg, more, msg = untilKeyword(skip)
					}

					if more {
						pending = true

						break
					}
				}
			case arg == "OUTPUT_INCLUDES" || usrv && arg == "IMPORTS":
				msg = "Arg '" + arg + "' has to go after 'FILE'"
			case usrv && arg == "OPTS_FILE":
				var file string

				if file, msg = value(arg); msg == "" {
					msg = parsePath(file)
				}
			case usrv && arg == "ORIGINS":
				arg, pending, msg = untilKeyword(parsePath)
			case usrv && arg == "FILES":
				arg, pending, msg = untilKeyword(func(file string) string { return parseProto(strings.TrimPrefix(file, "$S/")) })
			default:
				msg = "Arg '" + arg + "' is unknown, known args are " + pyListRepr(keywords)
			}

			if msg != "" {
				return msg
			}
		}
	}

	if !protodesc {
		return "Arg 'PROTODESC' is missing"
	}

	return ""
}

func pyDirname(p string) string {
	head := p[:strings.LastIndexByte(p, '/')+1]

	if head != strings.Repeat("/", len(head)) {
		head = strings.TrimRight(head, "/")
	}

	return head
}

func offerConvertersError(args []string) string {
	if len(args) == 0 || len(args)%3 != 0 {
		return "Expected --view-config header message triples"
	}

	var dirs []string

	for i := 0; i < len(args); i += 3 {
		if header := args[i+1]; args[i] != "--view-config" || !strings.HasPrefix(header, offersProtoRoot) || !strings.HasSuffix(header, ".pb.h") {
			return "Expected --view-config with an offers_catalog .pb.h header"
		} else if dir := pyDirname(header); !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	names := map[string]string{}

	for _, dir := range dirs {
		name := strings.ReplaceAll(dir[min(len(offersProtoRoot), len(dir)):], "/", "_")

		if prev, ok := names[name]; ok {
			return "Converter package name collision '" + name + "': " + prev + " and " + dir
		}

		names[name] = dir
	}

	return ""
}

func grpcLinterDataError(args []string) string {
	if len(args) != 2 {
		return ""
	}

	for i, param := range [...]string{"root", "module"} {
		if !strings.HasPrefix(args[i], "${ARCADIA_ROOT}/") {
			return "Error at macro 'C35_GRPC_LINTER_TESTS_DATA': Param '" + param + "' (" + args[i] + ") should start with '${ARCADIA_ROOT}/'"
		}
	}

	return ""
}

func sandboxImportExists(fs FS, modulePath string, arg ANY) bool {
	if strings.HasPrefix(arg.string(), "/") {
		return false
	}

	if v := arg.vfs(); v != 0 {
		present, _ := fs.exists(srcRootRel, v.relString())

		return v.isSource() && present
	}

	p := path.Clean(arg.string())

	if present, _ := fs.exists(dirKey(modulePath), p); present {
		return true
	}

	present, _ := fs.exists(srcRootRel, p)

	return present
}

func cmakeTargetNameFromPathError(modulePath string, args []string) string {
	if len(args) != 1 {
		return "[[imp]]CMAKE_EXPORTED_TARGET_NAME_FROM_PATH[[rst]] expects exactly one argument: a project root path"
	}

	if root := strings.Trim(args[0], "/"); root == "" || !strings.HasPrefix(modulePath, root+"/") {
		return "[[imp]]CMAKE_EXPORTED_TARGET_NAME_FROM_PATH[[rst]]: module '[[imp]]" + modulePath + "[[rst]]' is not strictly inside '[[unimp]]" + args[0] + "[[rst]]'"
	}

	return ""
}

func iosAssetsErrors(args []string) []string {
	var root string

	var contents []string

	key := ""

	for _, arg := range args {
		switch {
		case arg == "ROOT" || arg == "CONTENTS" || arg == "FLAGS":
			key = arg
		case key == "ROOT":
			root = arg
		case key == "CONTENTS":
			contents = append(contents, arg)
		}
	}

	var errs []string

	if root == "" && len(contents) > 0 {
		errs = append(errs, "Please specify ROOT directory for assets")
	}

	for _, content := range contents {
		if rel, err := filepath.Rel(path.Clean(root), path.Clean(content)); err == nil && strings.HasPrefix(rel, "..") {
			errs = append(errs, content+" is not subpath of "+root)
		}
	}

	return errs
}

func contribCreditsMissing(modulePath string, args []string, env Environment) bool {
	var excepts []string

	if i := slices.Index(args, "EXCEPT"); i >= 0 {
		args, excepts = args[:i], args[i+1:]
	}

	if env.string(envCreditsTextsFile) != "" || env.string(envNoCreditsTextsFile) != "" {
		return false
	}

	for _, arg := range args {
		if strings.HasPrefix(modulePath, arg) && !slices.ContainsFunc(excepts, func(ex string) bool { return strings.HasPrefix(modulePath, ex) }) {
			return true
		}
	}

	return false
}

func aiReferenceMissing(fs FS, args []string) []string {
	var missing []string

	for _, arg := range args {
		if present, _ := fs.exists(srcRootRel, path.Clean(arg)); strings.HasPrefix(arg, "/") || !present {
			missing = append(missing, arg)
		}
	}

	return missing
}

func clangWarningsErrors(args []ANY) []string {
	var errs []string

	for _, arg := range args {
		if item := arg.string(); strings.HasPrefix(item, "-Wno-") || !strings.HasPrefix(item, "-W") {
			errs = append(errs, "Warning tag [[alt1]]"+item+"[[rst]] is not allowed in call to macro [[alt1]]CLANG_WARNINGS[[rst]]")
		}
	}

	return errs
}

func fsStatementEnd(fs FS, dir string, line int, name string) (int, int) {
	return statementEnd(concatChunks(fs.read(joinRel(dir, "ya.make"))), line, name)
}

func dataStatementErrors(fs FS, modulePath string, v UnknownStmt, onWarn func(Warn)) {
	report := func(msg string) {
		row, col := fsStatementEnd(fs, modulePath, v.Line, "DATA")

		onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: msg, Row: row, Column: col})
	}

	const (
		autoNo = iota
		autoMaybe
		autoSeen
	)

	auto := autoNo

	missingConfig := func(arg string) {
		if auto == autoSeen {
			report("[[alt1]]AUTOUPDATED[[rst]] in [[alt1]]DATA[[rst]] section should be followed by configuration name, found: " + arg)
		}
	}

	for _, arg := range anyStrs(v.Args) {
		switch {
		case strings.HasPrefix(arg, "sbr://"):
			missingConfig(arg)

			auto = autoMaybe
		case strings.HasPrefix(arg, "ext:"):
			file := strings.TrimPrefix(arg, "ext:")

			if present, _ := fs.exists(dirKey(modulePath), file); !present {
				if external, _ := fs.exists(dirKey(modulePath), file+".external"); !external {
					report("Neither actual data nor .external file exists for [[imp]]" + file + "[[rst]] inside [[alt1]]DATA[[rst]] section.")
				}
			}

			missingConfig(arg)

			auto = autoMaybe
		case strings.HasPrefix(arg, "arcadia/"):
			missingConfig(arg)

			auto = autoNo
		case arg == "AUTOUPDATED":
			missingConfig(arg)

			if auto == autoMaybe {
				auto = autoSeen
			} else {
				report("[[alt1]]AUTOUPDATED[[rst]] in [[alt1]]DATA[[rst]] section should have path with [[imp]]sbr://[[rst]] before it, ignored")
			}
		case auto == autoSeen:
			auto = autoNo
		default:
			report("Path [[imp]]" + arg + "[[rst]] in [[alt1]]DATA[[rst]] section should start with one of the following prefixes: [[imp]]arcadia/, sbr://, ext:[[rst]]")

			auto = autoNo
		}
	}

	missingConfig("<none>")
}

func incompatiblePeerWarn(instance ModuleInstance, p STR) Warn {
	return Warn{
		Kind:    WarnModuleFailed,
		Message: "PEERDIR from " + instance.Path.relString() + " to " + p.string() + " is not allowed and skipped: module types are incompatible",
		Where:   makeFileWhere(instance.Path.relString()),
		Error:   "[[alt1]]PEERDIR[[rst]] to [[imp]]$S/" + p.string() + "[[rst]] is not allowed and skipped: module types are incompatible",
	}
}

func internalPeerDenied(from, to string, goPeer bool, env Environment) bool {
	if !strings.Contains(to, "internal") {
		return false
	}

	dir := "$S/" + to

	if !goPeer {
		if !env.bool(envCheckInternal) {
			return false
		}

		for _, exception := range strings.Fields(env.string(envInternalExceptions)) {
			if strings.Contains(dir, exception) {
				return false
			}
		}
	}

	parent, child := strings.Split("$S/"+from, "/"), strings.Split(dir, "/")
	prefix := 0

	for prefix < len(parent) && prefix < len(child) && parent[prefix] == child[prefix] {
		prefix++
	}

	return slices.Contains(child[min(prefix+1, len(child)):], "internal")
}

func (ctx *GenCtx) toolDirError(dir STR) string {
	switch present, isDir := ctx.fs.existsID(srcRootRel, dir); {
	case !present || !isDir:
		return "Trying to use [[alt1]]TOOL[[rst]] from missing directory: [[imp]]$S/" + dir.string() + "[[rst]]"
	case !ctx.fs.isFileID(dir, strYaMake):
		return "Trying to use [[alt1]]TOOL[[rst]] from directory without ya.make: [[imp]]$S/" + dir.string() + "[[rst]]"
	}

	return ""
}

func (ctx *GenCtx) toolWithoutModules(dir STR) string {
	ctx.dirEntry(dir.source(), ctx.target)
	ctx.noModuleRef(dir.source())

	return "[[alt1]]TOOL[[rst]] not found: no modules in $S/" + dir.string()
}

func (ctx *GenCtx) dependsDirError(dir STR) string {
	rel := dir.sourceClean().rel()

	switch present, isDir := ctx.fs.existsID(srcRootRel, rel); {
	case present && !isDir:
		return "DEPENDS to non-directory $S/" + rel.string()
	case !present || !ctx.fs.isFileID(rel, strYaMake):
		return "[[alt1]]DEPENDS[[rst]] to " + ctx.missingDirectory(rel) + "$S/" + rel.string()
	case ctx.dirDeclaration(dir.sourceClean(), ctx.target) == nil:
		return "[[alt1]]DEPENDS[[rst]] to directory without module: $S/" + rel.string()
	}

	return ""
}

func (ctx *GenCtx) dependsMultimoduleError(dir STR) string {
	instances := ctx.testDependencyInstances(dir)

	if len(instances) < 2 {
		return ""
	}

	for _, instance := range instances {
		if ctx.runtimeTestDependencyModule(TestBuildDependency{instance, genModule(ctx, instance)}).final {
			return ""
		}
	}

	return "[[alt1]]DEPENDS[[rst]] on a multimodule without final targets is not allowed: $S/" + dir.sourceClean().relString()
}

func utilUint32(v string) bool {
	_, err := strconv.ParseUint(strings.TrimPrefix(v, "+"), 10, 32)

	return err == nil && v != "+"
}

func tsConfErrorMessage(args []string) string {
	return tsColors.Replace(strings.ReplaceAll(strings.Join(args, " "), "\\n", "\n"))
}

func (c *NotsModule) configureErrors(ctx *GenCtx, modulePath string, decl *ModuleStmt, env Environment, report func(string)) {
	if c.OutputPrefixInvalid {
		report("TS_OUTPUT_PREFIX accepts at most one path")
	}

	switch c.Kind {
	case tokTsLibrary, tokTsPackage:
		c.libraryConfigureErrors(ctx, modulePath, env, report)
	case tokTsTestFor:
		if len(c.Checks) == 0 && env.string(envCppAnalysisMode) != "yes" {
			report(tsRed + "Missing test script" + tsReset + " \n" + tsCyan + "TS_TEST_FOR" + tsReset + " requires to use at least one " + tsCyan + "TS_TEST" + tsReset + " macro \nhttps://docs.yandex-team.ru/frontend-in-arcadia/references/TS_TEST_FOR")
		}
	}

	if decl == nil || c.Kind != tokTsTestFor {
		return
	}

	relative, target := false, ""

	for _, arg := range decl.Args {
		if arg == kwRELATIVE.any() {
			relative = true
		} else {
			target = arg.string()
		}
	}

	switch {
	case target == "." || target == "./":
		report("Tests should be for parent module but got path '" + target + "'")
	case relative && env.string(envAllowRelForPath) == "no":
		report("TS_TEST_FOR does not support RELATIVE path.\nUpdate your module to " + tsCyan + "TS_TEST_FOR(" + path.Clean(modulePath+"/"+target) + ")" + tsReset + "\nSee more details in https://st.yandex-team.ru/FBP-3073")
	case !relative && strings.HasPrefix(target, "."):
		report("You are using a relative path for a module. You have to add RELATIVE key, like (RELATIVE " + target + ")")
	}
}

func (c *NotsModule) libraryConfigureErrors(ctx *GenCtx, modulePath string, env Environment, report func(string)) {
	pkg := c.Kind == tokTsPackage

	if c.BuildScript == "" && !pkg {
		report("Module build script is required but not set.\nUse macro " + tsCyan + "TS_BUILD_SCRIPT(nots:build)" + tsReset + " to set it up.")

		return
	}

	normalize := func(p string) string { return strings.Trim(strings.TrimPrefix(p, "./"), "/") }

	var outputs []string

	for _, output := range c.Outputs {
		outputs = append(outputs, normalize(output))
	}

	if len(outputs) == 0 {
		if !pkg {
			report("Module outputs are required but not set.\nUse macro " + tsCyan + "TS_BUILD_OUTPUTS(build)" + tsReset + " to set it up.")
		}

		return
	}

	parsed := ctx.parsers.parsedJSONID(internStr(joinRel(modulePath, "package.json")))
	pj, _ := parsed.Value.(map[string]any)
	declared, _ := pj["files"].([]any)

	var files, missing []string

	for _, file := range declared {
		if name, ok := file.(string); ok {
			files = append(files, normalize(name))
		}
	}

	for _, output := range outputs {
		if !slices.ContainsFunc(files, func(file string) bool {
			return file == output || strings.HasPrefix(file, output+"/") || strings.HasPrefix(output, file+"/")
		}) {
			missing = append(missing, output)
		}
	}

	if len(missing) > 0 && env.string(envTsLegacyFacade) != "yes" {
		report("Directories from " + tsCyan + "TS_BUILD_OUTPUTS()" + tsReset + " are expected to be listed in " + tsCyan + "package.json#files" + tsReset + ".\nFollowing directories are missing in " + tsCyan + "package.json#files" + tsReset + ": " + tsRed + strings.Join(missing, ", ") + tsReset)
	}
}

func dropAbsoluteSources(sources []ANY, modulePath string, onWarn func(Warn)) []ANY {
	for i, src := range sources {
		if src.vfs() != 0 || !strings.HasPrefix(src.string(), "/") {
			continue
		}

		kept := slices.Clone(sources[:i])

		for _, rest := range sources[i:] {
			if rest.vfs() == 0 && strings.HasPrefix(rest.string(), "/") {
				onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(modulePath), Error: "source file name is outside the build tree: " + rest.string()})
			} else {
				kept = append(kept, rest)
			}
		}

		return kept
	}

	return sources
}
