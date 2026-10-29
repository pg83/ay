package main

import (
	"maps"
	"slices"
	"strings"
)

type EnvKind uint8

type envAppend struct {
	id  ENV
	buf []byte
}

type EnvStore struct {
	val              []STR
	kind             []EnvKind
	appends          []envAppend
	pending          BitSet
	textInputs       map[ENV][]SourceTextRange
	resourceBindings []EnvResourceBinding
	included         map[string]bool
	confBase         *EnvStore
	confWrites       []ENV
}

type Environment struct {
	s            *EnvStore
	resourceUses *[]STR
}

func newEnvironment() Environment {
	return Environment{s: &EnvStore{}}
}

func (s *EnvStore) ensure(e ENV) {
	if int(e) < len(s.kind) {
		return
	}

	n := max(int(e)+1, int(envTable.count))

	if cap(s.kind) >= n && cap(s.val) >= n {
		old := len(s.kind)

		s.kind = s.kind[:n]
		s.val = s.val[:n]

		clear(s.kind[old:])
		clear(s.val[old:])

		return
	}

	n = max(n, 2*len(s.kind))

	nk := make([]EnvKind, n)

	copy(nk, s.kind)
	s.kind = nk

	nv := make([]STR, n)

	copy(nv, s.val)
	s.val = nv
}

func (s *EnvStore) lookup(e ENV) (EnvKind, STR) {
	if len(s.appends) != 0 && s.pending.has(uint32(e)) {
		s.settle(e)
	}

	if int(e) < len(s.kind) {
		return s.kind[e], s.val[e]
	}

	return envAbsent, 0
}

func (s *EnvStore) settle(e ENV) {
	for i := range s.appends {
		if s.appends[i].id == e {
			value := string(s.appends[i].buf)

			s.appends = slices.Delete(s.appends, i, i+1)
			s.pending.remove(uint32(e))
			Environment{s: s}.setString(e, value)

			return
		}
	}
}

func (s *EnvStore) settleAll() {
	for len(s.appends) != 0 {
		s.settle(s.appends[0].id)
	}
}

func (s *EnvStore) dropAppend(e ENV) {
	if !s.pending.has(uint32(e)) {
		return
	}

	for i := range s.appends {
		if s.appends[i].id == e {
			s.appends = slices.Delete(s.appends, i, i+1)
			s.pending.remove(uint32(e))

			return
		}
	}
}

func (e Environment) appendString(id ENV, v string) {
	s := e.s

	for i := range s.appends {
		if s.appends[i].id == id {
			s.appends[i].buf = append(append(s.appends[i].buf, ' '), v...)

			return
		}
	}

	prev := ""

	if int(id) < len(s.kind) {
		switch s.kind[id] {
		case envVFS:
			prev = VFS(uint32(s.val[id])).string()
		case envStr, envInt:
			prev = s.val[id].string()
		}
	}

	if prev == "" {
		e.setFromString(id, v)

		return
	}

	buf := make([]byte, 0, 2*(len(prev)+len(v)+1))

	buf = append(append(append(buf, prev...), ' '), v...)
	s.appends = append(s.appends, envAppend{id: id, buf: buf})
	s.pending.add(uint32(id))
}

func (e Environment) bool(id ENV) bool {
	switch k, v := e.s.lookup(id); k {
	case envStr, envInt:
		return strIsTruthy(v)
	case envVFS:
		return true
	}

	return false
}

func strIsTruthy(v STR) bool {
	switch v {
	case strYes:
		return true
	case strNo, 0:
		return false
	}

	return stringIsTruthy(v.string())
}

func (e Environment) string(id ENV) string {
	if k, v := e.s.lookup(id); k != envAbsent {
		e.recordResourceUse(id)

		if k == envVFS {
			return VFS(uint32(v)).string()
		}

		return v.string()
	}

	return ""
}

func (e Environment) clone() Environment {
	e.s.settleAll()

	return Environment{s: &EnvStore{
		val:              append([]STR(nil), e.s.val...),
		kind:             append([]EnvKind(nil), e.s.kind...),
		textInputs:       maps.Clone(e.s.textInputs),
		resourceBindings: slices.Clone(e.s.resourceBindings),
		included:         maps.Clone(e.s.included),
	}}
}

func (e Environment) cloneInto(dst *EnvStore) Environment {
	e.s.settleAll()
	dst.appends = dst.appends[:0]

	if len(e.s.kind) > 0 {
		dst.ensure(ENV(len(e.s.kind) - 1))
	}

	clear(dst.val[copy(dst.val, e.s.val):])
	clear(dst.kind[copy(dst.kind, e.s.kind):])
	dst.textInputs = maps.Clone(e.s.textInputs)
	dst.resourceBindings = append(dst.resourceBindings[:0], e.s.resourceBindings...)
	dst.included = maps.Clone(e.s.included)
	dst.confBase = nil
	dst.confWrites = dst.confWrites[:0]

	if e.s.confBase == e.s {
		dst.confBase = e.s
	}

	return Environment{s: dst}
}

