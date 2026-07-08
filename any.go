package main

type ANY uint32

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

func (a ANY) relOrSelf() STR {
	if v := a.vfs(); v != 0 {
		return v.rel()
	}

	return a.str()
}

func pathAny(s STR) ANY {
	if v := s.vfs(); v != 0 {
		return v.any()
	}

	return s.any()
}

func (a ANY) strID() uint32 {
	return uint32(a)
}
