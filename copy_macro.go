package main

import "strings"

var copyMacroKeywords = map[string]int{
	"RESULT":          1,
	"FROM":            1,
	"DESTINATION":     1,
	"OUTPUT_INCLUDES": -1,
	"KEEP_DIR_STRUCT": 0,
	"AUTO":            0,
	"WITH_CONTEXT":    0,
}

func parseCopyEntries(args []string, line int) ([]CopyFileEntry, string) {
	values := make(map[string]string)

	var files, outputIncludes []string

	arrayStarted := false

	for i := 0; i < len(args); i++ {
		arg := args[i]
		count, keyword := copyMacroKeywords[arg]

		if !keyword {
			if arrayStarted {
				throwFmt("gen: COPY at line %d: Mixing positional arguments with named arrays is error prone and forbidden. Got positional arg %q after named array 'OUTPUT_INCLUDES' has been started.", line, arg)
			}

			files = append(files, arg)

			continue
		}

		if _, seen := values[arg]; seen && count != -1 {
			throwFmt("gen: COPY at line %d: Keyword '%s' appears more than once and is not an array.", line, arg)
		}

		switch count {
		case 0:
			values[arg] = "yes"
		case 1:
			if i+1 >= len(args) {
				throwFmt("gen: COPY at line %d: Missing value for scalar keyword '%s'.", line, arg)
			}

			if _, nextKeyword := copyMacroKeywords[args[i+1]]; nextKeyword {
				throwFmt("gen: COPY at line %d: Missing value for scalar keyword '%s'.", line, arg)
			}

			i++
			values[arg] = args[i]
		case -1:
			arrayStarted = true

			for i+1 < len(args) {
				if _, nextKeyword := copyMacroKeywords[args[i+1]]; nextKeyword {
					break
				}

				i++
				outputIncludes = append(outputIncludes, args[i])
			}
		}
	}

	out := make([]CopyFileEntry, 0, len(files))

	for _, file := range files {
		parts := strings.Split(file, "/")
		filename := parts[len(parts)-1]
		relPath := ""

		if values["KEEP_DIR_STRUCT"] != "" {
			relPath = copyMacroJoin(parts[:len(parts)-1]...)
		}

		src := copyMacroJoin(values["FROM"], relPath, filename)
		dst := copyMacroJoin(values["DESTINATION"], relPath, filename)
		includes := append([]string(nil), outputIncludes...)

		if values["WITH_CONTEXT"] != "" {
			includes = append(includes, src)
		}

		entry := parseCopyFileEntry(append([]string{src, dst, "OUTPUT_INCLUDES"}, includes...), false, line)

		entry.Auto = values["AUTO"] != ""
		out = append(out, entry)
	}

	return out, values["RESULT"]
}

func copyMacroJoin(parts ...string) string {
	result := ""

	for _, part := range parts {
		if strings.HasPrefix(part, "/") || result == "" {
			result = part
		} else if strings.HasSuffix(result, "/") {
			result += part
		} else {
			result += "/" + part
		}
	}

	return result
}
