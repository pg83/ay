package main

import (
	"encoding/base64"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

func aliceProtoStripComments(data []byte) string {
	var b strings.Builder

	b.Grow(len(data))

	for len(data) > 0 {
		i := strings.Index(string(data), "//")

		if i < 0 {
			b.Write(data)

			break
		}

		b.Write(data[:i])
		data = data[i:]

		if nl := strings.IndexByte(string(data), '\n'); nl >= 0 {
			data = data[nl:]
		} else {
			data = nil
		}
	}

	s := b.String()

	if !strings.Contains(s, "/*") {
		return s
	}

	var out strings.Builder

	out.Grow(len(s))

	for len(s) > 0 {
		i := strings.Index(s, "/*")

		if i < 0 {
			break
		}

		end := strings.Index(s[i+2:], "*/")

		if end < 0 {
			break
		}

		out.WriteString(s[:i])
		s = s[i+2+end+2:]
	}

	out.WriteString(s)

	return out.String()
}

func aliceProtoSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\f', '\r', '\v', 0x85:
		return true
	}

	return r >= 0x1c && r <= 0x1f || unicode.Is(unicode.Z, r)
}

func aliceProtoWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}

func aliceProtoKeyword(content, keyword string, at, from int) bool {
	if !strings.HasPrefix(content[at:], keyword) {
		return false
	}

	if at == 0 {
		return true
	}

	r, size := utf8.DecodeLastRuneInString(content[:at])

	return at-size >= from && !aliceProtoWord(r)
}

func aliceProtoSpaces(content string, i int) int {
	for i < len(content) {
		r, size := utf8.DecodeRuneInString(content[i:])

		if !aliceProtoSpace(r) {
			break
		}

		i += size
	}

	return i
}

func aliceProtoPackageName(content string) string {
	for from := 0; ; {
		at := strings.Index(content[from:], "package")

		if at < 0 {
			return ""
		}

		at += from

		if aliceProtoKeyword(content, "package", at, 0) {
			start := at + len("package")
			spaces := aliceProtoSpaces(content, start)

			if spaces > start {
				if semicolon := strings.IndexByte(content[spaces:], ';'); semicolon > 0 {
					return content[spaces : spaces+semicolon]
				} else if semicolon == 0 {
					_, size := utf8.DecodeLastRuneInString(content[:spaces])

					if spaces-size > start {
						return content[spaces-size : spaces]
					}
				}
			}
		}

		from = at + 1
	}
}

func aliceProtoMessages(data []byte) []string {
	content := aliceProtoStripComments(data)
	packageName := ""

	if name := strings.TrimSpace(aliceProtoPackageName(content)); name != "" {
		packageName = strings.ReplaceAll(name, ".", "::") + "::"
	}

	messages := []string{}
	depth := 0
	counted := 0

	for from := 0; ; {
		at := strings.Index(content[from:], "message")

		if at < 0 {
			break
		}

		at += from

		if !aliceProtoKeyword(content, "message", at, from) {
			from = at + 1

			continue
		}

		start := at + len("message")
		spaces := aliceProtoSpaces(content, start)
		nameEnd := spaces

		for nameEnd < len(content) {
			r, size := utf8.DecodeRuneInString(content[nameEnd:])

			if !aliceProtoWord(r) {
				break
			}

			nameEnd += size
		}

		brace := aliceProtoSpaces(content, nameEnd)

		if spaces == start || nameEnd == spaces || brace >= len(content) || content[brace] != '{' {
			from = at + 1

			continue
		}

		depth += strings.Count(content[counted:at], "{") - strings.Count(content[counted:at], "}")
		counted = at

		if depth <= 0 {
			messages = append(messages, packageName+content[spaces:nameEnd])
		}

		from = brace + 1
	}

	return messages
}

func aliceProtoRequestJSON(request map[string][]string) string {
	quote := func(value string) string {
		var buf strings.Builder

		encoder := json.NewEncoder(&buf)

		encoder.SetEscapeHTML(false)
		throw(encoder.Encode(value))

		encoded := strings.TrimSuffix(buf.String(), "\n")

		var out strings.Builder

		for i := 0; i < len(encoded); {
			if encoded[i] != '\\' {
				out.WriteByte(encoded[i])
				i++

				continue
			}

			switch {
			case strings.HasPrefix(encoded[i:], `\u2028`):
				out.WriteRune('\u2028')
				i += 6
			case strings.HasPrefix(encoded[i:], `\u2029`):
				out.WriteRune('\u2029')
				i += 6
			default:
				out.WriteString(encoded[i : i+2])
				i += 2
			}
		}

		return out.String()
	}

	keys := make([]string, 0, len(request))

	for key := range request {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	parts := make([]string, 0, len(keys))

	for _, key := range keys {
		messages := make([]string, 0, len(request[key]))

		for _, message := range request[key] {
			messages = append(messages, quote(message))
		}

		parts = append(parts, quote(key)+": ["+strings.Join(messages, ", ")+"]")
	}

	return "{" + strings.Join(parts, ", ") + "}"
}

func applyAliceProtoIncludes(fs FS, modulePath string, v UnknownStmt, d *ModuleData) {
	files := anyStrs(v.Args)

	if v.Name == tokAliceGenerateProtoIncludes {
		_, options := goMacroLists(v.Args, "DIR")
		directory := ""

		if dirs := options["DIR"]; len(dirs) > 0 {
			directory = dirs[0].string()
		}

		files = nil

		for _, file := range sortedUnique(ymakeGlob(fs, styleSourcePath(modulePath, path.Join(directory, "**/*.proto")))) {
			files = append(files, "$(S)/"+file)
		}
	}

	request := map[string][]string{}

	var includes []ANY

	for _, file := range files {
		if !strings.HasSuffix(file, ".proto") {
			continue
		}

		input := copyFileInputVFS(fs, source(modulePath), file)

		if !input.isSource() {
			throwFmt("ALICE_GENERATE_FUNCTION_PROTO_INCLUDES needs a source proto file: %s", file)
		}

		messages := aliceProtoMessages(concatChunks(fs.read(input.relString())))
		header := strings.ReplaceAll(input.relString(), ".proto", ".pb.h")
		start := strings.Index(header, "alice/")

		if start < 0 {
			start = len(header) - 1
		}

		header = header[start:]
		request[header] = messages
		includes = append(includes, internStr(header).any())
	}

	d.peerdirs = append(d.peerdirs, strAliceFunctionsProto.any())

	d.runPrograms = append(d.runPrograms, &RunProgramStmt{
		ToolPath: strAliceFunctionsFunctionProtoConvertersGenerator.any(),
		Args: []ANY{strProtoIncludesH.any(),
			internStr(base64.StdEncoding.EncodeToString([]byte(aliceProtoRequestJSON(request)))).any()},
		OUTNoAutoFiles: internAnys([]string{"proto_includes.h", "proto_includes.cpp"}),
		OutputIncludes: includes,
		Line:           v.Line,
		DeclSeq:        d.nextDeclSeq(),
	})

	d.srcs = append(d.srcs, SrcMeta{Source: strProtoIncludesCpp.any(), Prio: stmtPrioDefault, Seq: d.nextDeclSeq()})
}
