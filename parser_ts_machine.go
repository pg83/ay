package main

import (
	"math/bits"
	"sync"
	"unsafe"
)

var tsMachines = sync.OnceValue(func() tsMachineSet {
	return tsMachineSet{
		imports:  newTSMachine(func(b tsBuilder) tsFrag { return b.alt(b.decl(), b.call()) }),
		exports:  newTSMachine(func(b tsBuilder) tsFrag { return b.alt(b.seq(b.sps(), b.lit("=")), b.decl()) }),
		requires: newTSMachine(func(b tsBuilder) tsFrag { return b.call() }),
	}
})

const (
	tsDFAUnknown int32 = -1
	tsDFADead    int32 = -2
)

type tsMachineSet struct{ imports, exports, requires *tsMachine }

type tsMachine struct {
	nodes    []tsNode
	start    int
	classes  []*[256]bool
	closures [][]uint64

	states [][]uint64
	index  map[string]int32
	trans  []*[256]int32
}

type tsNode struct {
	eps   []int
	match func(byte) bool
	next  int
}

type tsFrag struct{ start, end int }

type tsBuilder struct{ m *tsMachine }

func (b tsBuilder) node() int {
	b.m.nodes = append(b.m.nodes, tsNode{next: -1})

	return len(b.m.nodes) - 1
}

func (b tsBuilder) link(from, to int) {
	b.m.nodes[from].eps = append(b.m.nodes[from].eps, to)
}

func (b tsBuilder) class(match func(byte) bool) tsFrag {
	s, e := b.node(), b.node()

	b.m.nodes[s].match, b.m.nodes[s].next = match, e

	return tsFrag{s, e}
}

func (b tsBuilder) lit(text string) tsFrag {
	frags := make([]tsFrag, 0, len(text))

	for i := range len(text) {
		c := text[i]

		frags = append(frags, b.class(func(x byte) bool { return x == c }))
	}

	return b.seq(frags...)
}

func (b tsBuilder) seq(frags ...tsFrag) tsFrag {
	if len(frags) == 0 {
		n := b.node()

		return tsFrag{n, n}
	}

	for i := 1; i < len(frags); i++ {
		b.link(frags[i-1].end, frags[i].start)
	}

	return tsFrag{frags[0].start, frags[len(frags)-1].end}
}

func (b tsBuilder) alt(frags ...tsFrag) tsFrag {
	s, e := b.node(), b.node()

	for _, f := range frags {
		b.link(s, f.start)
		b.link(f.end, e)
	}

	return tsFrag{s, e}
}

func (b tsBuilder) star(f tsFrag) tsFrag {
	s, e := b.node(), b.node()

	b.link(s, f.start)
	b.link(s, e)
	b.link(f.end, f.start)
	b.link(f.end, e)

	return tsFrag{s, e}
}

func (b tsBuilder) plus(build func() tsFrag) tsFrag {
	return b.seq(build(), b.star(build()))
}

func (b tsBuilder) opt(f tsFrag) tsFrag {
	return b.alt(f, b.seq())
}

func tsIdentStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '$' || c == '_'
}

func tsIdentChar(c byte) bool {
	return tsIdentStart(c) || c >= '0' && c <= '9'
}

func (b tsBuilder) sp() tsFrag {
	space := b.class(func(c byte) bool { return c == ' ' || c >= '\t' && c <= '\r' })
	notStar := func() tsFrag { return b.class(func(c byte) bool { return c != '*' }) }
	stars := func() tsFrag { return b.lit("*") }
	body := b.star(b.alt(notStar(), b.seq(b.plus(stars), b.class(func(c byte) bool { return c != '*' && c != '/' }))))
	comment := b.seq(b.lit("/*"), body, b.plus(stars), b.lit("/"))

	return b.alt(space, comment)
}

func (b tsBuilder) sps() tsFrag {
	return b.star(b.sp())
}

func (b tsBuilder) sp1() tsFrag {
	return b.plus(b.sp)
}

func (b tsBuilder) ident() tsFrag {
	return b.seq(b.class(tsIdentStart), b.star(b.class(tsIdentChar)))
}

