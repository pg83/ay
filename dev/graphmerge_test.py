import os
import subprocess
import tempfile
import unittest
from pathlib import Path

GRAPHMERGE = os.environ["AY_GRAPHMERGE_BINARY"]


class GraphMergeTest(unittest.TestCase):
    def merge(self, *contents):
        directory = Path(tempfile.mkdtemp())
        self.addCleanup(lambda: __import__("shutil").rmtree(directory))
        paths = []
        for index, content in enumerate(contents):
            path = directory / f"{index}.jsonl"
            path.write_text(content)
            paths.append(str(path))
        return subprocess.run([GRAPHMERGE, *paths], capture_output=True, text=True)

    def test_sorted_union_without_duplicates(self):
        result = self.merge("a\nc\nc\n", "b\nc\nd\n", "", "a\ne\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "a\nb\nc\nd\ne\n")

    def test_last_line_without_newline(self):
        result = self.merge("a\nb", "b\n")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout, "a\nb\n")

    def test_unsorted_input_fails(self):
        result = self.merge("a\n", "c\nb\n")
        self.assertEqual(result.returncode, 1)
        self.assertIn("input is not sorted", result.stderr)


if __name__ == "__main__":
    unittest.main()
