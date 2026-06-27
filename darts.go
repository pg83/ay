package main

import "sort"

type Darts struct {
	base  []int32
	check []int32
	value []int32
}

type dartsNode struct {
	children map[byte]*dartsNode
	key      int32
}

func NewDarts(keys []string) *Darts {
	root := &dartsNode{}

	for i, k := range keys {
		n := root

		for j := 0; j < len(k); j++ {
			b := k[j]

			if n.children == nil {
				n.children = make(map[byte]*dartsNode)
			}

			c := n.children[b]

			if c == nil {
				c = &dartsNode{}

				n.children[b] = c
			}

			n = c
		}

		n.key = int32(i) + 1
	}

	d := &Darts{base: []int32{0}, check: []int32{0}, value: []int32{0}}

	d.value[0] = root.key

	type item struct {
		node  *dartsNode
		state int32
	}

	queue := []item{{root, 0}}

	for len(queue) > 0 {
		it := queue[0]

		queue = queue[1:]

		n := it.node

		if len(n.children) == 0 {
			continue
		}

		codes := make([]int32, 0, len(n.children))

		for b := range n.children {
			codes = append(codes, int32(b)+1)
		}

		sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })

		base := d.findBase(codes)

		d.base[it.state] = base

		for _, c := range codes {
			t := base + c

			d.check[t] = it.state + 1

			child := n.children[byte(c-1)]

			d.value[t] = child.key

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
