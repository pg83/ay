import io
import sys
import tarfile
import tempfile
import unittest
from pathlib import Path

import reference_graph


class ReferenceGraphTest(unittest.TestCase):
    def extract(self, files):
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(directory))
        archive = directory / "graph.tar.gz"
        with tarfile.open(archive, "w:gz") as stream:
            for name, content in files.items():
                info = tarfile.TarInfo(name)
                info.size = len(content)
                stream.addfile(info, io.BytesIO(content))
        output = directory / "graph.json"
        saved = sys.argv
        sys.argv = ["reference_graph.py", "--in", str(archive), "--out", str(output)]
        try:
            reference_graph.main()
        finally:
            sys.argv = saved
        return output.read_bytes()

    def test_prefers_graph_fuse_json(self):
        self.assertEqual(self.extract({"a.json": b"a", "graph.fuse.json": b"fuse"}), b"fuse")

    def test_falls_back_to_first_json_by_path(self):
        self.assertEqual(self.extract({"z/x.json": b"z", "b/y.json": b"b", "notes.txt": b"t"}), b"b")


if __name__ == "__main__":
    unittest.main()
