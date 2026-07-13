package main

type Darts struct {
	base     []int32
	check    []int32
	value    []int32
	baseHint int32
}

type DartsNode struct {
	first int32
	key   int32
}

type DartsEdge struct {
	next  int32
	child int32
	code  byte
}

func newDarts(keys []string) *Darts {
	nodes := make([]DartsNode, 1, 64)
	edges := make([]DartsEdge, 1, 64)

	for i, k := range keys {
		n := int32(0)

		for j := 0; j < len(k); j++ {
			code := k[j]
			c := int32(0)

			for edge := nodes[n].first; edge != 0; edge = edges[edge].next {
				if edges[edge].code == code {
					c = edges[edge].child

					break
				}
			}

			if c == 0 {
				nodes = append(nodes, DartsNode{})
				c = int32(len(nodes) - 1)
				edges = append(edges, DartsEdge{next: nodes[n].first, child: c, code: code})
				nodes[n].first = int32(len(edges) - 1)
			}

			n = c
		}

		nodes[n].key = int32(i) + 1
	}

	d := &Darts{base: []int32{0}, check: []int32{0}, value: []int32{0}}

	d.value[0] = nodes[0].key

	type item struct {
		node  int32
		state int32
	}

	queue := []item{{0, 0}}

	var codesBuf, childrenBuf [256]int32

	for qi := 0; qi < len(queue); qi++ {
		it := queue[qi]
		n := &nodes[it.node]

		if n.first == 0 {
			continue
		}

		codes := codesBuf[:0]
		children := childrenBuf[:0]

		for edge := n.first; edge != 0; edge = edges[edge].next {
			codes = append(codes, int32(edges[edge].code)+1)
			children = append(children, edges[edge].child)
		}

		base := d.findBase(codes)

		d.base[it.state] = base

		for i, c := range codes {
			t := base + c

			d.check[t] = it.state + 1
			child := children[i]

			d.value[t] = nodes[child].key
			queue = append(queue, item{child, t})
		}
	}

	return d
}

func (d *Darts) findBase(codes []int32) int32 {
	maxCode := int32(0)

	for _, code := range codes {
		if code > maxCode {
			maxCode = code
		}
	}

	base := d.baseHint

	if base < 1 {
		base = 1
	}

	for ; ; base++ {
		d.ensure(base + maxCode)

		free := true

		for _, c := range codes {
			if d.check[base+c] != 0 {
				free = false

				break
			}
		}

		if free {
			d.baseHint = base

			return base
		}
	}
}

func (d *Darts) ensure(n int32) {
	for int32(len(d.base)) <= n {
		d.base = append(d.base, 0)
		d.check = append(d.check, 0)
		d.value = append(d.value, 0)
	}
}

func (d *Darts) longestSuffixMatch(s string) (int, bool) {
	st := int32(0)
	best := int32(0)
	found := false

	if d.value[0] != 0 {
		best = d.value[0] - 1
		found = true
	}

	for i := len(s) - 1; i >= 0; i-- {
		t := *unsafeAt(d.base, uint64(st)) + int32(s[i]) + 1

		if t >= int32(len(d.check)) || *unsafeAt(d.check, uint64(t)) != st+1 {
			return int(best), found
		}

		st = t

		if value := *unsafeAt(d.value, uint64(st)); value != 0 {
			best = value - 1
			found = true
		}
	}

	return int(best), found
}

func (d *Darts) longestMatch(parts ...string) (int, bool) {
	s := int32(0)
	best := int32(0)
	found := false

	if d.value[0] != 0 {
		best = d.value[0] - 1
		found = true
	}

	for _, p := range parts {
		for i := 0; i < len(p); i++ {
			t := *unsafeAt(d.base, uint64(s)) + int32(p[i]) + 1

			if t >= int32(len(d.check)) || *unsafeAt(d.check, uint64(t)) != s+1 {
				return int(best), found
			}

			s = t

			if value := *unsafeAt(d.value, uint64(s)); value != 0 {
				best = value - 1
				found = true
			}
		}
	}

	return int(best), found
}
