package main

type ClosureView struct {
	self    VFS
	buckets [closureBuckets][]VFS
}

func (s *IncludeScanner) view(bc BucketClosure, withSelf bool) ClosureView {
	cv := ClosureView{}

	if withSelf {
		cv.self = bc.self
	}

	for r := 0; r < closureBuckets; r++ {
		cv.buckets[r] = s.buckets.list[bc.buckets[r]]
	}

	return cv
}

func (cv ClosureView) len() int {
	n := 0

	if cv.self != 0 {
		n++
	}

	for r := 0; r < closureBuckets; r++ {
		n += len(cv.buckets[r])
	}

	return n
}

func (cv ClosureView) each(fn func(VFS)) {
	if cv.self != 0 {
		fn(cv.self)
	}

	for r := 0; r < closureBuckets; r++ {
		for _, v := range cv.buckets[r] {
			fn(v)
		}
	}
}

func (cv ClosureView) collect(keep func(VFS) bool) []VFS {
	var out []VFS

	cv.each(func(v VFS) {
		if keep(v) {
			out = append(out, v)
		}
	})

	return out
}
