package main

import "strings"

func runProgramSourceArguments(args []ANY, env Environment) []ANY {
	values, _ := runProgramSourceArgumentValues(args, env, false)

	return values
}

func runProgramSourceArgumentValues(args []ANY, env Environment, withText bool) ([]ANY, []bool) {
	var values []ANY
	var text []bool

	for _, arg := range args {
		value := arg.string()

		var track SourceTextTracker

		spans := []SourceTextRange{{0, len(value)}}

		if strings.ContainsRune(value, '$') {
			if withText {
				value = expandMakeExpressionTracked(value, env, runProgramReservedVariable, &track)
			} else {
				value = expandMakeExpression(value, env, runProgramReservedVariable)
			}

			spans = sourceWordSpans(value, func(r rune) bool { return r == ' ' })
		} else if value == "" {
			value = `""`
			spans[0].End = len(value)
		}

		for _, span := range spans {
			word := value[span.Start:span.End]

			if word == "&&" || word == ";" {
				word = `${quo:"` + word + `"}`
			}

			values = append(values, internAny(word))

			if withText {
				text = append(text, track.contains(span.Start, span.End))
			}
		}
	}

	return values, text
}

func runProgramReservedVariable(name string) bool {
	return keepDeferredVar(name) || name == "ARCADIA_ROOT" || name == "ARCADIA_BUILD_ROOT"
}

func bindRunProgramCalls(d *ModuleData, env Environment) {
	if len(d.runPrograms) == 0 {
		return
	}

	locals := env.clone()

	for i, kind := range locals.s.kind {
		if kind != envAbsent && runProgramReservedVariable(ENV(i).string()) {
			locals.unbind(ENV(i))
		}
	}

	for i, captured := range d.runPrograms {
		if captured.CallArgs == nil {
			continue
		}

		bound := parseRunProgram(captured.CallArgs, captured.Line)
		args := prepareRunProgramArguments(append([]ANY{bound.ToolPath}, bound.Args...), locals)

		if len(args) == 0 {
			throwFmt("RUN_PROGRAM expects at least 1 argument (tool path)")
		}

		bound.ToolPath, bound.Args = args[0], args[1:]

		split := len(bound.INFiles) - bound.INNoParse
		parsed, lateText := prepareRunProgramInputArguments(bound.INFiles[:split], locals)
		opaque := append(lateText, prepareRunProgramArguments(bound.INFiles[split:], locals)...)

		bound.INFiles, bound.INNoParse = append(parsed, opaque...), len(opaque)
		bound.OUTFiles = prepareRunProgramArguments(bound.OUTFiles, locals)
		bound.OUTNoAutoFiles = prepareRunProgramArguments(bound.OUTNoAutoFiles, locals)
		bound.ToolPaths = prepareRunProgramArguments(bound.ToolPaths, locals)
		bound.OutputIncludes = prepareRunProgramArguments(bound.OutputIncludes, locals)
		bound.InducedDeps = prepareRunProgramArguments(bound.InducedDeps, locals)
		bound.EnvPairs = prepareRunProgramArguments(bound.EnvPairs, locals)

		if bound.StdoutFile != nil {
			value := internStr(strings.Join(anyStrs(prepareRunProgramArguments([]ANY{*bound.StdoutFile}, locals)), " ")).any()

			bound.StdoutFile = &value
		}

		if bound.CWD != nil {
			value := internStr(strings.Join(anyStrs(prepareRunProgramArguments([]ANY{*bound.CWD}, locals)), " ")).any()

			bound.CWD = &value
		}

		bound.KV, bound.DeclSeq = captured.KV, captured.DeclSeq
		bound.CallArgs = captured.CallArgs

		d.runPrograms[i] = bindRunProgramFiles(bound, env)
	}
}

func prepareRunProgramArguments(args []ANY, env Environment) []ANY {
	text := expandConfigMacroBody(strings.Join(anyStrs(args), " "), env)

	return internAnys(strings.FieldsFunc(text, func(r rune) bool { return r == ' ' || r == ',' }))
}
