import hashlib
import json
import os
import tempfile
import unittest
from unittest import mock
from pathlib import Path

from fetch_validation_resource import download
from provision import OAUTH_TOKEN_RE, parse_trace, parse_xfail
from validate_case import CaseRunner
from validation_lib import (
    format_result_lines,
    make_summary,
    normalized_node_parity_counts,
    read_summary,
    write_json_atomic,
)


FAKE_AY = r'''#!/usr/bin/env python3
import json
import os
import shutil
import sys
from pathlib import Path

args = sys.argv[1:]
with (Path(__file__).parent / "calls.jsonl").open("a") as calls:
    calls.write(json.dumps({"args": args, "env": dict(os.environ)}) + "\n")
if args and args[0] == "make":
    print('{"node":1}')
elif args[:1] == ["normalize"]:
    source = Path(args[args.index("--in") + 1])
    destination = Path(args[args.index("--out") + 1])
    shutil.copyfile(source, destination)
elif args[:1] == ["sort"]:
    source = Path(args[args.index("--in") + 1])
    destination = Path(args[args.index("--out") + 1])
    destination.write_text("".join(sorted(source.read_text().splitlines(keepends=True))))
else:
    raise SystemExit("unexpected fake ay invocation: " + repr(args))
'''


class ValidationTest(unittest.TestCase):
    def run_case(self, reference: str, xfail=False, **extra):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        root = Path(temporary.name)
        ay = root / "ay"
        ay.write_text(FAKE_AY)
        ay.chmod(0o755)
        source = root / "slice.indexed"
        source.write_bytes(b"")
        graph = root / "reference.norm.jsonl"
        graph.write_text("".join(sorted(reference.splitlines(keepends=True))))
        spec = {
            "id": "synthetic",
            "target": "pkg/app",
            "command": ["ya", "make", "-G", "pkg/app"],
            "slice_url": "https://example/100",
            "graph_url": "https://example/200",
            "xfail": xfail,
            **extra,
        }
        output = root / "output"
        result = CaseRunner(ay, ay, spec, output, 10).execute(source, graph)
        return result, output

    def fake_ay_calls(self, output: Path) -> dict[str, list[dict]]:
        calls: dict[str, list[dict]] = {}
        for line in (output.parent / "calls.jsonl").read_text().splitlines():
            call = json.loads(line)
            calls.setdefault(call["args"][0], []).append(call)
        return calls

    def test_token_guard_ignores_uids_with_the_token_prefix(self):
        self.assertIsNone(OAUTH_TOKEN_RE.search(b'"self_uid": "FSJXp6E7uOy1__ABCDEFGH",'))
        self.assertIsNotNone(OAUTH_TOKEN_RE.search(b'"--token", "y1__AgAAAAABCDEFGHIJKLMNOPQRSTUV"'))

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
        self.assertEqual((output / "ours.only.jsonl").read_text(), "")
        self.assertEqual((output / "ref.only.jsonl").read_text(), "")

    def test_case_runner_retains_full_differences_beyond_the_sample(self):
        nodes = [json.dumps({"node": index, "payload": "x" * 1500}) + "\n" for index in range(50)]
        result, output = self.run_case("".join(nodes))
        self.assertEqual(result["parity"], {
            "matched": 0, "our_only": 1, "ref_only": 50, "our_total": 1, "ref_total": 50,
        })
        self.assertEqual((output / "ours.only.jsonl").read_text(), '{"node":1}\n')
        self.assertEqual((output / "ref.only.jsonl").read_text(), "".join(sorted(nodes)))
        sample = (output / "diff-sample.txt").read_text().splitlines()
        self.assertEqual(len(sample), 40)
        self.assertTrue(all(len(line) <= 1002 for line in sample))
        calls = self.fake_ay_calls(output)
        self.assertEqual(len(calls["make"]), 1)
        self.assertEqual(len(calls["normalize"]), 1)
        self.assertEqual(len(calls["sort"]), 1)

    def test_case_runner_passes_targets_to_generator_and_env(self):
        env = {"AUTOCHECK": "1", "YA_NO_GEN_RENAMED_RESULTS": "1"}
        targets = ["pkg/app", "pkg/lib"]
        with mock.patch.dict(os.environ, {"CC": "gcc"}):
            for name in env:
                os.environ.pop(name, None)
            result, output = self.run_case(
                '{"node":1}\n',
                target=targets,
                command=["ya", "make", "-G", *targets],
                env=env,
            )
        self.assertEqual(result["status"], "OK")
        self.assertEqual(result["target"], targets)
        calls = self.fake_ay_calls(output)
        self.assertEqual(sorted(calls), ["make", "normalize", "sort"])
        [make] = calls["make"]
        self.assertEqual(make["args"][:4], ["make", "-G", *targets])
        self.assertEqual({name: make["env"].get(name) for name in env}, env)
        self.assertNotIn("CC", make["env"])
        for call in calls["normalize"]:
            self.assertNotIn("--target", call["args"])
        for name in ("normalize", "sort"):
            for call in calls[name]:
                self.assertFalse(env.keys() & call["env"].keys(), call["args"])

    def test_case_runner_reports_auto_xfail_with_parity(self):
        result, output = self.run_case('{"node":2}\n', xfail="auto")
        self.assertEqual(result["status"], "XFAIL")
        self.assertFalse(result["exact"])
        self.assertEqual(result["parity"]["matched"], 0)
        self.assertTrue((output / "diff-sample.txt").is_file())

    def test_generation_failure_retains_xfail_policy(self):
        for policy, expected in ((False, "FAIL"), ("auto", "FAIL"), (True, "XFAIL")):
            with self.subTest(policy=policy):
                result, output = self.run_case(
                    '{"node":1}\n', xfail=policy,
                    command=["ya", "invalid-validation-command"],
                )
                self.assertEqual(result["status"], expected)
                self.assertEqual(result["phase"], "generate")
                self.assertEqual(result["xfail"], policy)
                self.assertFalse(result["exact"])
                self.assertNotEqual(result["returncode"], 0)
                self.assertNotIn("parity", result)
                self.assertNotIn("normalize", self.fake_ay_calls(output))

    def test_resource_download_checks_content_hash(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source"
            output = root / "output"
            source.write_bytes(b"immutable")
            digest = hashlib.sha256(source.read_bytes()).hexdigest()
            download(source.as_uri(), output, digest, attempts=1)
            self.assertEqual(output.read_bytes(), b"immutable")

    def test_resource_download_rejects_empty_body(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source"
            output = root / "output"
            source.write_bytes(b"")
            with self.assertRaisesRegex(ValueError, "empty download"):
                download(source.as_uri(), output, "", attempts=1)
            self.assertFalse(output.exists())

    def test_resource_download_checks_sandbox_md5(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            source = root / "source"
            output = root / "output"
            source.write_bytes(b"cut")
            sources = ([(source.as_uri(), False)], hashlib.md5(b"complete").hexdigest())
            with mock.patch("fetch_validation_resource.sandbox_sources", return_value=sources):
                with self.assertRaisesRegex(ValueError, "md5 mismatch"):
                    download(source.as_uri(), output, "", attempts=1)
            self.assertFalse(output.exists())

    def test_summary_is_structured_and_readable(self):
        results = [
            {"schema": 1, "case": "b", "target": ["b/x", "b/y"], "status": "XFAIL", "exact": False},
            {"schema": 1, "case": "a", "target": "a", "status": "OK", "exact": True},
        ]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "summary.json"
            write_json_atomic(path, make_summary(results))
            summary = read_summary(path)
        self.assertEqual(list(summary["cases"]), ["a", "b"])
        self.assertEqual(summary["counts"], {"FAIL": 0, "OK": 1, "XFAIL": 1})

    def test_text_report_surfaces_xfail_and_parity(self):
        lines = format_result_lines({
            "case": "known_gap",
            "status": "XFAIL",
            "detail": "normalized graphs differ",
            "parity": {
                "matched": 100,
                "our_only": 2,
                "ref_only": 3,
                "our_total": 102,
                "ref_total": 103,
            },
        })
        self.assertEqual(
            lines,
            [
                "[known_gap] exact normalized-node parity: matched=100 "
                "our_only=2 ref_only=3 our_total=102 ref_total=103",
                "[known_gap] XFAIL (not gating) — normalized graphs differ",
            ],
        )

    def test_provision_joins_stats_split_across_threads(self):
        with tempfile.TemporaryDirectory() as directory:
            trace = Path(directory) / "trace"
            trace.write_text(
                '11 lstat("/m/a/gotest/ya.make",  <unfinished ...>\n'
                '12 lstat("/m/b/ya.make",  <unfinished ...>\n'
                '11 <... lstat resumed>{st_mode=S_IFREG|0644, st_size=10, ...}) = 0\n'
                '12 <... lstat resumed>0x7ffd) = -1 ENOENT (No such file or directory)\n'
                '13 newfstatat(AT_FDCWD, "/m/c/x.h", {st_mode=S_IFREG|0644, st_size=1, ...}, 0) = 0\n'
            )
            _, _, exists, _ = parse_trace(str(trace), "/m")
        self.assertEqual(exists, {"a/gotest/ya.make", "c/x.h"})

    def test_provision_normalizes_xfail_values(self):
        self.assertIs(parse_xfail("false"), False)
        self.assertIs(parse_xfail("true"), True)
        self.assertEqual(parse_xfail("auto"), "auto")


if __name__ == "__main__":
    unittest.main()
