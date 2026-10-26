import io
import struct
import tarfile
import tempfile
import unittest
from pathlib import Path

import indexed


class IndexedReader:
    def __init__(self, path: Path):
        self.raw = path.read_bytes()
        assert self.raw[:8] == indexed.MAGIC
        count, names_offset, names_size, data_offset, data_size = struct.unpack_from("<QQQQQ", self.raw, 8)
        self.entries = [indexed.ENTRY.unpack_from(self.raw, indexed.HEADER + i * indexed.ENTRY.size) for i in range(count)]
        self.names = self.raw[names_offset:names_offset + names_size]
        self.data = self.raw[data_offset:data_offset + data_size]
        assert data_offset % indexed.ALIGN == 0

    def name(self, i):
        offset, length = self.entries[i][:2]
        return self.names[offset:offset + length].decode()

    def children(self, i):
        _, _, kind, _, first, count = self.entries[i]
        assert kind == indexed.KIND_DIR
        return {self.name(j): j for j in range(first, first + count)}

    def lookup(self, path):
        i = 0
        for part in filter(None, path.split("/")):
            i = self.children(i)[part]
        return i

    def read(self, path):
        _, _, kind, _, offset, size = self.entries[self.lookup(path)]
        assert kind == indexed.KIND_FILE
        return self.data[offset:offset + size]

    def tree(self, i=0, prefix=""):
        out = {}
        for name, j in self.children(i).items():
            path = prefix + name
            if self.entries[j][2] == indexed.KIND_DIR:
                out[path + "/"] = None
                out.update(self.tree(j, path + "/"))
            else:
                out[path] = self.read(path)
        return out


class IndexedTest(unittest.TestCase):
    def pack(self, members, root_marker=None):
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(directory))
        archive = directory / "in.tar.zst"
        with tarfile.open(archive, "w:zst") as stream:
            for name, kind, value, *mode in members:
                info = tarfile.TarInfo(name)
                if mode:
                    info.mode = mode[0]
                if kind == "file":
                    info.size = len(value)
                    stream.addfile(info, io.BytesIO(value))
                    continue
                info.type = {"dir": tarfile.DIRTYPE, "sym": tarfile.SYMTYPE, "hard": tarfile.LNKTYPE}[kind]
                info.linkname = value or ""
                stream.addfile(info)
        output = directory / "out.indexed"
        args = ["--in", str(archive), "--out", str(output)]
        if root_marker:
            args += ["--root-marker", root_marker]
        import sys
        saved = sys.argv
        sys.argv = ["indexed.py", *args]
        try:
            indexed.main()
        finally:
            sys.argv = saved
        return IndexedReader(output)

    def test_links_are_resolved_and_children_sorted(self):
        reader = self.pack([
            ("b/z.txt", "file", b"zz"),
            ("b/a.txt", "file", b"aa"),
            ("link_to_file", "sym", "b/a.txt"),
            ("link_to_dir", "sym", "b"),
            ("dangling", "sym", "missing"),
            ("outside", "sym", "../etc/passwd"),
            ("hard", "hard", "b/z.txt"),
            ("dup.txt", "file", b"first"),
            ("dup.txt", "file", b"second"),
        ])
        self.assertEqual(reader.tree(), {
            "b/": None,
            "b/a.txt": b"aa",
            "b/z.txt": b"zz",
            "dup.txt": b"second",
            "hard": b"zz",
            "link_to_dir/": None,
            "link_to_dir/a.txt": b"aa",
            "link_to_dir/z.txt": b"zz",
            "link_to_file": b"aa",
        })
        self.assertEqual(list(reader.children(0)), sorted(reader.children(0)))
        self.assertEqual(len(reader.data), len(b"aazzsecond"))

    def test_zip_archives_with_symlinks(self):
        import zipfile
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(directory))
        archive = directory / "in.zip"
        with zipfile.ZipFile(archive, "w") as stream:
            stream.writestr("arcadia/.arcadia.root", b"")
            stream.writestr("arcadia/a/file.txt", b"data")
            link = zipfile.ZipInfo("arcadia/link")
            link.external_attr = (0o120777 << 16)
            stream.writestr(link, b"a/file.txt")
        output = directory / "out.indexed"
        import sys
        saved = sys.argv
        sys.argv = ["indexed.py", "--in", str(archive), "--out", str(output), "--root-marker", ".arcadia.root"]
        try:
            indexed.main()
        finally:
            sys.argv = saved
        self.assertEqual(IndexedReader(output).tree(), {
            ".arcadia.root": b"",
            "a/": None,
            "a/file.txt": b"data",
            "link": b"data",
        })

    def test_modes_follow_unpacking(self):
        reader = self.pack([
            ("d/secret", "file", b"x", 0o000),
            ("d/link", "sym", "secret"),
        ])
        reader_entries = {reader.name(i): reader.entries[i] for i in range(len(reader.entries))}
        self.assertEqual(reader_entries["d"][3], 0o755)
        self.assertEqual(reader_entries["secret"][3], 0o000)
        self.assertTrue(reader_entries["link"][3] & indexed.MODE_SYMLINK)
        self.assertFalse(reader_entries["secret"][3] & indexed.MODE_SYMLINK)

    def test_root_marker_reroots_the_tree(self):
        reader = self.pack([
            ("slice/README", "file", b"outer"),
            ("slice/arcadia/.arcadia.root", "file", b""),
            ("slice/arcadia/build/ymake_conf.py", "file", b"print()"),
        ], root_marker=".arcadia.root")
        self.assertEqual(reader.tree(), {
            ".arcadia.root": b"",
            "build/": None,
            "build/ymake_conf.py": b"print()",
        })


if __name__ == "__main__":
    unittest.main()
