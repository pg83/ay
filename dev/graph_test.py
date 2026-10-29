import io
import os
import subprocess
import tarfile
import unittest

GRAPH = os.environ["AY_GRAPH_BINARY"]


class ReferenceGraphTest(unittest.TestCase):
    def extract(self, files):
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode="w") as stream:
            for name, content in files.items():
                info = tarfile.TarInfo(name)
                info.size = len(content)
                stream.addfile(info, io.BytesIO(content))
        return subprocess.run([GRAPH], input=archive.getvalue(), stdout=subprocess.PIPE, check=True).stdout

    def test_prefers_graph_fuse_json(self):
        self.assertEqual(self.extract({"a.json": b"a", "graph.fuse.json": b"fuse"}), b"fuse")

    def test_falls_back_to_first_json_by_path(self):
        self.assertEqual(self.extract({"z/x.json": b"z", "b/y.json": b"b", "notes.txt": b"t"}), b"b")

    def test_fuse_after_other_json_wins(self):
        self.assertEqual(self.extract({"a.json": b"a", "graph.fuse.json": b"fuse", "0.json": b"0"}), b"fuse")

    def test_archive_without_json_fails(self):
        with self.assertRaises(subprocess.CalledProcessError):
            self.extract({"notes.txt": b"t"})


if __name__ == "__main__":
    unittest.main()
