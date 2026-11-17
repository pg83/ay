package main

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const licensePropsMax = 31

const (
	constraintNone constraintType = iota
	constraintDeny
	constraintAllowOnly
)

const (
	scopePeers uint8 = 1 << iota
	scopeTools
	scopeAll = scopePeers | scopeTools
)

const (
	transitiveChecked uint8 = 1 << iota
	transitiveLicenseLoaded
	transitiveFeaturesLoaded
	transitiveReach
	transitiveNeeded
)

type constraintType uint8

type licenseProps struct {
	static, dynamic uint32
}

type licenseRestriction struct {
	kind  constraintType
	props uint32
}

func (r licenseRestriction) allows(dynamic bool, licenses []licenseProps) bool {
	return slices.ContainsFunc(licenses, func(l licenseProps) bool {
		props := l.static

		if dynamic {
			props = l.dynamic
		}

		if r.kind == constraintDeny {
			return r.props&props == 0
		}

		return ^r.props&props == 0
	})
}

type dirConstraint struct {
	prefix  string
	glob    *regexp.Regexp
	literal string
	scope   uint8
}

func (c dirConstraint) matches(dir string, scope uint8) bool {
	if c.scope&scope == 0 {
		return false
	}

	if c.literal != "" {
		return dir == c.literal
	}

	if c.glob != nil {
		return c.glob.MatchString(dir)
	}

	return pathIsPrefixOf(c.prefix, dir)
}

type dirConstraints struct {
	kind  constraintType
	items []dirConstraint
}

func (c *dirConstraints) allowed(dir string, scope uint8) bool {
	if c.kind == constraintDeny {
		return !slices.ContainsFunc(c.items, func(item dirConstraint) bool { return item.matches(dir, scope) })
	}

	mismatched := false

	for _, item := range c.items {
		if item.matches(dir, scope) {
			return true
		}

		mismatched = mismatched || item.scope&scope != 0
	}

	return !mismatched
}

func (c *dirConstraints) restricts(scope uint8) bool {
	return slices.ContainsFunc(c.items, func(item dirConstraint) bool { return item.scope&scope != 0 })
}

type featureVersion struct {
	feature, version string
}

type transitiveInfo struct {
	errors            []string
	licenseErrors     []string
	modulewiseErrors  []string
	modulewise        string
	selfExcepted      bool
	provides          []string
	checkFeatures     bool
	dependent         *dirConstraints
	dependentExcept   *dirConstraints
	requires          []string
	restriction       *licenseRestriction
	exceptions        []string
	goTestFor         string
	licenseConstraint bool
}

func (t *transitiveInfo) hasRequirements() bool {
	return t.licenseConstraint || t.dependent != nil || len(t.requires) != 0 || t.checkFeatures
}

type licenseConf struct {
	names        []string
	licenses     map[string]licenseProps
	defaultName  string
	defaultProps []licenseProps
	parsed       map[STR]licenseParse
	fields       map[STR][]string
	restrictions map[restrictionKey]*restrictionParse
	runErrors    []string
}

type restrictionKey struct {
	types, list STR
	modulewise  bool
}

type restrictionParse struct {
	kind constraintType
	r    licenseRestriction
	errs []string
}

type licenseParse struct {
	props []licenseProps
	err   string
}

func pathIsPrefixOf(prefix, path string) bool {
	prefix = strings.TrimSuffix(prefix, "/")

	return strings.HasPrefix(path, prefix) && (len(path) == len(prefix) || path[len(prefix)] == '/')
}

