package main

func ptr[T any](v T) *T {
	return &v
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
