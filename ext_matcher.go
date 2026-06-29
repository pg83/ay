package main

type ExtEntry[T any] struct {
	Ext string
	Val T
}

type ExtMatcher[T any] struct {
	darts  *Darts
	values []T
}

func newExtMatcher[T any](entries []ExtEntry[T]) *ExtMatcher[T] {
	keys := make([]string, len(entries))
	values := make([]T, len(entries))

	for i, e := range entries {
		keys[i] = reverseStr(e.Ext)
		values[i] = e.Val
	}

	return &ExtMatcher[T]{darts: newDarts(keys), values: values}
}

func (m *ExtMatcher[T]) match(path string) (T, bool) {
	i, ok := m.darts.longestSuffixMatch(path)

	if !ok {
		var zero T

		return zero, false
	}

	return m.values[i], true
}
