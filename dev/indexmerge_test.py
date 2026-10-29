import io
import os
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path

from index_test import MODE_SYMLINK, IndexedReader

INDEX = os.environ["AY_INDEX_BINARY"]
INDEXMERGE = os.environ["AY_INDEXMERGE_BINARY"]


class IndexMergeTest(unittest.TestCase):
    def setUp(self):
        self.directory = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(self.directory))
        self.count = 0

    def index(self, members):
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
                info.type = {"dir": tarfile.DIRTYPE, "sym": tarfile.SYMTYPE}[kind]
                info.linkname = value or ""
                stream.addfile(info)
        self.count += 1
        output = self.directory / f"{self.count}.indexed"
        with output.open("wb") as stdout:
            subprocess.run([INDEX], input=archive.getvalue(), stdout=stdout, check=True,
                           env={**os.environ, "TMPDIR": str(self.directory)})
        return output

    def merge(self, *inputs):
        output = self.directory / "merged.indexed"
        result = subprocess.run([INDEXMERGE, "--out", str(output), *map(str, inputs)], capture_output=True, text=True)
        return result, output

    def test_union_matches_index_of_the_union(self):
        first = [
            ("a/one.txt", "file", b"1"),
            ("a/shared.txt", "file", b"shared"),
            ("empty", "dir", None),
            ("b/x", "file", b"x"),
        ]
        second = [
            ("a/shared.txt", "file", b"shared"),
            ("a/two.txt", "file", b"22"),
            ("c/y", "file", b"yy"),
            ("other", "dir", None),
        ]
        result, merged = self.merge(self.index(first), self.index(second))
        self.assertEqual(result.returncode, 0, result.stderr)
        union = IndexedReader(self.index(first + second[1:]))
        reader = IndexedReader(merged)
        self.assertEqual(reader.tree(), union.tree())
        self.assertEqual(len(reader.data), len(b"1sharedx22yy"))

    def test_empty_directory_gains_children_from_another_input(self):
        first = self.index([("e", "dir", None), ("f", "dir", None), ("g/z", "file", b"z")])
        second = self.index([("f/inner.txt", "file", b"in")])
        result, merged = self.merge(first, second)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(IndexedReader(merged).tree(), {
            "e/": None, "f/": None, "f/inner.txt": b"in", "g/": None, "g/z": b"z",
        })

    def test_directory_links_stay_links(self):
        first = self.index([("real/a.txt", "file", b"a"), ("link", "sym", "real")])
        second = self.index([("real/b.txt", "file", b"bb"), ("link", "sym", "real")])
        result, merged = self.merge(first, second)
        self.assertEqual(result.returncode, 0, result.stderr)
        reader = IndexedReader(merged)
        self.assertEqual(reader.tree(), {
            "link/": None, "link/a.txt": b"a", "link/b.txt": b"bb",
            "real/": None, "real/a.txt": b"a", "real/b.txt": b"bb",
        })
        link = reader.entries[reader.lookup("link")]
        real = reader.entries[reader.lookup("real")]
        self.assertTrue(link[3] & MODE_SYMLINK)
        self.assertEqual(link[4:], real[4:])
        self.assertEqual(len(reader.data), len(b"abb"))

    def test_placeholders_yield_to_contents(self):
        placeholder = self.index([("a/ya.make", "file", b""), ("a/empty", "file", b"")])
        real = self.index([("a/ya.make", "file", b"LIBRARY()\n"), ("a/empty", "file", b"")])
        for inputs in ((placeholder, real), (real, placeholder)):
            result, merged = self.merge(*inputs)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(IndexedReader(merged).tree(), {
                "a/": None, "a/empty": b"", "a/ya.make": b"LIBRARY()\n",
            })

    def test_disagreeing_file_fails(self):
        result, _ = self.merge(self.index([("a.txt", "file", b"one")]), self.index([("a.txt", "file", b"three")]))
        self.assertEqual(result.returncode, 1)
        self.assertIn("a.txt disagrees with an earlier input", result.stderr)


if __name__ == "__main__":
    unittest.main()
