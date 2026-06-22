package main

import (
	"math"

	"github.com/zeebo/xxh3"
)

func nodeUIDWithBuf(n *Node, c *CanonBuf) UID {
	c.buf = c.buf[:0]
	c.writeNode(n)

	sum := xxh3.Hash128(c.buf)

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

// resourceFetchUID is the stable uid of a resource FETCH node: a hash of the
// resource URI and output dir, not the whole command. Independence from the
// binary path keeps it cache-stable across machines; output is included so two
// resources sharing a URI but unpacked to different dirs stay distinct.
func resourceFetchUID(uri, output string) UID {
	sum := xxh3.Hash128([]byte(uri + "\x00" + output))

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

type CanonBuf struct {
	buf []byte
	// fs, when set, makes writeVFSSlice mix each $(S) input's content hash into
	// the node hash, so a source edit changes the node uid. Left nil where only
	// the structural hash is wanted.
	fs FS

	// uids resolves DepRefs/ForeignDepRefs to dep uids for the preimage, so deps
	// are never materialized onto the node. Set before writeNode.
	uids *UidVec

	// fetchRefs resolves Resources patterns to FETCH node refs, folding the fetch
	// deps into the uid preimage on the fly rather than storing them on the node.
	fetchRefs *DenseMap[STR, NodeRef]
}

func (c *CanonBuf) writeByte(b byte) {
	c.buf = append(c.buf, b)
}

// writeUint32/writeUint64 append little-endian bytes directly: the spelled-out
// form inlines to stores, while PutUintNN-into-a-stack-array + append paid a
// memmove per write — and this is the inner op of the whole uid layer.
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

// writeSTR mixes an interned token (any ARG/ENV/TOK/VFS resolves to a STR via
// str()) by its recorded xxh3 lo, not its bytes. Equal strings share a STR and
// lo, so the hash matches without materialising the string. STR 0 (empty) has
// the sentinel lo 0.
func (c *CanonBuf) writeSTR(s STR) {
	c.writeUint64(internTable.los[s])
}

// writeRefUIDs serializes refs as their resolved dep uids, so Deps/ForeignDeps
// need not be materialized onto the node.
func (c *CanonBuf) writeRefUIDs(refs []NodeRef) {
	c.writeUint32(uint32(len(refs)))

	for _, r := range refs {
		u := c.uids.get(r)
		c.writeUint64(u.Hi)
		c.writeUint64(u.Lo)
	}
}

// writeDepRefUIDs serializes the node's build deps for the uid preimage:
// DepRefs followed by the resolved resource FETCH deps (Resources) — what the
// "deps" array lists minus the separately-hashed ForeignDepRefs. Resources are
// resolved on the fly through fetchRefs, never stored on the node.
func (c *CanonBuf) writeDepRefUIDs(n *Node) {
	count := len(n.DepRefs)

	for _, pat := range n.Resources {
		if _, ok := c.fetchRefs.get(pat); ok {
			count++
		}
	}

	c.writeUint32(uint32(count))

	for _, r := range n.DepRefs {
		u := c.uids.get(r)
		c.writeUint64(u.Hi)
		c.writeUint64(u.Lo)
	}

	for _, pat := range n.Resources {
		if ref, ok := c.fetchRefs.get(pat); ok {
			u := c.uids.get(ref)
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

// writeStrSlice canonicalises cmd args by each token's STR hash, so the same
// flag string hashes identically regardless of which namespace produced it.
func (c *CanonBuf) writeStrSlice(as []STR) {
	c.writeUint32(uint32(len(as)))

	for _, a := range as {
		c.writeSTR(a)
	}
}

// writeVFSChunks writes the flattened element sequence of the chunk list —
// byte-identical to writeVFSSlice over the concatenation.
func (c *CanonBuf) writeVFSChunks(chunks InputChunks) {
	total := 0

	for _, ch := range chunks {
		total += len(ch)
	}

	c.writeUint32(uint32(total))

	if fs, ok := c.fs.(*OsFS); ok {
		for _, ch := range chunks {
			c.writeVFSSliceOS(ch, fs)
		}

		return
	}

	for _, ch := range chunks {
		c.writeVFSSliceBody(ch)
	}
}

func (c *CanonBuf) writeVFSSlice(vs []VFS) {
	c.writeUint32(uint32(len(vs)))

	if fs, ok := c.fs.(*OsFS); ok {
		c.writeVFSSliceOS(vs, fs)

		return
	}

	c.writeVFSSliceBody(vs)
}

func (c *CanonBuf) writeVFSSliceBody(vs []VFS) {
	for _, v := range vs {
		c.writeSTR(v.str())

		// Mix the content hash of source inputs so editing a $(S) file changes the
		// node uid. $(B) inputs are produced — their content rides the producing
		// node's uid in deps, not here. ContentHash faults if the file was never
		// read by the FS (the hash must have been recorded during gen).
		if c.fs != nil && v.isSource() {
			c.writeUint64(c.fs.contentHash(v))
		}
	}
}

// writeVFSSliceOS is the *osFS arm of writeVFSSlice with loop state hoisted into
// locals — buf, the los array, and the content-hash array. The interface
// ContentHash per element forced a c.buf header reload around every append, and
// gcshape generics keep such a call indirect, so devirtualization is done by
// hand: the hot path inlines ContentHash's array probe, the cold path
// (contentHashSlow) may grow the array and is re-hoisted after. Byte output is
// identical to the generic loop above.
func (c *CanonBuf) writeVFSSliceOS(vs []VFS, fs *OsFS) {
	buf := c.buf
	los := internTable.los
	hashes := fs.contentHashes

	for _, v := range vs {
		s := v.strID()
		lo := los[s]
		buf = append(buf,
			byte(lo), byte(lo>>8), byte(lo>>16), byte(lo>>24),
			byte(lo>>32), byte(lo>>40), byte(lo>>48), byte(lo>>56))

		if !v.isSource() {
			continue
		}

		var h uint64

		if int(s) < len(hashes) && hashes[s] != 0 {
			h = hashes[s]
		} else {
			h = fs.contentHashSlow(v)
			hashes = fs.contentHashes
		}

		buf = append(buf,
			byte(h), byte(h>>8), byte(h>>16), byte(h>>24),
			byte(h>>32), byte(h>>40), byte(h>>48), byte(h>>56))
	}

	c.buf = buf
}

// writeStrChunks writes the flattened element sequence of the chunk list —
// byte-identical to writeStrSlice over the concatenation.
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

// --- canonical hash (gen-time self_uid): a fixed-field deterministic encoding.
// The gate recomputes hashes from the JSON, so only determinism matters here. ---

func (c *CanonBuf) writeRequirements(r Requirements) {
	c.writeUint64(math.Float64bits(r.CPU))
	c.writeUint64(math.Float64bits(r.RAM))
	c.writeByte(byte(r.Network))
	c.writeUint64(math.Float64bits(r.RAMDisk))
	c.writeBool(r.HasRAMDisk)
}

func (c *CanonBuf) writeTargetProperties(t TargetProperties) {
	c.writeBytes(t.ModuleDir)
	c.writeSTR(t.ModuleTag)
	c.writeByte(byte(t.ModuleLang))
	c.writeByte(byte(t.ModuleType))
}

func (c *CanonBuf) writeKV(kv KV) {
	c.writeByte(byte(kv.P))
	c.writeByte(byte(kv.PC))
	c.writeBool(kv.ShowOut)
	c.writeBool(kv.ShowOutBool)
	c.writeBytes(kv.Name)
	c.writeBytes(kv.Path)
	c.writeBytes(kv.DisableCache)
	c.writeBytes(kv.SpecialRunner)
	c.writeBool(kv.HasSpecialRunner)
	c.writeBool(kv.RunTestNode)
	c.writeUint32(uint32(len(kv.ExtOut)))

	// Hash ExtOut in slice order — deterministic per node, so the uid needs no
	// sort (the sorted form is only for the JSON output's parity).
	for _, e := range kv.ExtOut {
		c.writeBytes(e.Key)
		c.writeBytes(e.Val)
	}
}

func (c *CanonBuf) writeNode(n *Node) {
	switch {
	case n.Cache == nil:
		c.writeByte(0)
	case *n.Cache:
		c.writeByte(2)
	default:
		c.writeByte(1)
	}

	c.writeCmdSlice(n.Cmds)
	c.writeDepRefUIDs(n)
	c.writeEnv(n.Env)
	c.writeRefUIDs(n.ForeignDepRefs)
	c.writeVFSChunks(n.Inputs)
	c.writeKV(n.KV)
	c.writeVFSSlice(n.Outputs)
	c.writeBytes(string(n.Platform.Target))
	c.writeRequirements(n.Requirements)
	c.writeBool(n.Sandboxing)
	c.writeTargetProperties(n.TargetProperties)
}
