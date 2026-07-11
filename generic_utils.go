package main

func ptr[T any](v T) *T {
	return &v
}

func scrub[T any](s []T) []T {
	clear(s)

	return s[:0]
}

func scrubCap[T any](s []T) []T {
	clear(s[:cap(s)])

	return s[:0]
}

func concat[T any](lists ...[]T) []T {
	total := 0
	nonEmpty := 0
	last := -1

	for i, l := range lists {
		if len(l) > 0 {
			total += len(l)
			nonEmpty++
			last = i
		}
	}

	if total == 0 {
		return nil
	}

	if nonEmpty == 1 {
		return lists[last]
	}

	out := make([]T, 0, total)

	for _, l := range lists {
		out = append(out, l...)
	}

	return out
}

func astOne[T any](a *BumpAllocator[T], v T) *T {
	p := a.one()

	*p = v

	return p
}

func arenaAppend[T any](a *BumpAllocator[T], cur []T, v T) []T {
	if len(cur) < cap(cur) {
		cur = cur[:len(cur)+1]
		cur[len(cur)-1] = v

		return cur
	}

	n := 2 * cap(cur)

	if n < 4 {
		n = 4
	}

	block := a.alloc(n)

	copy(block, cur)
	a.commit(n)

	out := block[: len(cur)+1 : n]

	out[len(cur)] = v

	return out
}
