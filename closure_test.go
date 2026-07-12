package main

func (cl Closure) flat() []VFS {
	var out []VFS

	cl.each(func(v VFS) { out = append(out, v) })

	return out
}

func closureViewOf(vs ...VFS) Closure {
	var scratch [closureBuckets][]VFS

	for _, v := range vs {
		r := int(v.strID() & (closureBuckets - 1))
		scratch[r] = append(scratch[r], v)
	}

	var buckets [][]VFS

	for r := 0; r < closureBuckets; r++ {
		if len(scratch[r]) > 0 {
			buckets = append(buckets, scratch[r])
		}
	}

	list := BucketList(buckets)

	return Closure{buckets: &list}
}
