package main

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

var (
	boostAtomicMacroIncludeRe = regexp.MustCompile(`(?m)^\s*#\s*include\s+BOOST_[A-Z0-9_]+\(\s*([A-Za-z0-9_./]+)\s*\)`)
	boostABIPrefixIncludeRe   = regexp.MustCompile(`(?m)^\s*#\s*include\s+BOOST_ABI_PREFIX\b`)
	boostABISuffixIncludeRe   = regexp.MustCompile(`(?m)^\s*#\s*include\s+BOOST_ABI_SUFFIX\b`)
	boostPlatformIncludeRe    = regexp.MustCompile(`(?m)^\s*#\s*include\s+BOOST_PLATFORM_CONFIG\b`)
	boostPPIterateIncludeRe   = regexp.MustCompile(`(?m)^\s*#\s*include\s+BOOST_PP_ITERATE\(\)`)
	boostPPSlotIncludeRe      = regexp.MustCompile(`(?m)^\s*#\s*include\s+BOOST_PP_ASSIGN_SLOT\(`)
)

var (
	boostMPLPPIterateTargets = []string{
		"contrib/restricted/boost/mpl/include/boost/mpl/apply.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/apply_fwd.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/apply_wrap.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/arg.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/advance_backward.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/advance_forward.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/fold_impl_body.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/full_lambda.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/lambda_no_ctps.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/numeric_op.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/reverse_fold_impl_body.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/sequence_wrapper.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/aux_/template_arity.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/bind.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/bind_fwd.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/inherit.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/list/aux_/numbered.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/list/aux_/numbered_c.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/map/aux_/numbered.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/placeholders.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/quote.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/set/aux_/numbered.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/set/aux_/numbered_c.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/unpack_args.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/vector/aux_/numbered.hpp",
		"contrib/restricted/boost/mpl/include/boost/mpl/vector/aux_/numbered_c.hpp",
	}
	boostUtilityPPIterateTargets = []string{
		"contrib/restricted/boost/utility/include/boost/utility/in_place_factory.hpp",
		"contrib/restricted/boost/utility/include/boost/utility/detail/result_of_iterate.hpp",
		"contrib/restricted/boost/utility/include/boost/utility/typed_in_place_factory.hpp",
	}
)

// augmentMacroExpandedIncludes adds the macro-driven include edges that
// upstream ymake's conditional-blind scanner sees in Boost dispatcher
// headers. These are kept parser-side so the per-source parse cache owns
// them and every later closure walk reuses the same expanded directives.
func augmentMacroExpandedIncludes(fs *FS, rel string, data []byte, set parsedIncludeSet) parsedIncludeSet {
	extras := macroExpandedIncludes(fs, rel, data, set.bucket(parsedIncludesLocal))
	if len(extras) == 0 {
		return set
	}

	return appendParsedDirectives(set, parsedIncludesLocal, extras...)
}

