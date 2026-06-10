package main

import (
	"math"

	"github.com/zeebo/xxh3"
)

func nodeUIDWithBuf(n *Node, c *canonBuf) UID {
	c.buf = c.buf[:0]
	c.writeNode(n)

	sum := xxh3.Hash128(c.buf)

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

// resourceFetchUID is the stable uid of a resource FETCH node: a hash of the
// resource URI and its output dir, NOT of the whole command. This keeps the uid
// independent of the ay binary path (and any other command detail) so a node that
// fetches the same sandbox resource into the same place is cache-stable across
// machines. Output is included so two resources sharing a URI (e.g. CLANG and
// CLANG20 both pinned to one clang20 sbr) but unpacked to different dirs stay distinct.
func resourceFetchUID(uri, output string) UID {
	sum := xxh3.Hash128([]byte(uri + "\x00" + output))

	return UID{Hi: sum.Hi, Lo: sum.Lo}
}

type canonBuf struct {
	buf []byte
	// fs, when set, makes writeVFSSlice mix each $(S) input's file-content hash
	// (xxh3, recorded by the FS on read) into the node hash, so a source edit
	// changes the node uid. Left nil where only the structural hash is wanted
	// (e.g. the dump/-G path, which is re-uid'd from canonical content anyway).
	fs FS

	// uids resolves a node's DepRefs/ForeignDepRefs to dep uids for the preimage,
	// so deps are never materialized onto the node. Set before writeNode.
	uids *uidVec
}

func (c *canonBuf) writeByte(b byte) {
	c.buf = append(c.buf, b)
}

// writeUint32/writeUint64 append the little-endian bytes directly: the
// spelled-out form compiles to inline stores, while the PutUintNN-into-a-
// stack-array + append(b[:]...) shape paid a runtime memmove call per write —
// and this is the inner op of the whole uid layer (every STR lo, dep uid half,
// content hash).
func (c *canonBuf) writeUint32(n uint32) {
	c.buf = append(c.buf, byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
}

func (c *canonBuf) writeUint64(n uint64) {
	c.buf = append(c.buf,
		byte(n), byte(n>>8), byte(n>>16), byte(n>>24),
		byte(n>>32), byte(n>>40), byte(n>>48), byte(n>>56))
}

func (c *canonBuf) writeBool(b bool) {
	if b {
		c.buf = append(c.buf, 1)
	} else {
		c.buf = append(c.buf, 0)
	}
}

func (c *canonBuf) writeBytes(s string) {
	c.writeUint32(uint32(len(s)))
	c.buf = append(c.buf, s...)
}

// writeSTR mixes an interned token (STR — and therefore any ARG/ENV/TOK/VFS,
// which all resolve to a STR via str()) by the xxh3 lo recorded per STR in
// internTable.los, not its bytes. Equal strings share a STR and thus a lo, so the
// hash is identical without materialising the string. STR 0 (the empty token) has
// the sentinel lo 0.
func (c *canonBuf) writeSTR(s STR) {
	c.writeUint64(internTable.los[s])
}

// writeRefUIDs serializes refs as their resolved dep uids (looked up by id in
// c.uids), so Deps/ForeignDeps need not be materialized onto the node.
func (c *canonBuf) writeRefUIDs(refs []NodeRef) {
	c.writeUint32(uint32(len(refs)))

	for _, r := range refs {
		u := c.uids.get(r)
		c.writeUint64(u.Hi)
		c.writeUint64(u.Lo)
	}
}

func (c *canonBuf) writeStringSlice(ss []string) {
	c.writeUint32(uint32(len(ss)))

	for _, s := range ss {
		c.writeBytes(s)
	}
}

// writeStrSlice canonicalises cmd args by each token's STR hash (writeSTR), so a
// node's UID is identical whether an arg reached the STR via ARG/STR/VFS/TOK
// str() — the same flag string hashes the same regardless of which namespace
// produced it.
func (c *canonBuf) writeStrSlice(as []STR) {
	c.writeUint32(uint32(len(as)))

	for _, a := range as {
		c.writeSTR(a)
	}
}

// writeVFSChunks writes the flattened element sequence of the chunk list —
// byte-identical to writeVFSSlice over the concatenation.
func (c *canonBuf) writeVFSChunks(chunks inputChunks) {
	total := 0

	for _, ch := range chunks {
		total += len(ch)
	}

	c.writeUint32(uint32(total))

	if fs, ok := c.fs.(*osFS); ok {
		for _, ch := range chunks {
			c.writeVFSSliceOS(ch, fs)
		}

		return
	}

	for _, ch := range chunks {
		c.writeVFSSliceBody(ch)
	}
}

func (c *canonBuf) writeVFSSlice(vs []VFS) {
	c.writeUint32(uint32(len(vs)))

	if fs, ok := c.fs.(*osFS); ok {
		c.writeVFSSliceOS(vs, fs)

		return
	}

	c.writeVFSSliceBody(vs)
}

func (c *canonBuf) writeVFSSliceBody(vs []VFS) {
	for _, v := range vs {
		c.writeSTR(v.str())

		// Mix the content hash of source inputs so editing a $(S) file changes the
		// node uid. $(B) inputs are produced — their content is captured via the
		// producing node's uid in deps, not here. ContentHash faults if the file
		// was never read by the FS (the hash must have been recorded during gen).
		if c.fs != nil && v.IsSource() {
			c.writeUint64(c.fs.ContentHash(v))
		}
	}
}

// writeVFSSliceOS is the *osFS arm of writeVFSSlice with the loop state hoisted
// into locals — buf, the los array, and the content-hash array. The interface
// ContentHash per $(S) element forced a c.buf header reload around every append
// (per the disasm), and gcshape generics keep such a call indirect (one shape
// instantiation serves every pointer type arg), so the devirtualization is done
// by hand: the hot path is ContentHash's array probe inlined here, the cold
// path (contentHashSlow, the lazy read) may grow the array and is re-hoisted
// after. Byte output is identical to the generic loop above.
func (c *canonBuf) writeVFSSliceOS(vs []VFS, fs *osFS) {
	buf := c.buf
	los := internTable.los
	hashes := fs.contentHashes

	for _, v := range vs {
		s := v.strID()
		lo := los[s]
		buf = append(buf,
			byte(lo), byte(lo>>8), byte(lo>>16), byte(lo>>24),
			byte(lo>>32), byte(lo>>40), byte(lo>>48), byte(lo>>56))

		if !v.IsSource() {
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

func (c *canonBuf) writeCmdSlice(cmds []Cmd) {
	c.writeUint32(uint32(len(cmds)))

	for _, cm := range cmds {
		c.writeStrSlice(cm.CmdArgs)
		c.writeSTR(cm.Cwd)
		c.writeEnv(cm.Env)
		c.writeBytes(cm.Stdout.String())
	}
}

func (c *canonBuf) writeEnv(env EnvVars) {
	c.writeUint32(uint32(len(env)))

	for _, e := range env {
		c.writeSTR(e.Name.str())
		c.writeSTR(e.Value)
	}
}

// --- canonical hash (gen-time self_uid): a fixed-field deterministic encoding.
// The gate recomputes content hashes from the JSON, so only determinism matters
// here, not parity with the old map encoding. ---

func (c *canonBuf) writeRequirements(r Requirements) {
	c.writeUint64(math.Float64bits(r.CPU))
	c.writeUint64(math.Float64bits(r.RAM))
	c.writeByte(byte(r.Network))
	c.writeUint64(math.Float64bits(r.RAMDisk))
	c.writeBool(r.HasRAMDisk)
}

func (c *canonBuf) writeTargetProperties(t TargetProperties) {
	c.writeBytes(t.ModuleDir)
	c.writeSTR(t.ModuleTag)
	c.writeByte(byte(t.ModuleLang))
	c.writeByte(byte(t.ModuleType))
}

func (c *canonBuf) writeKV(kv KV) {
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
	// sort (the sorted form is only for the JSON output's parity, see appendKV).
	for _, e := range kv.ExtOut {
		c.writeBytes(e.Key)
		c.writeBytes(e.Val)
	}
}

func (c *canonBuf) writeNode(n *Node) {
	switch {
	case n.Cache == nil:
		c.writeByte(0)
	case *n.Cache:
		c.writeByte(2)
	default:
		c.writeByte(1)
	}

	c.writeCmdSlice(n.Cmds)
	c.writeRefUIDs(n.DepRefs)
	c.writeEnv(n.Env)
	c.writeRefUIDs(n.ForeignDepRefs)
	c.writeVFSChunks(n.Inputs)
	c.writeKV(n.KV)
	c.writeVFSSlice(n.Outputs)
	c.writeBytes(string(n.Platform.Target))
	c.writeRequirements(n.Requirements)
	c.writeBool(n.Sandboxing)
	c.writeStrSlice(nodeTags(n))
	c.writeTargetProperties(n.TargetProperties)
}
