package main

var expansionMemo = ExpansionMemo{heads: newIntMap[uint32](1 << 14)}

const (
	expansionMemoDepth = 8
	expansionVolatile  = 1 << 31
)

const (
	expansionVariable = iota + 1
	expansionScalar
	expansionTokens
	expansionRunProgram
	expansionCompilerFlags
	expansionRunProgramPrepare
)

type ExpansionStep struct {
	id   ENV
	kind EnvKind
	val  STR
}

type ExpansionEntry struct {
	next   uint32
	steps  uint32
	nsteps uint32
	words  uint32
	nwords uint32
}

type ExpansionMemo struct {
	heads    *IntMap[uint32]
	entries  []ExpansionEntry
	steps    []ExpansionStep
	words    []ANY
	trace    []ExpansionStep
	tracing  bool
	poisoned bool
}

func expansionKey(id uint32, tag uint32) uint64 {
	return splitMix64(id, tag)
}

func (m *ExpansionMemo) find(key uint64, env Environment) ([]ANY, bool) {
	m.tracing = false

	head, seen := m.heads.cell(key)

	if !seen || *head == expansionVolatile {
		return nil, false
	}

	for i := *head; i != 0; {
		e := &m.entries[i-1]

		if m.valid(e, env) {
			for _, st := range m.steps[e.steps : e.steps+e.nsteps] {
				if st.kind != envAbsent {
					env.recordResourceUse(st.id)
				}
			}

			return m.words[e.words : e.words+e.nwords : e.words+e.nwords], true
		}

		i = e.next
	}

	m.tracing, m.poisoned, m.trace = true, false, m.trace[:0]

	return nil, false
}

func (m *ExpansionMemo) valid(e *ExpansionEntry, env Environment) bool {
	for _, st := range m.steps[e.steps : e.steps+e.nsteps] {
		k, v := env.s.lookup(st.id)

		if k == envAbsent {
			v = 0
		}

		if k != st.kind || v != st.val {
			return false
		}
	}

	return true
}

func (m *ExpansionMemo) note(env Environment, id ENV) {
	if id == 0 {
		m.poisoned = true

		return
	}

	k, v := env.s.lookup(id)

	if k == envAbsent {
		v = 0
	}

	m.trace = append(m.trace, ExpansionStep{id: id, kind: k, val: v})
}

func (m *ExpansionMemo) cancel() {
	m.tracing = false
}

func (m *ExpansionMemo) end(key uint64, words ...ANY) {
	if !m.tracing {
		return
	}

	m.tracing = false

	if m.poisoned {
		return
	}

	head := m.heads.get(key)
	depth := 0

	for i := *head; i != 0; i = m.entries[i-1].next {
		depth++
	}

	if depth >= expansionMemoDepth {
		*head = expansionVolatile

		return
	}

	m.entries = append(m.entries, ExpansionEntry{next: *head, steps: uint32(len(m.steps)), nsteps: uint32(len(m.trace)), words: uint32(len(m.words)), nwords: uint32(len(words))})
	m.steps = append(m.steps, m.trace...)
	m.words = append(m.words, words...)

	*head = uint32(len(m.entries))
}