func macroExpandedIncludes(fs *FS, rel string, data []byte, existing []includeDirective) []includeDirective {
	seen := make(map[includeDirective]struct{}, len(existing)+64)
	for _, d := range existing {
		seen[d] = struct{}{}
	}
	extras := make([]includeDirective, 0, 64)

	add := func(target string) {
		target = cleanRel(target)
		if target == "" {
			return
		}

		d := includeDirective{kind: includeQuoted, target: target}
		if _, dup := seen[d]; dup {
			return
		}

		seen[d] = struct{}{}
		extras = append(extras, d)
	}

	if strings.HasPrefix(rel, "contrib/restricted/boost/mpl/") && strings.HasSuffix(rel, "include_preprocessed.hpp") {
		addRecursiveMatchingFiles(fs, path.Join(path.Dir(rel), "preprocessed"), func(child string) bool {
			return strings.HasSuffix(child, ".hpp")
		}, add)
	}

	for _, m := range boostAtomicMacroIncludeRe.FindAllSubmatch(data, -1) {
		addPrefixMatchedFiles(fs, boostIncludePrefixRel(rel, string(m[1])), add)
	}

	if boostABIPrefixIncludeRe.Match(data) {
		add("contrib/restricted/boost/config/include/boost/config/abi/msvc_prefix.hpp")
	}

	if boostABISuffixIncludeRe.Match(data) {
		add("contrib/restricted/boost/config/include/boost/config/abi/msvc_suffix.hpp")
	}

	if boostPlatformIncludeRe.Match(data) {
		addRecursiveMatchingFiles(fs, "contrib/restricted/boost/config/include/boost/config/platform", func(child string) bool {
			return strings.HasSuffix(child, ".hpp")
		}, add)
	}

	if boostPPIterateIncludeRe.Match(data) {
		addRecursiveMatchingFiles(fs, "contrib/restricted/boost/preprocessor/include/boost/preprocessor/iteration/detail/bounds", func(child string) bool {
			return strings.HasSuffix(child, ".hpp")
		}, add)
		addRecursiveMatchingFiles(fs, "contrib/restricted/boost/preprocessor/include/boost/preprocessor/iteration/detail/iter", func(child string) bool {
			return strings.HasSuffix(child, ".hpp")
		}, add)

		if strings.HasPrefix(rel, "contrib/restricted/boost/utility/") {
			addTargetSet(rel, add, boostUtilityPPIterateTargets)
		}

		if strings.HasPrefix(rel, "contrib/restricted/boost/mpl/") {
			addTargetSet(rel, add, boostMPLPPIterateTargets)
		}
	}

	if boostPPSlotIncludeRe.Match(data) {
		add("contrib/restricted/boost/preprocessor/include/boost/preprocessor/slot/detail/shared.hpp")
	}

	if len(extras) == 0 {
		return nil
	}

	sort.Slice(extras, func(i, j int) bool {
		if extras[i].target != extras[j].target {
			return extras[i].target < extras[j].target
		}
		return extras[i].kind < extras[j].kind
	})

	return extras
}

func boostIncludePrefixRel(rel, includePrefix string) string {
	includePrefix = cleanRel(includePrefix)
	if includePrefix == "" {
		return ""
	}

	idx := strings.Index(rel, "/include/")
	if idx < 0 {
		return ""
	}

	return cleanRel(rel[:idx+len("/include/")] + includePrefix)
}

func addTargetSet(rel string, add func(target string), targets []string) {
	for _, target := range targets {
		if target == rel {
			continue
		}
		add(target)
	}
}

func addPrefixMatchedFiles(fs *FS, prefixRel string, add func(target string)) {
	if prefixRel == "" {
		return
	}

	dir := cleanRel(path.Dir(prefixRel))
	base := path.Base(prefixRel)
	if base == "." || base == "" {
		return
	}

	for name, isDir := range fs.Listdir(dir) {
		if isDir || !strings.HasPrefix(name, base) || !strings.HasSuffix(name, ".hpp") {
			continue
		}
		switch path.Join(dir, name) {
		case "contrib/restricted/boost/atomic/include/boost/atomic/detail/caps_arch_gcc_arm.hpp",
			"contrib/restricted/boost/atomic/include/boost/atomic/detail/core_arch_ops_gcc_arm.hpp",
			"contrib/restricted/boost/atomic/include/boost/atomic/detail/fence_arch_ops_gcc_arm.hpp":
			continue
		}

		add(path.Join(dir, name))
	}
}

func addRecursiveMatchingFiles(fs *FS, rootRel string, match func(child string) bool, add func(target string)) {
	rootRel = cleanRel(rootRel)
	if rootRel == "" || !fs.IsDir(rootRel) {
		return
	}

	fs.Walk(rootRel, func(child string, isDir bool) {
		if isDir || !match(child) {
			return
		}
		if child == "contrib/restricted/boost/mpl/include/boost/mpl/aux_/preprocessed/plain/vector.hpp" {
			return
		}

		add(child)
	})
}
