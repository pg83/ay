package main

func bucketHash(elems []VFS) (uint64, uint64) {
	sum, xr := bucketHashPlatform(elems)
	count := mix64(uint64(len(elems)) + 1)
	h1 := sum + count
	h2 := xr ^ count

	if h1 == 0 {
		h1 = 1
	}

	if h2 == 0 {
		h2 = 1
	}

	return h1, h2
}
