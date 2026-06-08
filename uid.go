package main

import (
	"encoding/binary"

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

// writeStrSlice canonicalises cmd args by their materialized string, so a node's
// UID is identical whether an arg reached the STR via ARG/STR/VFS/TOK str() — the
// same flag string hashes the same regardless of which namespace produced it.
func (c *canonBuf) writeStrSlice(as []STR) {
	c.writeUint32(uint32(len(as)))

	for _, a := range as {
		c.writeBytes(a.String())
	}
}

func (c *canonBuf) writeVFSSlice(vs []VFS) {
	c.writeUint32(uint32(len(vs)))

	for _, v := range vs {
		c.writeUint64(internTable.los[v.strID()])

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
		c.writeBytes(cm.Cwd.String())
		c.writeEnv(cm.Env)
		c.writeBytes(cm.Stdout)
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
	c.writeBytes(platformTarget(n.Platform))
	c.writeRequirements(n.Requirements)
	c.writeBool(n.Sandboxing)
	c.writeStringSlice(n.Tags)
	c.writeTargetProperties(n.TargetProperties)
}
