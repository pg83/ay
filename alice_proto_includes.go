package main

import (
	"encoding/base64"
	"encoding/json"
	"path"
	"regexp"
	"slices"
	"strings"
)

var (
	aliceProtoLineComment  = regexp.MustCompile(`(?m)//.*$`)
	aliceProtoBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	aliceProtoPackage      = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_])package[\s\p{Z}\x0b\x85\x1c-\x1f]+([^;]+);`)
	aliceProtoMessage      = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_])(message)[\s\p{Z}\x0b\x85\x1c-\x1f]+([\p{L}\p{N}_]+)[\s\p{Z}\x0b\x85\x1c-\x1f]*\{`)
)

func aliceProtoMessages(data []byte) []string {
	content := aliceProtoLineComment.ReplaceAllString(string(data), "")

	content = aliceProtoBlockComment.ReplaceAllString(content, "")

	packageName := ""

	if match := aliceProtoPackage.FindStringSubmatch(content); match != nil && strings.TrimSpace(match[1]) != "" {
		packageName = strings.ReplaceAll(strings.TrimSpace(match[1]), ".", "::") + "::"
	}

	messages := []string{}

	for _, match := range aliceProtoMessage.FindAllStringSubmatchIndex(content, -1) {
		prefix := content[:match[2]]

		if strings.Count(prefix, "{") > strings.Count(prefix, "}") {
			continue
		}

		messages = append(messages, packageName+content[match[4]:match[5]])
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
