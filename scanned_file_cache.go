package main

type ScannedFileCache struct {
	DenseMap2[VFS, []VFS, Closure]
	deferred    BitSet
	diagnostics DenseMap[uint32, *IncludeDiagnostics]
}

func (c *ScannedFileCache) get1(file VFS) (ScannedChildren, bool) {
	paths, ok := c.DenseMap2.get1(file)

	if !ok {
		return ScannedChildren{}, false
	}

	slot := c.rowSlot(file)
	diagnostics, _ := c.diagnostics.get(slot)

	return ScannedChildren{Paths: paths, Deferred: c.deferred.has(slot), Diagnostics: diagnostics}, true
}

func (c *ScannedFileCache) put1(file VFS, entry ScannedChildren) {
	c.DenseMap2.put1(file, entry.Paths)

	slot := c.rowSlot(file)

	if entry.Deferred {
		c.deferred.add(slot)
	} else {
		c.deferred.remove(slot)
	}

	if entry.Diagnostics != nil {
		c.diagnostics.put(slot, entry.Diagnostics)
	} else if previous, _ := c.diagnostics.get(slot); previous != nil {
		c.diagnostics.put(slot, nil)
	}
}
