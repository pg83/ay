package main

func (cv ClosureView) flat() []VFS {
	var out []VFS

	cv.each(func(v VFS) { out = append(out, v) })

	return out
}

func closureViewOf(vs ...VFS) ClosureView {
	var cv ClosureView

	for _, v := range vs {
		r := int(v.strID() & (closureBuckets - 1))
		cv.buckets[r] = append(cv.buckets[r], v)
	}

	return cv
}
