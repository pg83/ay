import importlib.machinery
import importlib.util
import io
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path


def load_acceptance():
    path = Path(__file__).resolve().parent.parent / "acceptance"
    name = "ay_acceptance_under_test"
    loader = importlib.machinery.SourceFileLoader(name, str(path))
    spec = importlib.util.spec_from_loader(name, loader)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    loader.exec_module(module)
    return module


acceptance = load_acceptance()


class AcceptanceTest(unittest.TestCase):
    def test_run_validate_reads_build_summary(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            repo = root / "repo"
            repo.mkdir()
            (repo / "build.py").write_text("# marker\n")
            build = repo / "build"
            build.write_text(
                "#!/usr/bin/env python3\n"
                "import json, pathlib, sys\n"
                "build_root = pathlib.Path(sys.argv[sys.argv.index('-B') + 1])\n"
                "out = build_root / 'validation' / 'summary.json'\n"
                "out.parent.mkdir(parents=True, exist_ok=True)\n"
                "out.write_text(json.dumps({'schema': 1, 'cases': {'case': {'status': 'OK'}}}))\n"
            )
            build.chmod(0o755)
            result = acceptance.run_validate(
                "SIDE",
                str(repo),
                str(root / "out"),
                str(root / "cache"),
            )
        self.assertTrue(result.completed)
        self.assertTrue(result.structured)
        self.assertEqual(result.summary["cases"]["case"]["status"], "OK")

    def test_structured_summary_becomes_policy_metrics(self):
        result = acceptance.RunResult(
            "NEW",
            True,
            returncode=0,
            structured=True,
            summary={
                "schema": 1,
                "cases": {
                    "exact": {"status": "OK"},
                    "known": {
                        "status": "XFAIL",
                        "parity": {"matched": 10, "our_only": 2, "ref_only": 3},
                    },
                },
            },
        )
        status, matched, onlies = acceptance.result_metrics(result)
        self.assertEqual(status, {"exact": "OK", "known": "XFAIL"})
        self.assertEqual(matched, {"known": 10})
        self.assertEqual(onlies, {"known": (2, 3)})
        self.assertEqual(acceptance.usability_reason(result, status), "")

    def test_policy_rejects_structured_parity_regression(self):
        with redirect_stdout(io.StringIO()):
            accepted, reasons = acceptance.decide(
                {"known": "XFAIL"}, {"known": 10}, {"known": (1, 1)},
                {"known": "XFAIL"}, {"known": 9}, {"known": (2, 1)},
            )
        self.assertFalse(accepted)
        self.assertEqual(reasons, ["known parity matched dropped 10 -> 9"])

    def test_legacy_text_parser_remains_available(self):
        status, matched, onlies = acceptance.parse(
            "[old] exact normalized-node parity: matched=7 our_only=2 ref_only=3 our_total=9 ref_total=10\n"
            "[old] XFAIL (not gating)\n"
        )
        self.assertEqual(status, {"old": "XFAIL"})
        self.assertEqual(matched, {"old": 7})
        self.assertEqual(onlies, {"old": (2, 3)})


if __name__ == "__main__":
    unittest.main()