func (s *EnvStore) confWrite(id ENV) {
	if s.confBase == s {
		s.confBase = nil
	} else if s.confBase != nil {
		s.confWrites = append(s.confWrites, id)
	}
}

func (e Environment) setVFS(id ENV, v VFS) {
	if len(e.s.appends) != 0 {
		e.s.dropAppend(id)
	}

	delete(e.s.textInputs, id)

	e.s.clearResourceBinding(id)

	e.s.confWrite(id)
	e.s.ensure(id)
	e.s.kind[id] = envVFS
	e.s.val[id] = STR(uint32(v))
}

func (e Environment) setStrID(id ENV, v STR) {
	if len(e.s.appends) != 0 {
		e.s.dropAppend(id)
	}

	delete(e.s.textInputs, id)

	e.s.clearResourceBinding(id)

	e.s.confWrite(id)
	e.s.ensure(id)
	e.s.kind[id] = envStr
	e.s.val[id] = v
}

func (e Environment) setStringID(id ENV, v STR) {
	e.setStrID(id, v)
}

func (e Environment) setString(id ENV, v string) {
	if vfsHasPrefix(v) {
		e.setVFS(id, intern(v))

		return
	}

	e.setStrID(id, internStr(v))
}

func (e Environment) setInt(id ENV, n int) {
	if len(e.s.appends) != 0 {
		e.s.dropAppend(id)
	}

	delete(e.s.textInputs, id)

	e.s.clearResourceBinding(id)

	e.s.confWrite(id)
	e.s.ensure(id)
	e.s.kind[id] = envInt
	e.s.val[id] = intSTR[n]
}

func (e Environment) setBool(id ENV, v bool) {
	if v {
		e.setStrID(id, strYes)
	} else {
		e.setStrID(id, strNo)
	}
}

func (e Environment) setFromString(id ENV, v string) {
	switch v {
	case "yes":
		e.setBool(id, true)
	case "no":
		e.setBool(id, false)
	default:
		e.setString(id, v)
	}
}

func (e Environment) setFromStringID(id ENV, v STR) {
	e.setStrID(id, v)
}

func (e Environment) unbind(id ENV) EnvKind {
	if len(e.s.appends) != 0 {
		e.s.dropAppend(id)
	}

	k, _ := e.s.lookup(id)

	if k != envAbsent {
		e.s.confWrite(id)
		e.s.kind[id] = envAbsent
	}

	return k
}

func (e Environment) rebind(id ENV, k EnvKind) {
	if k != envAbsent {
		e.s.confWrite(id)
		e.s.kind[id] = k
	}
}

func (e Environment) hasBindingID(id ENV) bool {
	k, _ := e.s.lookup(id)

	return k != envAbsent
}

func (e Environment) lookup(name string) (string, bool) {
	id := internedEnv(name)

	if id == 0 {
		return "", false
	}

	k, v := e.s.lookup(id)

	if k == envAbsent {
		return "", false
	}

	e.recordResourceUse(id)

	if k == envVFS {
		return VFS(uint32(v)).string(), true
	}

	return v.string(), true
}

func (e Environment) setDefaultString(id ENV, v string) {
	if e.hasBindingID(id) {
		return
	}

	e.setString(id, v)
}

func (env Environment) expand(value string) string {
	if !strings.Contains(value, "$") {
		return value
	}

	for iteration := 1; value != ""; iteration++ {
		var out strings.Builder

		from := 0

		for {
			start, end, name := nextConfReference(value, from, false)

			if start == end {
				break
			}

			out.WriteString(value[from:start])
			out.WriteString(env.string(internedEnv(name)))
			from = end
		}

		expanded := value

		if from != 0 {
			out.WriteString(value[from:])
			expanded = out.String()
		}

		if iteration >= 128 {
			throwFmt("conf: scalar substitution exceeded the limit 128")
		}

		if expanded == value {
			return value
		}

		value = expanded
	}

	return value
}

func nextConfReference(text string, from int, modifiers bool) (start, end int, name string) {
	for from < len(text) {
		dollar := strings.IndexByte(text[from:], '$')

		if dollar < 0 {
			break
		}

		start = from + dollar
		from = start + 1

		first := from
		braced := first < len(text) && text[first] == '{'

		if braced {
			first++

			if modifiers {
				for i := first; i < len(text) && text[i] != '}'; i++ {
					if text[i] == ':' {
						first = i + 1

						break
					}
				}
			}
		}

		end = confNameEnd(text, first)

		if end == first {
			continue
		}

		if !braced {
			return start, end, text[first:end]
		}

		if end < len(text) && text[end] == '}' {
			return start, end + 1, text[first:end]
		}
	}

	return len(text), len(text), ""
}
