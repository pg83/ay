import hashlib
import tarfile
import tempfile
import unittest
from pathlib import Path

from fetch_validation_resource import download
from provision import parse_xfail
from validate_case import CaseRunner
from validation_lib import make_summary, normalized_node_parity_counts, read_summary, write_json_atomic


FAKE_AY = r'''#!/usr/bin/env python3
import shutil
import sys
import tarfile
from pathlib import Path

args = sys.argv[1:]
if args[:2] == ["fetch", "sandbox"]:
    archive = Path(args[args.index("--resource-file") + 1])
    destination = Path(args[args.index("--untar-to") + 1])
    destination.mkdir(parents=True, exist_ok=True)
    with tarfile.open(archive, "r:*") as stream:
        stream.extractall(destination)
elif args and args[0] == "make":
    print('{"node":1}')
elif args[:3] == ["dev", "dump", "normalize"]:
    source = Path(args[args.index("--in") + 1])
    destination = Path(args[args.index("--out") + 1])
    shutil.copyfile(source, destination)
elif args[:3] == ["dev", "dump", "sort"]:
    source = Path(args[args.index("--in") + 1])
    destination = Path(args[args.index("--out") + 1])
    destination.write_text("".join(sorted(source.read_text().splitlines(keepends=True))))
else:
    raise SystemExit("unexpected fake ay invocation: " + repr(args))
'''


class ValidationTest(unittest.TestCase):
    def make_archive(self, path: Path, files: dict[str, str]) -> None:
        staging = path.parent / (path.name + ".contents")
        staging.mkdir()
        for name, content in files.items():
            destination = staging / name
            destination.parent.mkdir(parents=True, exist_ok=True)
            destination.write_text(content)
        with tarfile.open(path, "w") as stream:
            for item in sorted(staging.rglob("*")):
                stream.add(item, arcname=item.relative_to(staging))

    def run_case(self, reference: str, xfail=False):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        ay = root / "ay"
        ay.write_text(FAKE_AY)
        ay.chmod(0o755)
        source = root / "slice.tar"
        graph = root / "graph.tar"
        self.make_archive(source, {"repo/.arcadia.root": ""})
        self.make_archive(graph, {"graph.fuse.json": reference})
        spec = {
            "id": "synthetic",
            "target": "pkg/app",
            "command": ["ya", "make", "-G", "pkg/app"],
            "slice_url": "https://example/100",
            "graph_url": "https://example/200",
            "xfail": xfail,
        }
        output = root / "output"
        result = CaseRunner(ay, spec, output, 10).execute(source, graph)
        return result, output

    def test_parity_counts_and_sample(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            left = root / "left"
            right = root / "right"
            left.write_text("a\nb\nd\n")
            right.write_text("a\nc\nd\ne\n")
            parity, sample = normalized_node_parity_counts(left, right)
        self.assertEqual(parity, {
            "matched": 2,
            "our_only": 1,
            "ref_only": 2,
            "our_total": 3,
            "ref_total": 4,
        })
        self.assertEqual(sample, ["- b", "+ c", "+ e"])

    def test_case_runner_reports_exact_result(self):
        result, output = self.run_case('{"node":1}\n')
        self.assertEqual(result["status"], "OK")
        self.assertTrue(result["exact"])
        self.assertTrue((output / "commands.txt").is_file())

    def test_case_runner_reports_auto_xfail_with_parity(self):
        result, output = self.run_case('{"node":2}\n', xfail="auto")
        self.assertEqual(result["status"], "XFAIL")
        self.assertFalse(result["exact"])
        self.assertEqual(result["parity"]["matched"], 0)
        self.assertTrue((output / "diff-sample.txt").is_file())

    def test_resource_download_checks_content_hash(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source"
            output = root / "output"
            source.write_bytes(b"immutable")
            digest = hashlib.sha256(source.read_bytes()).hexdigest()
            download(source.as_uri(), output, digest, attempts=1)
            self.assertEqual(output.read_bytes(), b"immutable")

    def test_summary_is_structured_and_readable(self):
        results = [
            {"schema": 1, "case": "b", "target": "b", "status": "XFAIL", "exact": False},
            {"schema": 1, "case": "a", "target": "a", "status": "OK", "exact": True},
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "summary.json"
            write_json_atomic(path, make_summary(results))
            summary = read_summary(path)
        self.assertEqual(list(summary["cases"]), ["a", "b"])
        self.assertEqual(summary["counts"], {"FAIL": 0, "OK": 1, "XFAIL": 1})

    def test_provision_normalizes_xfail_values(self):
        self.assertIs(parse_xfail("false"), False)
        self.assertIs(parse_xfail("true"), True)
        self.assertEqual(parse_xfail("auto"), "auto")


if __name__ == "__main__":
    unittest.main()
