package main

import (
	"maps"
	"slices"
)

var emptyAppendArgs = []ANY{strEmpty.any()}

type EnvKind uint8

type envAppend struct {
	id    ENV
	parts []ANY
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
	joined           []byte
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

	n = max(n, len(s.kind)+len(s.kind)/8+64)

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
			buf := s.joined[:0]

			for k, part := range s.appends[i].parts {
				if k > 0 {
					buf = append(buf, ' ')
				}

				buf = append(buf, part.string()...)
			}

			s.joined = buf[:0]
			s.appends = slices.Delete(s.appends, i, i+1)
			s.pending.remove(uint32(e))

			ranges := s.textInputs[e]

			if vfsHasPrefix(string(buf)) {
				Environment{s: s}.setVFS(e, intern(string(buf)))
			} else {
				Environment{s: s}.setStrID(e, internBytes(buf))
			}

			if ranges != nil {
				s.textInputs[e] = ranges
			}

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

func (e Environment) appendANYs(id ENV, args []ANY) {
	if len(args) == 0 {
		args = emptyAppendArgs
	}

	s := e.s

	for i := range s.appends {
		if s.appends[i].id == id {
			s.appends[i].parts = append(s.appends[i].parts, args...)

			return
		}
	}

	var prev ANY

	if int(id) < len(s.kind) {
		switch s.kind[id] {
		case envVFS:
			prev = VFS(uint32(s.val[id])).any()
		case envStr, envInt:
			prev = s.val[id].any()
		}
	}

	if prev == 0 || prev == strEmpty.any() {
		if len(args) == 1 {
			e.setFromANY(id, args[0])
		} else {
			e.setFromSTR(id, joinAnysSTR(args, " "))
		}

		return
	}

	s.appends = append(s.appends, envAppend{id: id, parts: append(append(make([]ANY, 0, max(8, len(args)+1)), prev), args...)})
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

func (e Environment) stringLen(id ENV) int {
	s := e.s

	if len(s.appends) != 0 && s.pending.has(uint32(id)) {
		for i := range s.appends {
			if s.appends[i].id == id {
				n := -1

				for _, part := range s.appends[i].parts {
					n += 1 + len(part.string())
				}

				return n
			}
		}
	}

	if int(id) >= len(s.kind) {
		return 0
	}

	switch s.kind[id] {
	case envVFS:
		return VFS(uint32(s.val[id])).stringLen()
	case envAbsent:
		return 0
	}

	return len(s.val[id].string())
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

func (e Environment) stringID(id ENV) STR {
	if k, v := e.s.lookup(id); k == envStr || k == envInt {
		e.recordResourceUse(id)

		return v
	}

	return 0
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
	dst.appends = dst.appends[:0]

	for _, a := range e.s.appends {
		dst.appends = append(dst.appends, envAppend{id: a.id, parts: slices.Clone(a.parts)})
	}

	dst.pending.copyFrom(&e.s.pending)

	if n := max(len(e.s.kind), int(envTable.count)); n > 0 {
		dst.ensure(ENV(n - 1))
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
	if s.confBase != nil {
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

func (e Environment) setFromSTR(id ENV, v STR) {
	if strHasDollar(v.any()) {
		if s := v.string(); vfsHasPrefix(s) {
			e.setVFS(id, intern(s))

			return
		}
	}

	e.setStrID(id, v)
}

func (e Environment) setFromANY(id ENV, v ANY) {
	if f := v.vfs(); f != 0 {
		e.setVFS(id, f)

		return
	}

	e.setFromSTR(id, v.str())
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

func (e Environment) lookupValue(name string) (string, VFS, bool) {
	id := internedEnv(name)

	if id == 0 {
		return "", 0, false
	}

	k, v := e.s.lookup(id)

	if k == envAbsent {
		return "", 0, false
	}

	e.recordResourceUse(id)

	if k == envVFS {
		return "", VFS(uint32(v)), true
	}

	return v.string(), 0, true
}

func (e Environment) setDefaultString(id ENV, v string) {
	if e.hasBindingID(id) {
		return
	}

	e.setString(id, v)
}
