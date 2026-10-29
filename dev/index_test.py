import io
import os
import struct
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path

INDEX = os.environ["AY_INDEX_BINARY"]
MAGIC = b"AYIDX\x00\x00\x01"
HEADER = 64
ENTRY = struct.Struct("<IIIIQQ")
KIND_FILE, KIND_DIR = 1, 2
MODE_SYMLINK = 1 << 16
ALIGN = 4096


class IndexedReader:
    def __init__(self, path: Path):
        self.raw = path.read_bytes()
        assert self.raw[:8] == MAGIC
        count, names_offset, names_size, data_offset, data_size = struct.unpack_from("<QQQQQ", self.raw, 8)
        self.entries = [ENTRY.unpack_from(self.raw, HEADER + i * ENTRY.size) for i in range(count)]
        self.names = self.raw[names_offset:names_offset + names_size]
        self.data = self.raw[data_offset:data_offset + data_size]
        assert data_offset % ALIGN == 0

    def name(self, i):
        offset, length = self.entries[i][:2]
        return self.names[offset:offset + length].decode()

    def children(self, i):
        _, _, kind, _, first, count = self.entries[i]
        assert kind == KIND_DIR
        return {self.name(j): j for j in range(first, first + count)}

    def lookup(self, path):
        i = 0
        for part in filter(None, path.split("/")):
            i = self.children(i)[part]
        return i

    def read(self, path):
        _, _, kind, _, offset, size = self.entries[self.lookup(path)]
        assert kind == KIND_FILE
        return self.data[offset:offset + size]

    def tree(self, i=0, prefix=""):
        out = {}
        for name, j in self.children(i).items():
            path = prefix + name
            if self.entries[j][2] == KIND_DIR:
                out[path + "/"] = None
                out.update(self.tree(j, path + "/"))
            else:
                out[path] = self.read(path)
        return out


class IndexedTest(unittest.TestCase):
    def pack(self, members, root_marker=None):
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(directory))
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as stream:
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
        args = [INDEX, *(["--root-marker", root_marker] if root_marker else [])]
        with output.open("wb") as stdout:
            subprocess.run(args, input=archive.getvalue(), stdout=stdout, stderr=subprocess.PIPE, check=True,
                           env={**os.environ, "TMPDIR": str(directory)})
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

    def test_modes_follow_unpacking(self):
        reader = self.pack([
            ("d/secret", "file", b"x", 0o000),
            ("d/link", "sym", "secret"),
        ])
        reader_entries = {reader.name(i): reader.entries[i] for i in range(len(reader.entries))}
        self.assertEqual(reader_entries["d"][3], 0o755)
        self.assertEqual(reader_entries["secret"][3], 0o000)
        self.assertTrue(reader_entries["link"][3] & MODE_SYMLINK)
        self.assertFalse(reader_entries["secret"][3] & MODE_SYMLINK)

    def test_missing_root_marker_fails(self):
        with self.assertRaises(subprocess.CalledProcessError):
            self.pack([("README", "file", b"x")], root_marker=".arcadia.root")

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