func (b tsBuilder) specifier() tsFrag {
	quoted := func(q byte) tsFrag {
		return b.seq(b.class(func(c byte) bool { return c == q }), b.star(b.class(func(c byte) bool { return c != q })), b.class(func(c byte) bool { return c == q }))
	}

	return b.alt(quoted('\''), quoted('"'))
}

func (b tsBuilder) call() tsFrag {
	return b.seq(b.sps(), b.lit("("), b.sps(), b.specifier(), b.sps(), b.lit(")"))
}

func (b tsBuilder) clause() tsFrag {
	alias := func() tsFrag { return b.seq(b.sp1(), b.lit("as"), b.sp1(), b.ident()) }
	sep := func() tsFrag { return b.seq(b.sps(), b.lit(","), b.sps()) }
	spec := func() tsFrag { return b.alt(b.ident(), b.seq(b.ident(), alias())) }
	list := func() tsFrag { return b.seq(spec(), b.star(b.seq(sep(), spec())), b.opt(sep())) }

	named := func() tsFrag {
		return b.alt(b.seq(b.lit("{"), b.sps(), b.lit("}")), b.seq(b.lit("{"), b.sps(), list(), b.sps(), b.lit("}")))
	}

	namespace := func() tsFrag { return b.seq(b.lit("*"), b.opt(alias())) }

	return b.alt(b.ident(), namespace(), named(), b.seq(b.ident(), sep(), namespace()), b.seq(b.ident(), sep(), named()))
}

func (b tsBuilder) decl() tsFrag {
	full := b.seq(b.opt(b.seq(b.lit("type"), b.sp1())), b.clause(), b.sp1(), b.lit("from"), b.sp1(), b.specifier())

	return b.seq(b.sp1(), b.alt(full, b.specifier()))
}

func newTSMachine(build func(b tsBuilder) tsFrag) *tsMachine {
	m := &tsMachine{index: map[string]int32{}}
	f := build(tsBuilder{m})
	words := (len(m.nodes) + 63) / 64

	m.start = f.start
	m.classes = make([]*[256]bool, len(m.nodes))
	m.closures = make([][]uint64, len(m.nodes))

	for n, node := range m.nodes {
		if node.match != nil {
			class := new([256]bool)

			for c := range 256 {
				class[c] = node.match(byte(c))
			}

			m.classes[n] = class
		}

		m.closures[n] = make([]uint64, words)
		m.close(m.closures[n], n)
	}

	m.state(m.closures[m.start])

	return m
}

func (m *tsMachine) close(set []uint64, n int) {
	if set[n/64]&(1<<(n%64)) != 0 {
		return
	}

	set[n/64] |= 1 << (n % 64)

	for _, e := range m.nodes[n].eps {
		m.close(set, e)
	}
}

func (m *tsMachine) state(set []uint64) int32 {
	key := unsafe.String((*byte)(unsafe.Pointer(&set[0])), len(set)*8)

	if id, ok := m.index[key]; ok {
		return id
	}

	id := int32(len(m.states))
	trans := new([256]int32)

	for c := range trans {
		trans[c] = tsDFAUnknown
	}

	m.states = append(m.states, set)
	m.trans = append(m.trans, trans)
	m.index[key] = id

	return id
}

func (m *tsMachine) step(state int32, c byte) int32 {
	next := make([]uint64, len(m.states[state]))
	live := false

	for w, word := range m.states[state] {
		for word != 0 {
			n := w*64 + bits.TrailingZeros64(word)

			word &= word - 1

			if class := m.classes[n]; class != nil && class[c] {
				for i, bit := range m.closures[m.nodes[n].next] {
					next[i] |= bit
				}

				live = true
			}
		}
	}

	to := tsDFADead

	if live {
		to = m.state(next)
	}

	m.trans[state][c] = to

	return to
}

func tsFailPos(s string, pos int, keyword string) int {
	machines := tsMachines()
	m := machines.requires

	switch keyword {
	case "import":
		m = machines.imports
	case "export":
		m = machines.exports
	}

	state := int32(0)

	for p := pos; p < len(s); p++ {
		next := m.trans[state][s[p]]

		if next == tsDFAUnknown {
			next = m.step(state, s[p])
		}

		if next == tsDFADead {
			return p
		}

		state = next
	}

	return len(s)
}
