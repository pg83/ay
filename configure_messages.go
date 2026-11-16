package main

import (
	"bytes"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	treePluginMacrosOnce sync.Once
	treePluginMacros     map[string]bool
	pluginMacroDef       = regexp.MustCompile(`(?m)^def on([a-z0-9_]+)\(`)
)

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

func testSizeError(size string) string {
	switch size {
	case "", "small", "medium", "large":
		return ""
	}

	return "Unknown test size: [[imp]]" + strings.ToUpper(size) + "[[rst]], choose from [[imp]]SMALL, MEDIUM, LARGE[[rst]]"
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