func (ctx *GenCtx) loadLicenseConf(env Environment) *licenseConf {
	groups := map[string]*[2][]string{}

	group := func(name string) *[2][]string {
		if groups[name] == nil {
			groups[name] = &[2][]string{}
		}

		return groups[name]
	}

	report := func(msg string) {
		ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: "At top level", Error: msg})
	}

	add := func(g *[2][]string, kind, license string) {
		if kind != "dynamic" {
			g[0] = append(g[0], license)
		}

		if kind != "static" {
			g[1] = append(g[1], license)
		}
	}

	linkingKind := func(kind string) bool {
		if kind != "default" && kind != "static" && kind != "dynamic" {
			report("Linking type " + kind + " is not supported. Linking type should be static, dynamic or default.")

			return false
		}

		return true
	}

	for _, file := range strings.Fields(env.expand(env.string(envLICENSES))) {
		if !ctx.fs.isFile(srcRootRel, file) {
			report(confFileMissing("license", ctx.fs.root()+"/"+file))

			continue
		}

		root, err := readConfJSON(concatChunks(ctx.fs.read(file)))

		if err != "" {
			report("License config " + ctx.fs.root() + "/" + file + " is invalid. Json syntax error: " + err)

			continue
		}

		if root.kind != confJSONObject {
			report("License conf root section should be a map.")

			continue
		}

		for i, prop := range root.keys {
			section := root.items[i]

			if section.kind != confJSONObject {
				report("License property section with name " + prop + " should be a map.")

				continue
			}

			if prop != "restricted" {
				g := group(prop)

				for k, kind := range section.keys {
					if list := section.items[k]; list.kind != confJSONArray {
						report("List of licenses by path " + prop + "." + kind + "should be an array.")
					} else if linkingKind(kind) {
						for _, license := range list.items {
							if license.kind != confJSONString {
								report("License type by path " + prop + "." + kind + " should be a string.")
							} else {
								add(g, kind, license.str)
							}
						}
					}
				}

				continue
			}

			for j, license := range section.keys {
				linking := section.items[j]

				if linking.kind != confJSONObject {
					report("Linking types section by path " + prop + "." + license + "should be a map.")

					continue
				}

				for k, kind := range linking.keys {
					if props := linking.items[k]; props.kind != confJSONArray {
						report("List of properties by path " + prop + "." + license + "." + kind + "should be an array.")
					} else if linkingKind(kind) {
						for _, p := range props.items {
							if p.kind != confJSONString {
								report("Property type by path " + prop + "." + license + "." + kind + " should be a string.")
							} else {
								add(group(p.str), kind, license)
							}
						}
					}
				}
			}
		}
	}

	c := &licenseConf{licenses: map[string]licenseProps{}, parsed: map[STR]licenseParse{}, fields: map[STR][]string{}, restrictions: map[restrictionKey]*restrictionParse{}}

	for name := range groups {
		c.names = append(c.names, name)
	}

	sort.Strings(c.names)

	if len(c.names) >= licensePropsMax {
		c.runErrors = append(c.runErrors, "To many license properties.")
		c.names = c.names[:licensePropsMax]
	}

	for bit, name := range c.names {
		for i, list := range groups[name] {
			for _, license := range list {
				props := c.licenses[license]

				if i == 0 {
					props.static |= 1 << bit
				} else {
					props.dynamic |= 1 << bit
				}

				c.licenses[license] = props
			}
		}
	}

	c.names = append(c.names, "INVALID")

	invalid := uint32(1) << (len(c.names) - 1)

	c.defaultName = env.expand(env.string(envDEFAULT_MODULE_LICENSE))
	c.defaultProps = []licenseProps{{invalid, invalid}}

	if c.defaultName != "" {
		if parse := c.parse(internStr(c.defaultName)); parse.err == "" {
			c.defaultProps = parse.props
		} else {
			c.runErrors = append(c.runErrors, "Failed to evaluate default license: "+parse.err)
		}
	}

	return c
}

func (c *licenseConf) parse(expr STR) licenseParse {
	if p, ok := c.parsed[expr]; ok {
		return p
	}

	props, err := parseSPDX(c.licenses, expr.string())
	p := licenseParse{props: props, err: err}

	c.parsed[expr] = p

	return p
}

func (c *licenseConf) props(license STR) []licenseProps {
	if license != 0 {
		if parse := c.parse(license); parse.err == "" {
			return parse.props
		}
	}

	return c.defaultProps
}

func (c *licenseConf) fieldsOf(list STR) []string {
	fields, ok := c.fields[list]

	if !ok {
		fields = strings.Fields(list.string())
		c.fields[list] = fields
	}

	return fields
}

func (c *licenseConf) explicitPrefix(env Environment, prefixes, exceptions ENV, modulePath string) string {
	for _, prefix := range c.fieldsOf(env.str(prefixes)) {
		if pathIsPrefixOf(prefix, modulePath) {
			if slices.ContainsFunc(c.fieldsOf(env.str(exceptions)), func(e string) bool { return pathIsPrefixOf(e, modulePath) }) {
				return ""
			}

			return prefix
		}
	}

	return ""
}

func (c *licenseConf) restrictionOf(env Environment, typesVar, listVar ENV, macro string, modulewise bool) *restrictionParse {
	key := restrictionKey{env.str(typesVar), env.str(listVar), modulewise}

	if p, ok := c.restrictions[key]; ok {
		return p
	}

	p := &restrictionParse{}

	if key.types != strEmpty {
		if p.kind = constraintKind(macro, env.expand(key.types.string()), &p.errs); p.kind != constraintNone {
			p.r = c.restriction(p.kind, env.expand(key.list.string()), macro, &p.errs)
		}
	}

	if !strHasDollar(key.types.any()) && !strHasDollar(key.list.any()) {
		c.restrictions[key] = p
	}

	return p
}

