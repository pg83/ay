package main

type Darts struct {
	base  []int32
	check []int32
	value []int32
}

type DartsNode struct {
	children [256]int32
	nChild   int
	key      int32
}

func newDarts(keys []string) *Darts {
	nodes := make([]DartsNode, 1, 64)

	for i, k := range keys {
		n := int32(0)

		for j := 0; j < len(k); j++ {
			b := k[j]
			c := nodes[n].children[b]

			if c == 0 {
				nodes = append(nodes, DartsNode{})
				c = int32(len(nodes) - 1)
				nodes[n].children[b] = c
				nodes[n].nChild++
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

	var codesBuf [256]int32

	for qi := 0; qi < len(queue); qi++ {
		it := queue[qi]
		n := &nodes[it.node]

		if n.nChild == 0 {
			continue
		}

		codes := codesBuf[:0]

		for b := 0; b < 256; b++ {
			if n.children[b] != 0 {
				codes = append(codes, int32(b)+1)
			}
		}

		base := d.findBase(codes)

		d.base[it.state] = base

		for _, c := range codes {
			t := base + c

			d.check[t] = it.state + 1

			child := n.children[byte(c-1)]

			d.value[t] = nodes[child].key
			queue = append(queue, item{child, t})
		}
	}

	return d
}

func (d *Darts) findBase(codes []int32) int32 {
	for base := int32(1); ; base++ {
		d.ensure(base + codes[len(codes)-1])

		free := true

		for _, c := range codes {
			if d.check[base+c] != 0 {
				free = false

				break
			}
		}

		if free {
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
		t := d.base[st] + int32(s[i]) + 1

		if t >= int32(len(d.check)) || d.check[t] != st+1 {
			return int(best), found
		}

		st = t

		if d.value[st] != 0 {
			best = d.value[st] - 1
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
			t := d.base[s] + int32(p[i]) + 1

			if t >= int32(len(d.check)) || d.check[t] != s+1 {
				return int(best), found
			}

			s = t

			if d.value[s] != 0 {
				best = d.value[s] - 1
				found = true
			}
		}
	}

	return int(best), found
}
