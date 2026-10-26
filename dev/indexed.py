#!/usr/bin/env python3
"""Convert a resource tarball (any compression tarfile reads) into the
.indexed archive read by indexed/indexed.go: a header, the entry table and
the names, then the file contents laid out flat.

Two streaming passes over the tarball: the first collects the tree, the
second writes the contents in archive order, so the output is written
sequentially. Symlinks and hard links are resolved inside the archive; a
link that leaves the tree or dangles is dropped, as a stat-following walk of
the unpacked tree would not see it.
"""

from __future__ import annotations

import argparse
import os
import posixpath
import stat
import struct
import sys
import tarfile
import zipfile

MAGIC = b"AYIDX\x00\x00\x01"
HEADER = 64
ENTRY = struct.Struct("<IIIIQQ")
KIND_FILE, KIND_DIR = 1, 2
ALIGN = 4096
MAX_LINK_DEPTH = 40


class Node:
    __slots__ = ("kind", "mode", "children", "link", "member", "size", "offset", "alias")

    def __init__(self, kind):
        self.kind = kind  # "file", "dir", "symlink", "hardlink"
        self.mode = 0o644
        self.children = {} if kind == "dir" else None
        self.link = None
        self.member = -1
        self.size = 0
        self.offset = 0
        self.alias = None


def clean(name: str) -> str:
    name = posixpath.normpath("/" + name).lstrip("/")
    return "" if name == "." else name


class Tree:
    def __init__(self):
        self.root = Node("dir")
        self.root.mode = 0o755

    def parent(self, path: str) -> Node:
        node = self.root
        if not path:
            return node
        for part in path.split("/"):
            child = node.children.get(part)
            if child is None or child.kind != "dir":
                child = Node("dir")
                child.mode = 0o755
                node.children[part] = child
            node = child
        return node

    def add(self, path: str, node: Node) -> None:
        if not path:
            return
        directory, name = posixpath.split(path)
        parent = self.parent(directory)
        previous = parent.children.get(name)
        if previous is not None and previous.kind == "dir" and node.kind == "dir":
            previous.mode = node.mode
            return
        parent.children[name] = node

    def lookup(self, path: str, depth: int = 0):
        """Follows symlinks in every component; returns the final node."""
        if depth > MAX_LINK_DEPTH:
            return None
        node = self.root
        done = ""
        parts = path.split("/") if path else []
        for index, part in enumerate(parts):
            if node.kind != "dir":
                return None
            child = node.children.get(part)
            if child is None:
                return None
            here = posixpath.join(done, part)
            if child.kind == "symlink":
                target = child.link
                if target.startswith("/"):
                    return None
                resolved = clean(posixpath.join(done, target))
                if posixpath.normpath(posixpath.join(done, target)).startswith(".."):
                    return None
                rest = "/".join(parts[index + 1:])
                return self.lookup(posixpath.join(resolved, rest) if rest else resolved, depth + 1)
            if child.kind == "hardlink":
                return self.lookup(child.link, depth + 1)
            node, done = child, here
        return node


def is_zip(path: str) -> bool:
    with open(path, "rb") as stream:
        return stream.read(2) == b"PK"


def members(path: str):
    """(ordinal, name, kind, mode, size, link) for every archive entry."""
    if is_zip(path):
        with zipfile.ZipFile(path) as archive:
            for ordinal, info in enumerate(archive.infolist()):
                mode = info.external_attr >> 16
                if info.is_dir() or stat.S_ISDIR(mode):
                    yield ordinal, info.filename, "dir", mode, 0, None
                elif stat.S_ISLNK(mode):
                    yield ordinal, info.filename, "symlink", mode, 0, archive.read(info).decode("utf-8", "surrogateescape")
                else:
                    yield ordinal, info.filename, "file", mode or 0o644, info.file_size, None
        return
    with tarfile.open(path, mode="r|*") as archive:
        for ordinal, member in enumerate(archive):
            if member.isdir():
                yield ordinal, member.name, "dir", member.mode, 0, None
            elif member.issym():
                yield ordinal, member.name, "symlink", member.mode, 0, member.linkname
            elif member.islnk():
                yield ordinal, member.name, "hardlink", member.mode, 0, clean(member.linkname)
            elif member.isreg():
                yield ordinal, member.name, "file", member.mode, member.size, None


def contents(path: str, wanted: set[int]):
    """(ordinal, stream) for the wanted regular-file entries, in archive order."""
    if is_zip(path):
        with zipfile.ZipFile(path) as archive:
            for ordinal, info in enumerate(archive.infolist()):
                if ordinal in wanted:
                    with archive.open(info) as stream:
                        yield ordinal, stream
        return
    with tarfile.open(path, mode="r|*") as archive:
        for ordinal, member in enumerate(archive):
            if ordinal in wanted:
                yield ordinal, archive.extractfile(member)