func (c *licenseConf) restriction(kind constraintType, list, macro string, errs *[]string) licenseRestriction {
	r := licenseRestriction{kind: kind}

	for _, arg := range strings.Fields(list) {
		if i := slices.Index(c.names, arg); i >= 0 {
			r.props |= 1 << i
		} else {
			*errs = append(*errs, "adds restriction to unknown license property [[alt2]]"+arg+"[[rst]] via [[alt1]]"+macro+"[[rst]]")
		}
	}

	return r
}

type spdxFrame struct {
	props []licenseProps
	last  int
	op    byte
}

func reduceSPDX(prev, top spdxFrame) (spdxFrame, string) {
	op := prev.op

	prev.op = 0

	switch op {
	case '|':
		prev.props = append(prev.props, top.props...)
		prev.last = len(top.props)
	case '&':
		last := slices.Clone(prev.props[len(prev.props)-prev.last:])

		prev.props = prev.props[:len(prev.props)-prev.last]

		for _, p := range top.props {
			for _, l := range last {
				prev.props = append(prev.props, licenseProps{l.static | p.static, l.dynamic | p.dynamic})
			}
		}

		prev.last *= len(top.props)
	default:
		return prev, "Missing operator between ')' and '('"
	}

	return prev, ""
}

func parseSPDX(licenses map[string]licenseProps, expr string) ([]licenseProps, string) {
	top := spdxFrame{op: '|'}

	var tail []spdxFrame
	var unhandled string

	waitWith := false

	opName := func(op byte) string {
		if op == '&' {
			return "AND"
		}

		return "OR"
	}

	handleOp := func(op byte, rhs string) string {
		if waitWith {
			return "No right argument for WITH operator"
		}

		if op == 0 {
			return ""
		}

		props, ok := licenses[rhs]

		if !ok {
			return "Unknown license " + rhs
		}

		if op == '&' {
			for i := len(top.props) - top.last; i < len(top.props); i++ {
				top.props[i].static |= props.static
				top.props[i].dynamic |= props.dynamic
			}
		} else {
			top.props = append(top.props, props)
			top.last = 1
		}

		return ""
	}

	finalize := func() string {
		if unhandled == "" && top.op != 0 {
			return "No right argument for " + opName(top.op) + " operator"
		}

		op, rhs := top.op, unhandled

		top.op, unhandled = 0, ""

		return handleOp(op, rhs)
	}

	rest := expr

	for {
		rest = strings.TrimLeft(rest, " \t\n\r\v\f")

		if rest == "" {
			break
		}

		tok := rest[:1]

		if tok != "(" && tok != ")" {
			end := strings.IndexAny(rest, " \t\n\r\v\f()")

			if end < 0 {
				end = len(rest)
			}

			tok = rest[:end]
		}

		rest = rest[len(tok):]

		switch tok {
		case "(":
			if waitWith {
				return nil, "No right argument for WITH operator"
			}

			if unhandled != "" {
				return nil, "Missing operator between '" + unhandled + "' and '('"
			}

			tail = append(tail, top)
			top = spdxFrame{op: '|'}
		case ")":
			if len(tail) == 0 {
				return nil, "Closing bracket without matching opening bracket"
			}

			if err := finalize(); err != "" {
				return nil, err
			}

			var err string

			if top, err = reduceSPDX(tail[len(tail)-1], top); err != "" {
				return nil, err
			}

			tail = tail[:len(tail)-1]
		case "WITH":
			if unhandled == "" {
				return nil, "No left argument for WITH operator"
			}

			waitWith = true
		case "AND", "OR":
			if unhandled == "" && top.op != 0 {
				return nil, "No left argument for " + tok + " operator"
			}

			op, rhs := top.op, unhandled

			top.op, unhandled = '&', ""

			if tok == "OR" {
				top.op = '|'
			}

			if err := handleOp(op, rhs); err != "" {
				return nil, err
			}
		default:
			last := unhandled

			unhandled = tok

			if waitWith {
				waitWith = false

				op := top.op

				top.op = 0

				if err := handleOp(op, last+" WITH "+unhandled); err != "" {
					return nil, err
				}

				unhandled = ""

				continue
			}

			if last != "" {
				return nil, "Missing operator between '" + last + "' and '" + unhandled + "' licenses"
			}

			if top.op == 0 && len(top.props) != 0 {
				return nil, "Missing operator before '" + unhandled + "' license"
			}
		}
	}

	if len(tail) != 0 {
		return nil, "No closing bracket for sub expression"
	}

	if err := finalize(); err != "" {
		return nil, err
	}

	return top.props, ""
}

func constraintKind(macro, types string, errs *[]string) constraintType {
	kind := constraintNone

	for _, t := range strings.Fields(types) {
		next := constraintNone

		switch t {
		case "DENY":
			next = constraintDeny
		case "ALLOW_ONLY":
			next = constraintAllowOnly
		default:
			*errs = append(*errs, macro+" macro requires first argument to be restriction type 'DENY' or 'ALLOW_ONLY'. Got '"+t+"' instead")

			continue
		}

		if kind != constraintNone && kind != next {
			*errs = append(*errs, "Multiple "+macro+" macro with different restriction types applied to the same module")

			return constraintNone
		}

		kind = next
	}

	return kind
}

