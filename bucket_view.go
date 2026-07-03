package main

type ClosureView struct {
	self    VFS
	buckets [closureBuckets][]VFS
}

func (s *IncludeScanner) view(bc BucketClosure) ClosureView {
	cv := ClosureView{self: bc.self}

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

	eachBucketVFS(cv.buckets[:], fn)
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

// eachBucketVFS / collectBucketVFS operate on a closure's bucket chunks
// ([][]VFS) — i.e. the closure without self. Consumers that do not need self
// pass cv.buckets[:] and use these instead of the ClosureView methods.
func eachBucketVFS(chunks [][]VFS, fn func(VFS)) {
	for _, ch := range chunks {
		for _, v := range ch {
			fn(v)
		}
	}
}

func collectBucketVFS(chunks [][]VFS, keep func(VFS) bool) []VFS {
	var out []VFS

	eachBucketVFS(chunks, func(v VFS) {
		if keep(v) {
			out = append(out, v)
		}
	})

	return out
}
