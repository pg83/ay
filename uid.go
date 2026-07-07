package main

import (
	"github.com/zeebo/xxh3"
)

func (c *CanonBuf) calcUID(n *Node) UID {
	c.buf = c.buf[:0]
	c.writeNode(n)

	sum := xxh3.Hash128(c.buf)

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

func resourceFetchUID(uri, output string) *UID {
	sum := xxh3.Hash128([]byte(uri + "\x00" + output))

	return &UID{Hi: sum.Hi, Lo: sum.Lo}
}

type chunkAccum struct {
	sum, xor, sq, cb uint64
}

type chunkKey struct {
	p *VFS
	n int
}

type CanonBuf struct {
	buf       []byte
	scratch   []uint64
	eVals     []uint64
	eSeen     []bool
	chunkMemo map[chunkKey]chunkAccum
	hash      func(VFS) uint64
	fsHashes  *PageVec[uint64]
	futs      *PageVec[*NodeFuture]
	fetchRefs *DenseMap[STR, NodeRef]
}

func (c *CanonBuf) inputVal(v VFS) uint64 {
	id := v.strID()

	if int(id) >= len(c.eSeen) {
		n := int(id) + 1 + len(c.eSeen)

		eVals := make([]uint64, n)
		eSeen := make([]bool, n)

		copy(eVals, c.eVals)
		copy(eSeen, c.eSeen)
		c.eVals = eVals
		c.eSeen = eSeen
	}

	if !c.eSeen[id] {
		e := internTable.cells.get(id >> 1).lo ^ uint64(id&1)

		if v.isSource() {
			e ^= c.sourceHash(v)
		}

		c.eVals[id] = e
		c.eSeen[id] = true
	}

	return c.eVals[id]
}

func (c *CanonBuf) sourceHash(v VFS) uint64 {
	if c.fsHashes != nil {
		if h := c.fsHashes.getSafe(v.strID()); h != 0 {
			return h
		}
	}

	return c.hash(v)
}

func (c *CanonBuf) writeByte(b byte) {
	c.buf = append(c.buf, b)
}

func (c *CanonBuf) writeUint32(n uint32) {
	c.buf = append(c.buf, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
}

func (c *CanonBuf) writeUint64(n uint64) {
	c.buf = append(c.buf,
		byte(n), byte(n>>8), byte(n>>16), byte(n>>24),
		byte(n>>32), byte(n>>40), byte(n>>48), byte(n>>56))
}

func (c *CanonBuf) writeBool(b bool) {
	if b {
		c.buf = append(c.buf, 1)
	} else {
		c.buf = append(c.buf, 0)
	}
}

func (c *CanonBuf) writeBytes(s string) {
	c.writeUint32(uint32(len(s)))
	c.buf = append(c.buf, s...)
}

func (c *CanonBuf) writeSTR(s STR) {
	c.writeUint64(internTable.cells.get(uint32(s)).lo)
}

func (c *CanonBuf) writeRefUIDs(refs []NodeRef) {
	c.writeUint32(uint32(len(refs)))

	for _, r := range refs {
		u := c.futs.get(uint32(r)).uid

		c.writeUint64(u.Hi)
		c.writeUint64(u.Lo)
	}
}

func (c *CanonBuf) writeDepRefUIDs(n *Node) {
	count := len(n.DepRefs)

	for _, pat := range n.Resources {
		if _, ok := c.fetchRefs.get(pat); ok {
			count++
		}
	}

	c.writeUint32(uint32(count))

	for _, r := range n.DepRefs {
		u := c.futs.get(uint32(r)).uid

		c.writeUint64(u.Hi)
		c.writeUint64(u.Lo)
	}

	for _, pat := range n.Resources {
		if ref, ok := c.fetchRefs.get(pat); ok {
			u := c.futs.get(uint32(ref)).uid

			c.writeUint64(u.Hi)
			c.writeUint64(u.Lo)
		}
	}
}

func (c *CanonBuf) writeStringSlice(ss []string) {
	c.writeUint32(uint32(len(ss)))

	for _, s := range ss {
		c.writeBytes(s)
	}
}

func (c *CanonBuf) writeStrSlice(as []STR) {
	c.writeUint32(uint32(len(as)))

	for _, a := range as {
		c.writeSTR(a)
	}
}

func (c *CanonBuf) writeVFSChunks(chunks InputChunks) {
	total := 0

	for _, ch := range chunks {
		total += len(ch)
	}

	c.writeUint32(uint32(total))

	var sum, xor, sq, cb uint64

	for _, ch := range chunks {
		if len(ch) == 0 {
			continue
		}

		key := chunkKey{p: &ch[0], n: len(ch)}
		a, ok := c.chunkMemo[key]

		if !ok {
			a = c.chunkAccumOf(ch)
			c.chunkMemo[key] = a
		}

		sum += a.sum
		xor ^= a.xor
		sq += a.sq
		cb += a.cb
	}

	c.writeUint64(sum)
	c.writeUint64(xor)
	c.writeUint64(sq)
	c.writeUint64(cb)
}

func (c *CanonBuf) chunkAccumOf(ch []VFS) chunkAccum {
	if cap(c.scratch) < len(ch) {
		c.scratch = make([]uint64, len(ch))
	}

	es := c.scratch[:len(ch)]

	for i, v := range ch {
		es[i] = c.inputVal(v)
	}

	sum, xor, sq, cb := uidAccum(es)

	return chunkAccum{sum: sum, xor: xor, sq: sq, cb: cb}
}

func (c *CanonBuf) writeVFSSlice(vs []VFS) {
	c.writeUint32(uint32(len(vs)))
	c.writeVFSSliceBody(vs)
}

func (c *CanonBuf) writeVFSSliceBody(vs []VFS) {
	for _, v := range vs {
		c.writeSTR(v.fullSTR())

		if v.isSource() {
			c.writeUint64(c.hash(v))
		}
	}
}

func (c *CanonBuf) writeStrChunks(chunks ArgChunks) {
	total := 0

	for _, ch := range chunks {
		total += len(ch)
	}

	c.writeUint32(uint32(total))

	for _, ch := range chunks {
		for _, a := range ch {
			c.writeSTR(a)
		}
	}
}

func (c *CanonBuf) writeCmdSlice(cmds []Cmd) {
	c.writeUint32(uint32(len(cmds)))

	for _, cm := range cmds {
		c.writeStrChunks(cm.CmdArgs)
		c.writeSTR(cm.Cwd)
		c.writeEnv(cm.Env)
		c.writeSTR(cm.Stdout)
	}
}

func (c *CanonBuf) writeEnv(env EnvVars) {
	c.writeUint32(uint32(len(env)))

	for _, e := range env {
		c.writeSTR(e.Name.str())
		c.writeSTR(e.Value)
	}
}

func (c *CanonBuf) writeNode(n *Node) {
	c.writeCmdSlice(n.Cmds)
	c.writeDepRefUIDs(n)
	c.writeEnv(n.Env)
	c.writeRefUIDs(n.ForeignDepRefs)
	c.writeVFSChunks(n.Inputs)
	c.writeVFSSlice(n.Outputs)
	c.writeBytes(string(n.Platform.Target))
}