func globConstraint(glob string) *regexp.Regexp {
	var re strings.Builder

	re.WriteByte('^')

	sep := false

	for _, segment := range strings.Split(glob, "/") {
		if segment == "" {
			continue
		}

		if sep {
			re.WriteByte('/')
		}

		sep = true

		if segment == "**" {
			re.WriteString("(.*/)?")

			sep = false

			continue
		}

		for _, c := range segment {
			switch c {
			case '*':
				re.WriteString("[^/]*")
			case '?':
				re.WriteString("[^/]")
			case '.':
				re.WriteString(`\.`)
			default:
				re.WriteRune(c)
			}
		}
	}

	re.WriteByte('$')

	compiled, err := regexp.Compile(re.String())

	if err != nil {
		return regexp.MustCompile(`$^`)
	}

	return compiled
}

func dependentDirConstraints(kind constraintType, restrictions string, errs *[]string) (*dirConstraints, *dirConstraints) {
	constraints := &dirConstraints{kind: kind}
	exceptions := &dirConstraints{kind: constraintAllowOnly}
	scope, glob, except := scopeAll, false, false

	for _, arg := range strings.Fields(restrictions) {
		switch arg {
		case "PEERDIRS":
			scope = scopePeers

			continue
		case "ALL":
			scope = scopeAll

			continue
		case "GLOB":
			glob = true

			continue
		case "EXCEPT":
			except = true

			continue
		}

		asExcept := except

		except = false

		if asExcept && kind == constraintAllowOnly {
			*errs = append(*errs, "[[alt1]]EXCEPT[[rst]] items are not allowed for [[alt1]]ALLOW_ONLY[[rst]] restrictions via [[alt1]]CHECK_DEPENDENT_DIRS[[rst]] macro")

			continue
		}

		dest, itemScope := constraints, scope

		if asExcept {
			dest, itemScope = exceptions, scopeAll
		}

		item := dirConstraint{prefix: arg, scope: itemScope}

		if glob {
			glob = false

			if strings.ContainsAny(arg, "*?") {
				item.glob = globConstraint(arg)
			} else {
				item.literal = strings.Join(strings.FieldsFunc(arg, func(r rune) bool { return r == '/' }), "/")
			}
		}

		dest.items = append(dest.items, item)
	}

	if len(exceptions.items) == 0 {
		exceptions = nil
	}

	return constraints, exceptions
}

func parseFeatureVersions(value, dir string) ([]featureVersion, []string) {
	var features []featureVersion
	var errs []string

	for _, decl := range strings.Fields(value) {
		feature, version, _ := strings.Cut(decl, "=")

		if feature == "" {
			errs = append(errs, "Invalid FEATURE_VERSIONS empty feature name declaration '"+decl+"' in module "+dir)
		}

		if version == "" {
			errs = append(errs, "Invalid FEATURE_VERSIONS empty feature version declaration '"+decl+"' in module "+dir)
		}

		if i := slices.IndexFunc(features, func(f featureVersion) bool { return f.feature == feature }); i >= 0 {
			features[i].version = version
		} else {
			features = append(features, featureVersion{feature, version})
		}
	}

	return features, errs
}

