package main

import (
	"encoding/binary"
	"math"

	"github.com/zeebo/xxh3"
)

func nodeUIDWithBuf(n *Node, c *canonBuf) UID {
	c.buf = c.buf[:0]
	c.writeNode(n)

	sum := xxh3.Hash128(c.buf)

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

func (c *canonBuf) writeUint32(n uint32) {
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], n)
	c.buf = append(c.buf, lenBuf[:]...)
}

func (c *canonBuf) writeUint64(n uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], n)
	c.buf = append(c.buf, b[:]...)
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

func (c *canonBuf) writeVFSSlice(vs []VFS) {
	c.writeUint32(uint32(len(vs)))

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

func (c *canonBuf) writeCmdSlice(cmds []Cmd) {
	c.writeUint32(uint32(len(cmds)))

	for _, cm := range cmds {
		c.writeStrSlice(cm.CmdArgs)
		c.writeSTR(cm.Cwd)
		c.writeEnv(cm.Env)
		c.writeBytes(cm.Stdout)
	}
}

func (c *canonBuf) writeEnv(env EnvVars) {
	c.writeUint32(uint32(len(env)))

	for _, e := range env {
		c.writeBytes(e.Name)
		c.writeBytes(e.Value)
	}
}

// --- canonical hash (gen-time self_uid): a fixed-field deterministic encoding.
// The gate recomputes content hashes from the JSON, so only determinism matters
// here, not parity with the old map encoding. ---

func (c *canonBuf) writeRequirements(r Requirements) {
	c.writeUint64(math.Float64bits(r.CPU))
	c.writeUint64(math.Float64bits(r.RAM))
	c.writeBytes(r.Network)
	c.writeUint64(math.Float64bits(r.RAMDisk))
	c.writeBool(r.HasRAMDisk)
}

func (c *canonBuf) writeTargetProperties(t TargetProperties) {
	c.writeBytes(t.ModuleDir)
	c.writeBytes(t.ModuleTag)
	c.writeBytes(t.ModuleLang)
	c.writeBytes(t.ModuleType)
}

func (c *canonBuf) writeKV(kv KV) {
	c.writeByte(byte(kv.P))
	c.writeByte(byte(kv.PC))
	c.writeBytes(kv.ShowOut)
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
	c.writeVFSSlice(n.Inputs)
	c.writeKV(n.KV)
	c.writeVFSSlice(n.Outputs)
	c.writeBytes(string(n.Platform.Target))
	c.writeRequirements(n.Requirements)
	c.writeBool(n.Sandboxing)
	c.writeStrSlice(nodeTags(n))
	c.writeTargetProperties(n.TargetProperties)
}