def collect(path: str) -> tuple[Tree, list[tuple[str, Node]]]:
    tree = Tree()
    files: list[tuple[str, Node]] = []
    for ordinal, raw_name, kind, mode, size, link in members(path):
        name = clean(raw_name)
        if not name:
            continue
        node = Node(kind)
        node.link = link
        if kind == "file":
            node.size = size
            node.member = ordinal
            files.append((name, node))
        node.mode = mode & 0o7777
        tree.add(name, node)
    return tree, files


def find_root(tree: Tree, marker: str) -> str:
    queue = [("", tree.root)]
    while queue:
        following = []
        for path, node in queue:
            child = node.children.get(marker)
            if child is not None and child.kind == "file":
                return path
            for name in sorted(node.children):
                sub = node.children[name]
                if sub.kind == "dir":
                    following.append((posixpath.join(path, name), sub))
        queue = following
    raise SystemExit(f"indexed: no {marker} in the archive")


def resolve_links(tree: Tree, node: Node, path: str) -> None:
    """Replaces link children by the nodes they resolve to (or drops them)."""
    for name in list(node.children):
        child = node.children[name]
        here = posixpath.join(path, name)
        if child.kind in ("symlink", "hardlink"):
            target = tree.lookup(here)
            if target is None or target.kind not in ("file", "dir"):
                del node.children[name]
                continue
            child.alias = target
        elif child.kind == "dir":
            resolve_links(tree, child, here)


def final(node: Node) -> Node:
    return node.alias if node.alias is not None else node


def layout(top: Node):
    """Breadth-first entry order; children of a directory are contiguous."""
    order = [top]
    index_of_dir = {id(top): 0}
    names = [b""]
    first_child = {}
    queue = [top]
    while queue:
        following = []
        for directory in queue:
            children = sorted(
                ((name.encode("utf-8", "surrogateescape"), final(child)) for name, child in directory.children.items()),
                key=lambda item: item[0],
            )
            first_child[id(directory)] = (len(order), len(children))
            for encoded, child in children:
                names.append(encoded)
                order.append(child)
                if child.kind == "dir" and id(child) not in index_of_dir:
                    index_of_dir[id(child)] = len(order) - 1
                    following.append(child)
        queue = following
    return order, names, first_child


def write(path: str, archive_path: str, top: Node, files: list[tuple[str, Node]]) -> None:
    order, names, first_child = layout(top)

    reachable = {id(node) for node in order if node.kind == "file"}
    data_size = 0
    written = []
    for _name, node in files:
        if id(node) in reachable:
            node.offset = data_size
            data_size += node.size
            written.append(node)

    names_blob = bytearray()
    entries = bytearray()
    for node, name in zip(order, names):
        offset = len(names_blob)
        names_blob += name
        if node.kind == "dir":
            first, count = first_child[id(node)]
            entries += ENTRY.pack(offset, len(name), KIND_DIR, node.mode, first, count)
        else:
            entries += ENTRY.pack(offset, len(name), KIND_FILE, node.mode, node.offset, node.size)

    names_offset = HEADER + len(entries)
    data_offset = (names_offset + len(names_blob) + ALIGN - 1) // ALIGN * ALIGN
    header = MAGIC + struct.pack("<QQQQQ", len(order), names_offset, len(names_blob), data_offset, data_size)
    header += b"\x00" * (HEADER - len(header))

    temporary = path + ".tmp"
    with open(temporary, "wb") as out:
        out.write(header)
        out.write(entries)
        out.write(names_blob)
        out.write(b"\x00" * (data_offset - out.tell()))
        by_member = {node.member: node for node in written}
        for ordinal, stream in contents(archive_path, set(by_member)):
            node = by_member[ordinal]
            remaining = node.size
            while remaining:
                chunk = stream.read(min(remaining, 1 << 20))
                if not chunk:
                    raise SystemExit(f"indexed: entry {ordinal}: archive ended early")
                out.write(chunk)
                remaining -= len(chunk)
        if out.tell() != data_offset + data_size:
            raise SystemExit("indexed: data size mismatch")
    os.replace(temporary, path)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--in", dest="source", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--root-marker", help="re-root at the shallowest directory holding this file")
    args = parser.parse_args()

    tree, files = collect(args.source)
    root = find_root(tree, args.root_marker) if args.root_marker else ""
    resolve_links(tree, tree.root, "")
    top = tree.lookup(root) if root else tree.root
    write(args.out, args.source, top, files)
    return 0


if __name__ == "__main__":
    sys.exit(main())