func (ctx *GenCtx) captureTransitive(modulePath string, env Environment, d *ModuleData, platform *Platform) *transitiveInfo {
	licenses := ctx.licenses
	own := licenses.defaultProps

	var info transitiveInfo

	switch {
	case d.license == 0 && d.confModule != nil && platform.conf.requiresNoLicense(d.confModule):
	case d.license == 0:
		for _, prefix := range licenses.fieldsOf(env.str(envEXPLICIT_LICENSE_PREFIXES)) {
			if !pathIsPrefixOf(prefix, modulePath) {
				continue
			}

			if !slices.ContainsFunc(licenses.fieldsOf(env.str(envEXPLICIT_LICENSE_EXCEPTIONS)), func(e string) bool { return pathIsPrefixOf(e, modulePath) }) {
				info.licenseErrors = append(info.licenseErrors, "Explicit license must be specified for modules inside "+prefix+" directory")

				continue
			}

			break
		}
	default:
		if parse := licenses.parse(d.license); parse.err != "" {
			info.licenseErrors = append(info.licenseErrors, "Module LICENSE must be valid SPDX expression: "+parse.err)
		} else {
			own = parse.props
		}
	}

	if mw := licenses.restrictionOf(env, envMODULEWISE_LICENSES_RESTRICTION_TYPES, envMODULEWISE_LICENSES_RESTRICTIONS, "MODULEWISE_LICENSE_RESTRICTION", true); mw.kind != constraintNone || len(mw.errs) != 0 {
		info.modulewiseErrors = mw.errs

		if mw.kind != constraintNone && !mw.r.allows(d.dynamicLink, own) {
			info.modulewise = "Modules licensed with [[alt2]]" + licenseNames(d.license, licenses) + "[[rst]] are forbidden."
		}
	}

	if lr := licenses.restrictionOf(env, envMODULE_LICENSES_RESTRICTION_TYPES, envMODULE_LICENSES_RESTRICTIONS, "LICENSE_RESTRICTION", false); lr.kind != constraintNone || len(lr.errs) != 0 {
		info.errors = append(info.errors, lr.errs...)

		if lr.kind != constraintNone {
			info.restriction, info.licenseConstraint = &lr.r, true
		}
	}

	if info.modulewise != "" || info.licenseConstraint {
		info.exceptions = strings.Fields(env.expand(env.string(envMODULE_LICENSES_RESTRICTION_EXCEPTIONS)))
		info.selfExcepted = slices.Contains(info.exceptions, modulePath)
	}

	if types := env.str(envCHECK_DEPENDENT_DIRS_TYPES); types != strEmpty {
		if kind := constraintKind("CHECK_DEPENDENT_DIRS", env.expand(types.string()), &info.errors); kind != constraintNone {
			info.dependent, info.dependentExcept = dependentDirConstraints(kind, env.expand(env.string(envCHECK_DEPENDENT_DIRS_RESTRICTIONS)), &info.errors)
		}
	}

	if requires := env.str(envREQUIRED_TRANSITIVE_PEERS); requires != strEmpty {
		info.requires = strings.Fields(env.expand(requires.string()))
	}

	if d.features = env.str(envFEATURE_VERSIONS); strHasDollar(d.features.any()) {
		d.features = internStr(env.expand(d.features.string()))
	}

	info.checkFeatures = env.str(envCHECK_FEATURE_VERSION_CONFLICTS) != strEmpty

	if d.goTestFor != 0 && len(d.goTestSrcs) != 0 {
		info.goTestFor = d.goTestFor.string()
	}

	if len(d.provides) != 0 {
		info.provides = anyStrs(d.provides)
	}

	if len(info.errors)+len(info.licenseErrors)+len(info.modulewiseErrors)+len(info.provides) == 0 &&
		info.modulewise == "" && !info.hasRequirements() && info.goTestFor == "" {
		return nil
	}

	out := info

	return &out
}

func licenseNames(license STR, licenses *licenseConf) string {
	if license == 0 {
		return licenses.defaultName
	}

	return license.string()
}

type transitiveChecker struct {
	ctx      *GenCtx
	licenses *licenseConf
	epoch    uint32
}

func passesPeers(r *ModuleEmitResult) bool {
	return !r.isPROGRAM && !r.dynamicLink
}

func (r *ModuleEmitResult) transitiveDir() string {
	return r.testProject.relString()
}

func (r *ModuleEmitResult) transitiveName() string {
	switch {
	case r.ARPath != nil:
		return ymakeVFS(*r.ARPath)
	case r.LDPath != nil:
		return ymakeVFS(*r.LDPath)
	}

	return "$B/" + r.transitiveDir()
}

func (tc *transitiveChecker) report(dir, msg string) {
	tc.ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(dir), Error: msg})
}

func (tc *transitiveChecker) reportAll(dir string, msgs []string) {
	for _, msg := range msgs {
		tc.report(dir, msg)
	}
}

func (tc *transitiveChecker) closure(r *ModuleEmitResult) []*ModuleEmitResult {
	var out []*ModuleEmitResult
	var walk func(*ModuleEmitResult)

	tc.epoch++

	walk = func(m *ModuleEmitResult) {
		for _, p := range m.testPeers {
			if p.reachEpoch == tc.epoch {
				continue
			}

			p.reachEpoch = tc.epoch

			if passesPeers(p) {
				walk(p)
			}

			out = append(out, p)
		}
	}

	walk(r)

	return out
}

