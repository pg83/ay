package main

import (
	"strings"
	"unicode"
)

type SourceTextRange struct{ Start, End int }
type SourceTextTracker struct{ ranges []SourceTextRange }

func (e Environment) setTextInputs(id ENV, ranges []SourceTextRange) {
	if len(ranges) == 0 {
		return
	}

	if e.s.textInputs == nil {
		e.s.textInputs = make(map[ENV][]SourceTextRange)
	}

	e.s.textInputs[id] = ranges
}

func (t *SourceTextTracker) replace(start, end, size int, inserted []SourceTextRange) {
	var next []SourceTextRange

	delta := size - (end - start)

	for _, r := range t.ranges {
		if r.Start < start {
			next = append(next, SourceTextRange{r.Start, min(r.End, start)})
		}

		if r.End > end {
			next = append(next, SourceTextRange{max(r.Start, end) + delta, r.End + delta})
		}
	}

	for _, r := range inserted {
		next = append(next, SourceTextRange{start + r.Start, start + r.End})
	}

	t.ranges = next
}

func (t *SourceTextTracker) contains(start, end int) bool {
	for _, r := range t.ranges {
		if r.Start < end && r.End > start {
			return true
		}
	}

	return false
}

func (t *SourceTextTracker) literal(pos int) bool {
	return t != nil && t.contains(pos, pos+1)
}

func expandSourceScalarInputValue(value string, env Environment) (string, []SourceTextRange) {
	if len(env.s.textInputs) == 0 {
		return expandSourceScalarVarRef(value, env), nil
	}

	var track SourceTextTracker

	value = expandMakeExpressionTracked(value, env, func(name string) bool {
		return !tokenExpandAssignment.clearsUnbound(name) || strings.HasSuffix(name, resourceGlobalSuffix)
	}, &track)

	return value, track.ranges
}

func runProgramSourceInputArguments(args []ANY, env Environment) []ANY {
	if len(env.s.textInputs) == 0 {
		return runProgramSourceArguments(args, env)
	}

	words, text := runProgramSourceArgumentValues(args, env, true)

	var out []ANY

	section := kwARGS.any()

	for i, word := range words {
		if runProgramKeywords.has(uint32(word.str())) {
			section = word
		}

		if text[i] && (section == kwIN.any() || section == kwIN_DEPS.any()) {
			out = append(out, kwIN_NOPARSE.any(), word, section)
		} else {
			out = append(out, word)
		}
	}

	return out
}

func prepareRunProgramInputArguments(args []ANY, env Environment) ([]ANY, []ANY) {
	if len(env.s.textInputs) == 0 {
		return prepareRunProgramArguments(args, env), nil
	}

	var parsed, text []ANY

	for _, arg := range args {
		var track SourceTextTracker

		value := continueConfigMacroBodyTracked(arg.string(), env, 0, nil, &track)

		for _, span := range sourceWordSpans(value, func(r rune) bool { return r == ' ' || r == ',' }) {
			word := internAny(value[span.Start:span.End])

			if track.contains(span.Start, span.End) {
				text = append(text, word)
			} else {
				parsed = append(parsed, word)
			}
		}
	}

	return parsed, text
}

func expandSourceInputTokens(args []ANY, env Environment, phase TokenExpansion) ([]ANY, []ANY) {
	if len(env.s.textInputs) == 0 {
		return expandTokens(args, env, false, phase), nil
	}

	var parsed, text []ANY

	for _, arg := range args {
		if !strHasDollar(arg.str().any()) {
			parsed = append(parsed, arg)

			continue
		}

		var track SourceTextTracker

		value := expandTokenTracked(arg.string(), env, phase, &track)

		for _, span := range sourceWordSpans(value, unicode.IsSpace) {
			word := internAny(value[span.Start:span.End])

			if track.contains(span.Start, span.End) {
				text = append(text, word)
			} else {
				parsed = append(parsed, word)
			}
		}
	}

	return parsed, text
}

type sourceTextAppend struct {
	id     ENV
	ranges []SourceTextRange
}

func captureSourceTextAppend(v *UnknownStmt, env Environment) *sourceTextAppend {
	if v.Name != tokSetAppend || len(v.Args) == 0 || len(env.s.textInputs) == 0 {
		return nil
	}

	id := internEnv(expandStmtToken(v.Args[0].string(), env))
	out := &sourceTextAppend{id: id, ranges: append([]SourceTextRange(nil), env.s.textInputs[id]...)}
	offset := 0

	if prev := env.string(id); prev != "" {
		offset = len(prev) + 1
	}

	for _, arg := range v.Args[1:] {
		if !strHasDollar(arg.str().any()) {
			offset += len(arg.string()) + 1

			continue
		}

		var track SourceTextTracker

		value := expandTokenTracked(arg.string(), env, tokenExpandLazy, &track)

		for _, span := range sourceWordSpans(value, unicode.IsSpace) {
			size := span.End - span.Start

			if track.contains(span.Start, span.End) {
				out.ranges = append(out.ranges, SourceTextRange{offset, offset + size})
			}

			offset += size + 1
		}
	}

	return out
}

func sourceWordSpans(value string, space func(rune) bool) []SourceTextRange {
	var spans []SourceTextRange

	start := -1

	for i, r := range value {
		if space(r) {
			if start >= 0 {
				spans = append(spans, SourceTextRange{start, i})
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}

	if start >= 0 {
		spans = append(spans, SourceTextRange{start, len(value)})
	}

	return spans
}

func sliceSourceTextRanges(ranges []SourceTextRange, start, end int) []SourceTextRange {
	var sliced []SourceTextRange

	for _, r := range ranges {
		lo, hi := max(r.Start, start), min(r.End, end)

		if lo < hi {
			sliced = append(sliced, SourceTextRange{lo - start, hi - start})
		}
	}

	return sliced
}
