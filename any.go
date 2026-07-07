package main

type ANY uint32

func (s STR) any() ANY {
	return ANY(uint32(s) << 1)
}

func (v VFS) any() ANY {
	return ANY(uint32(v)<<1 | 1)
}

func (a ANY) vfs() VFS {
	if uint32(a)&1 == 0 {
		return 0
	}

	return VFS(uint32(a) >> 1)
}

func (a ANY) str() STR {
	if uint32(a)&1 != 0 {
		return 0
	}

	return STR(uint32(a) >> 1)
}

func (a ANY) string() string {
	if v := a.vfs(); v != 0 {
		return v.string()
	}

	return a.str().string()
}

func (a ANY) sharedString() string {
	if v := a.vfs(); v != 0 {
		return v.sharedString()
	}

	return a.str().sharedString()
}

func (a ARG) any() ANY {
	return a.str().any()
}