func toolClosure(r *ModuleEmitResult) []*ModuleEmitResult {
	seen := map[*ModuleEmitResult]bool{}
	visited := map[*ModuleEmitResult]bool{}

	var out []*ModuleEmitResult
	var walk func(*ModuleEmitResult)

	walk = func(m *ModuleEmitResult) {
		if visited[m] {
			return
		}

		visited[m] = true

		for _, p := range m.testPeers {
			walk(p)
		}

		for _, t := range m.tools {
			walk(t)

			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}

	walk(r)

	return out
}

func dependencyPath(from, to *ModuleEmitResult, tools, indirectOnly bool) string {
	seen := map[*ModuleEmitResult]bool{}

	var stack []*ModuleEmitResult
	var find func(*ModuleEmitResult) bool

	find = func(m *ModuleEmitResult) bool {
		seen[m] = true
		stack = append(stack, m)

		if m == to {
			return true
		}

		next := m.testPeers

		if tools {
			next = slices.Concat(m.testPeers, m.tools)
		}

		for _, n := range next {
			if !seen[n] && find(n) {
				return true
			}
		}

		stack = stack[:len(stack)-1]

		return false
	}

	if !find(from) || len(stack) < 2 || indirectOnly && len(stack) == 2 {
		return ""
	}

	var out strings.Builder

	for i := 1; i < len(stack); i++ {
		out.WriteString("\n    " + stack[i-1].transitiveName() + " -> " + stack[i].transitiveName())
	}

	return out.String()
}

func (tc *transitiveChecker) loadLicense(m *ModuleEmitResult, by *ModuleEmitResult) []licenseProps {
	props := tc.licenses.props(m.license)

	if m.transitiveState&transitiveLicenseLoaded != 0 {
		return props
	}

	m.transitiveState |= transitiveLicenseLoaded

	if info := m.transitive; info != nil {
		tc.reportAll(m.transitiveDir(), info.licenseErrors)
		tc.reportAll(by.transitiveDir(), info.modulewiseErrors)

		excepted := info.selfExcepted

		if by != m {
			excepted = by.transitive != nil && slices.Contains(by.transitive.exceptions, m.transitiveDir())
		}

		if info.modulewise != "" && !excepted {
			tc.report(by.transitiveDir(), info.modulewise)
		}
	}

	return props
}

func (tc *transitiveChecker) loadFeatures(m *ModuleEmitResult, by *ModuleEmitResult) []featureVersion {
	if m.features == strEmpty {
		return nil
	}

	features, errs := parseFeatureVersions(m.features.string(), m.transitiveDir())

	if m.transitiveState&transitiveFeaturesLoaded == 0 {
		m.transitiveState |= transitiveFeaturesLoaded
		tc.reportAll(by.transitiveDir(), errs)
	}

	return features
}

func (tc *transitiveChecker) check(m *ModuleEmitResult) {
	if m.transitiveState&transitiveChecked != 0 {
		return
	}

	m.transitiveState |= transitiveChecked

	dir := m.transitiveDir()

	tc.loadLicense(m, m)

	info := m.transitive

	var features []featureVersion

	if info != nil {
		tc.reportAll(dir, info.errors)

		if info.checkFeatures {
			if features = tc.loadFeatures(m, m); len(features) == 0 {
				tc.report(dir, "Usage CHECK_FEATURE_VERSION_CONFLICTS without FEATURE_VERSION")
			}
		}
	}

	requirements := info != nil && (info.licenseConstraint || info.dependent != nil || len(info.requires) != 0 || len(features) != 0)

	if !requirements {
		if m.checkProvides && m.provided != nil && m.provided.conflict {
			tc.checkProvides(m, tc.closure(m))
		}

		return
	}

	peers := tc.closure(m)

	if info.licenseConstraint {
		for _, p := range peers {
			if slices.Contains(info.exceptions, p.transitiveDir()) {
				continue
			}

			if !info.restriction.allows(p.dynamicLink, tc.loadLicense(p, m)) {
				tc.report(dir, "Forbids direct or indirect PEERDIR dependency on code with any license used by module [[imp]]"+p.transitiveName()+"[[rst]] ([[alt2]]"+licenseNames(p.license, tc.licenses)+"[[rst]]) via [[alt1]]LICENSE_RESTRICTION[[rst]] restrictions"+dependencyPath(m, p, false, true))
			}
		}
	}

	if c := info.dependent; c != nil {
		tc.checkDependentDirs(m, peers, c, info.dependentExcept)
	}

	if len(info.requires) != 0 {
		var unmet []string

		for _, req := range info.requires {
			if present, isDir := tc.ctx.fs.exists(srcRootRel, req); !present || !isDir {
				tc.report(dir, "[[alt1]]REQUIRES[[rst]] to non-directory [[imp]]$S/"+req+"[[rst]]")
			} else if !slices.Contains(unmet, req) {
				unmet = append(unmet, req)
			}
		}

		for _, p := range peers {
			unmet = slices.DeleteFunc(unmet, func(req string) bool { return req == p.transitiveDir() })
		}

		for _, req := range unmet {
			tc.report(dir, "missing indirect PEERDIR dependency to directory [[imp]]$S/"+req+"[[rst]] required by [[alt1]]REQUIRES[[rst]]")
		}
	}

	if len(features) != 0 {
		for _, p := range peers {
			theirs := tc.loadFeatures(p, m)

			for _, own := range features {
				if i := slices.IndexFunc(theirs, func(f featureVersion) bool { return f.feature == own.feature }); i >= 0 && theirs[i].version != own.version {
					tc.report(dir, fmt.Sprintf("Invalid feature %s version %s != %s in peer %s", own.feature, theirs[i].version, own.version, p.transitiveDir()))
				}
			}
		}
	}

	tc.checkProvides(m, peers)
}

func (tc *transitiveChecker) checkDependentDirs(m *ModuleEmitResult, peers []*ModuleEmitResult, c, except *dirConstraints) {
	dir := m.transitiveDir()

	allowed := func(d string, scope uint8) bool {
		return c.allowed(d, scope) || except != nil && except.allowed(d, scope)
	}

	if c.restricts(scopePeers) {
		for _, p := range peers {
			if p.transitiveDir() == dir {
				continue
			}

			if !allowed(p.transitiveDir(), scopePeers) {
				tc.report(dir, "forbids direct or indirect PEERDIR dependency to module [[imp]]"+p.transitiveName()+"[[rst]] via [[alt1]]CHECK_DEPENDENT_DIRS[[rst]]"+dependencyPath(m, p, false, true))
			}
		}
	}

	if !c.restricts(scopeTools) {
		return
	}

	seen := map[*ModuleEmitResult]bool{}

	for _, tool := range toolClosure(m) {
		if !allowed(tool.transitiveDir(), scopeTools) {
			tc.report(dir, "forbids direct or indirect dependency to tool [[imp]]"+tool.transitiveName()+"[[rst]] via [[alt1]]CHECK_DEPENDENT_DIRS[[rst]]"+dependencyPath(m, tool, true, false))
		}

		for _, p := range tc.closure(tool) {
			if seen[p] {
				continue
			}

			seen[p] = true

			if !allowed(p.transitiveDir(), scopeTools) {
				tc.report(dir, "forbids direct or indirect dependency to module [[imp]]"+p.transitiveName()+"[[rst]] via [[alt1]]CHECK_DEPENDENT_DIRS[[rst]]"+dependencyPath(m, tool, true, false)+dependencyPath(tool, p, false, false))
			}
		}
	}
}

func (tc *transitiveChecker) checkProvides(m *ModuleEmitResult, peers []*ModuleEmitResult) {
	if m.provided == nil || !m.provided.conflict {
		return
	}

	dir := m.transitiveDir()
	provided := map[string]*ModuleEmitResult{}

	register := func(p *ModuleEmitResult) {
		if p.transitive == nil {
			return
		}

		for _, feature := range p.transitive.provides {
			first, ok := provided[feature]

			if !ok {
				provided[feature] = p

				continue
			}

			if first != m {
				tc.report(dir, "depends on two modules which [[alt1]]PROVIDES[[rst]] same feature '"+feature+"':\n[[imp]]"+p.transitiveName()+"[[rst]]"+dependencyPath(m, p, false, false)+"\n[[imp]]"+first.transitiveName()+"[[rst]]"+dependencyPath(m, first, false, false))
			} else {
				tc.report(dir, "[[alt1]]PROVIDES[[rst]] feature '"+feature+"' and depends on module [[imp]]"+p.transitiveName()+"[[rst]] providing same feature:"+dependencyPath(m, p, false, false))
			}
		}
	}

	register(m)

	for _, p := range peers {
		register(p)
	}
}

func (tc *transitiveChecker) checkGoTest(m *ModuleEmitResult) {
	if m.transitive == nil || m.transitive.goTestFor == "" {
		return
	}

	tested := source(m.transitive.goTestFor)

	isTarget := func(p *ModuleEmitResult) bool {
		return p.testProject == tested && p != m && isGoModuleType(p.ModuleStmtName)
	}

	var target *ModuleEmitResult
	var reach func(*ModuleEmitResult) bool

	tc.epoch++

	reach = func(x *ModuleEmitResult) bool {
		if x.reachEpoch == tc.epoch {
			return x.transitiveState&transitiveReach != 0
		}

		x.reachEpoch = tc.epoch
		x.transitiveState &^= transitiveReach

		for _, q := range x.testPeers {
			if isTarget(q) || passesPeers(q) && reach(q) {
				if isTarget(q) {
					target = q
				}

				x.transitiveState |= transitiveReach

				return true
			}
		}

		return false
	}

	for _, peer := range m.testPeers {
		if !isTarget(peer) && reach(peer) {
			tc.report(m.transitiveDir(), "go test issue: transitive dependencies on modules under test are prohibited in Arcadia due to scalability issues\nSee https://github.com/golang/go/issues/29258 and st/YMAKE-102 for more details\n"+dependencyPath(peer, target, false, false))
		}
	}
}

func (ctx *GenCtx) reportDefaultLicense() {
	var dirs []string

	if len(ctx.startModules) != 0 {
		dirs = append(dirs, ctx.startModules[0].transitiveDir())
	}

	if ctx.firstTool != nil {
		dirs = append(dirs, ctx.firstTool.transitiveDir())
	}

	if ctx.testMode {
		dirs = append(dirs, "build/platform/test_tool")
	}

	for _, dir := range dirs {
		for _, msg := range ctx.licenses.runErrors {
			ctx.onWarn(Warn{Kind: WarnMisconfiguration, Where: makeFileWhere(dir), Error: msg})
		}
	}
}

func (ctx *GenCtx) checkTransitiveRequirements() {
	if len(ctx.licenses.runErrors) != 0 {
		ctx.reportDefaultLicense()
	}

	tc := &transitiveChecker{ctx: ctx, licenses: ctx.licenses}

	var walk func(*ModuleEmitResult)

	walk = func(m *ModuleEmitResult) {
		for _, p := range m.testPeers {
			if p.transitiveState&(transitiveChecked|transitiveNeeded) != transitiveNeeded {
				continue
			}

			if passesPeers(p) {
				walk(p)
			}

			tc.check(p)
		}
	}

	for _, start := range ctx.startModules {
		tc.check(start)
		tc.checkGoTest(start)
		walk(start)
	}
}

func (ctx *GenCtx) recordTool(res *ModuleEmitResult) {
	if ctx.firstTool == nil {
		ctx.firstTool = res
	}

	if ctx.frameDepth == 0 {
		return
	}

	if f := ctx.frames[ctx.frameDepth-1]; !slices.Contains(f.tools, res) {
		f.tools = append(f.tools, res)
	}
}

type featureProvider struct {
	feature string
	module  *ModuleEmitResult
}

type providedSummary struct {
	providers []featureProvider
	conflict  bool
}

func (s *providedSummary) add(feature string, m *ModuleEmitResult) {
	for _, p := range s.providers {
		if p.feature == feature {
			s.conflict = s.conflict || p.module != m

			return
		}
	}

	s.providers = append(s.providers, featureProvider{feature, m})
}

func ownProvided(r *ModuleEmitResult) *providedSummary {
	if r.transitive == nil || len(r.transitive.provides) == 0 {
		return nil
	}

	s := &providedSummary{}

	for i, feature := range r.transitive.provides {
		s.add(feature, r)
		s.conflict = s.conflict || slices.Contains(r.transitive.provides[:i], feature)
	}

	return s
}

func (s *providedSummary) covers(other *providedSummary) bool {
	return (s.conflict || !other.conflict) && !slices.ContainsFunc(other.providers, func(p featureProvider) bool { return !slices.Contains(s.providers, p) })
}

func mergeProvided(a, b *providedSummary) *providedSummary {
	switch {
	case b == nil || a == b:
		return a
	case a == nil:
		return b
	case a.covers(b):
		return a
	case b.covers(a):
		return b
	}

	out := &providedSummary{providers: slices.Clone(a.providers), conflict: a.conflict || b.conflict}

	for _, p := range b.providers {
		out.add(p.feature, p.module)
	}

	return out
}

func (r *ModuleEmitResult) recordTransitive(ctx *GenCtx, d *ModuleData, tools []*ModuleEmitResult, platform *Platform) {
	r.license, r.features, r.transitive = d.license, d.features, d.transitive

	if ctx.blacklist != nil {
		if rule := ctx.blacklist.match(r.testProject.relString()); rule != "" {
			r.blacklistRule = internStr(rule)
		}
	}

	if ctx.isolated != nil {
		r.inIsolated = ctx.isolated.contains(r.testProject.relString())

		if platform != ctx.host {
			ctx.checkModuleIsolation(r, d)
		}
	}

	r.tools = ctx.appendPeerList(r.tools, tools)
	r.checkProvides = d.confModule != nil && platform.conf.checksProvides(d.confModule)
	r.provided = ownProvided(r)

	needed := false

	for _, p := range r.testPeers {
		if passesPeers(p) {
			r.provided = mergeProvided(r.provided, p.provided)
		} else {
			r.provided = mergeProvided(r.provided, ownProvided(p))
		}

		needed = needed || p.transitiveState&transitiveNeeded != 0
	}

	if r.transitive != nil || r.checkProvides && r.provided != nil && r.provided.conflict || passesPeers(r) && needed {
		r.transitiveState |= transitiveNeeded
	}
}

func (ctx *GenCtx) appendPeerList(to, from []*ModuleEmitResult) []*ModuleEmitResult {
	switch {
	case len(from) == 0:
		return to
	case len(to) == 0:
		return ctx.peerLists.list(from...)
	}

	return append(to, from...)
}

func licenseExpression(args []ANY) STR {
	if len(args) == 1 {
		return args[0].str()
	}

	return internStr(strings.Join(anyStrs(args), " "))
}
