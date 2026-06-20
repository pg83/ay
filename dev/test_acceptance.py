#!/usr/bin/env python3
"""Regression tests for the ./acceptance merge-gate orchestrator.

These pin the robustness contract from T-51: every acceptance rejection must
explain which side failed and why (launch error, nonzero/crashed validate.py
exit, timeout, worker exception, missing result), with the diagnostic on
STDOUT (the merger captures acceptance stdout only). Normal ACCEPT and normal
policy-REJECT paths must not regress.

Run: python3 dev/test_acceptance.py
"""
import importlib.machinery
import importlib.util
import os
import subprocess
import sys
import tempfile
import unittest

SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
REPO_ROOT = os.path.dirname(SCRIPT_DIR)
ACCEPTANCE = os.path.join(REPO_ROOT, "acceptance")


def _load_acceptance():
    # `acceptance` has no .py extension, so give importlib an explicit source loader.
    loader = importlib.machinery.SourceFileLoader("acceptance_mod", ACCEPTANCE)
    spec = importlib.util.spec_from_loader("acceptance_mod", loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    return mod


def _make_repo(root, body):
    """Create a fake repo at root with dev/validate.py whose source is `body`."""
    dev = os.path.join(root, "dev")
    os.makedirs(dev, exist_ok=True)
    with open(os.path.join(dev, "validate.py"), "w", encoding="utf-8") as f:
        f.write(body)


def _run_acceptance(old_repo, new_repo, env=None):
    """Run ./acceptance OLD NEW as a subprocess, capturing stdout and stderr
    SEPARATELY (the merger sees stdout only). Returns (returncode, stdout, stderr)."""
    proc = subprocess.run(
        [sys.executable, ACCEPTANCE, old_repo, new_repo],
        capture_output=True,
        text=True,
        env=env,
    )
    return proc.returncode, proc.stdout, proc.stderr


PASS_OK = "import sys\nprint('[sg2] OK')\n"
PASS_OK_TWO = "import sys\nprint('[sg2] OK')\nprint('[sg3] OK')\n"
FAIL_SG2 = "import sys\nprint('[sg2] FAIL')\nsys.exit(1)\n"
CRASH_NO_OUTPUT = "import sys\nsys.exit(7)\n"
# Emits one OK case, then raises — interpreter prints a traceback and exits 1,
# exactly like a partial validate.py run that dies mid-stream after some cases.
PARTIAL_EXCEPTION = (
    "import sys\nprint('[sg2] OK', flush=True)\n"
    "raise RuntimeError('boom generating sg3')\n"
)
ALL_SKIP = "print('[sg2] SKIP (data not present on host: x)')\n"


class AcceptanceRobustnessTest(unittest.TestCase):
    def setUp(self):
        # /tmp is read-only on this host; keep scratch under the repo.
        self.tmp = tempfile.mkdtemp(prefix="t51-", dir=os.path.join(REPO_ROOT, ".tmp")
                                    if os.path.isdir(os.path.join(REPO_ROOT, ".tmp"))
                                    else REPO_ROOT)
        self.old = os.path.join(self.tmp, "old")
        self.new = os.path.join(self.tmp, "new")
        self.env = dict(os.environ, TMPDIR=self.tmp)

    def tearDown(self):
        import shutil
        shutil.rmtree(self.tmp, ignore_errors=True)

    def test_crashed_new_validator_rejects_with_stdout_reason(self):
        """A NEW validator that crashes (exit 7) emitting no case lines must
        REJECT (not silently ACCEPT), and the reason must be on STDOUT."""
        _make_repo(self.old, PASS_OK)
        _make_repo(self.new, CRASH_NO_OUTPUT)
        rc, out, err = _run_acceptance(self.old, self.new, self.env)
        self.assertNotEqual(rc, 0, f"crashed NEW validator must not ACCEPT\nstdout:\n{out}")
        self.assertIn("REJECT", out)
        self.assertIn("NEW", out)
        self.assertIn("7", out, "exit code of the crashed validator should be surfaced")
        # The diagnostic must be richer than a bare header + banners.
        self.assertNotIn("VERDICT: ACCEPT", out)

    def test_partial_exception_after_case_rejects_with_traceback(self):
        """NEW prints [sg2] OK then raises (exit 1 with a Python traceback),
        while OLD passes [sg2] and [sg3]. validate.py exit 1 is overloaded
        (normal gating FAIL *and* unhandled exception); the partial run must
        REJECT as a validator crash — NOT silently ACCEPT by comparing only the
        single parsed common case (sg2)."""
        _make_repo(self.old, PASS_OK_TWO)
        _make_repo(self.new, PARTIAL_EXCEPTION)
        rc, out, err = _run_acceptance(self.old, self.new, self.env)
        self.assertNotEqual(rc, 0, f"partial-exception NEW must not ACCEPT\nstdout:\n{out}")
        self.assertNotIn("VERDICT: ACCEPT", out)
        self.assertIn("REJECT", out)
        self.assertIn("NEW", out)
        # The crash context (the exception) must reach the merger via stdout.
        self.assertIn("RuntimeError", out, f"traceback context must be surfaced\nstdout:\n{out}")

    def test_launch_failure_recorded_not_lost_to_stderr(self):
        """A subprocess launch failure (Popen raising) must be captured into a
        failed RunResult with the exception text — never escape to stderr only."""
        mod = _load_acceptance()
        _make_repo(self.new, PASS_OK)

        import subprocess as _sp
        orig_popen = _sp.Popen

        def boom(*a, **k):
            raise OSError("simulated launch failure: too many open files")

        _sp.Popen = boom
        try:
            res = mod.run_validate("NEW", self.new, os.path.join(self.tmp, "o"))
        finally:
            _sp.Popen = orig_popen
        self.assertFalse(res.completed)
        self.assertIn("simulated launch failure", res.error)

    def test_stream_read_failure_after_case_line_marks_side_unusable(self):
        """validate.py emits [sg2] OK, then the stdout pipe raises mid-stream
        (e.g. OSError on read). Even though a case line parsed and the child
        would exit 0/1, the truncated transcript is UNUSABLE: run_validate must
        record a NEW-named side error carrying the stream exception text, so the
        gate REJECTs instead of ACCEPTing on the surviving common case."""
        mod = _load_acceptance()
        _make_repo(self.new, PASS_OK)

        class _FakeStdout:
            def __init__(self):
                self._yielded = False

            def __iter__(self):
                return self

            def __next__(self):
                if not self._yielded:
                    self._yielded = True
                    return "[sg2] OK\n"
                raise OSError("simulated pipe read failure: input/output error")

        class _FakeProc:
            def __init__(self):
                self.pid = os.getpid()
                self.stdout = _FakeStdout()
                self.returncode = 0

            def wait(self):
                self.returncode = 0
                return 0

        orig_popen = mod.subprocess.Popen
        mod.subprocess.Popen = lambda *a, **k: _FakeProc()
        try:
            res = mod.run_validate("NEW", self.new, os.path.join(self.tmp, "o"))
        finally:
            mod.subprocess.Popen = orig_popen

        self.assertFalse(res.completed, "stream-truncated run must be UNUSABLE")
        self.assertEqual(res.label, "NEW")
        self.assertIn("simulated pipe read failure", res.error,
                      f"stream exception text must reach the side error\nerror: {res.error!r}")
        # usability_reason must agree even though [sg2] OK parsed and exit==0.
        status, _, _ = mod.parse(res.output)
        self.assertEqual(status.get("sg2"), "OK", "the partial case line did parse")
        self.assertTrue(mod.usability_reason(res, status),
                        "a stream-truncated side must be reported unusable, not ACCEPTed")

    def test_normal_success_accepts(self):
        """Both sides pass identically -> ACCEPT, exit 0 (no regression)."""
        _make_repo(self.old, PASS_OK)
        _make_repo(self.new, PASS_OK)
        rc, out, err = _run_acceptance(self.old, self.new, self.env)
        self.assertEqual(rc, 0, f"expected ACCEPT\nstdout:\n{out}\nstderr:\n{err}")
        self.assertIn("VERDICT: ACCEPT", out)

    def test_normal_validate_rejection_not_regressed(self):
        """OLD OK, NEW FAIL on a gating case -> policy REJECT with the reason."""
        _make_repo(self.old, PASS_OK)
        _make_repo(self.new, FAIL_SG2)
        rc, out, err = _run_acceptance(self.old, self.new, self.env)
        self.assertEqual(rc, 1, f"expected policy REJECT\nstdout:\n{out}")
        self.assertIn("REJECT", out)
        self.assertIn("new FAIL appeared", out)

    def test_all_skip_both_sides_accepts(self):
        """All-SKIP (no data on host) is a clean run -> ACCEPT (unchanged)."""
        _make_repo(self.old, ALL_SKIP)
        _make_repo(self.new, ALL_SKIP)
        rc, out, err = _run_acceptance(self.old, self.new, self.env)
        self.assertEqual(rc, 0, f"all-SKIP must stay ACCEPT\nstdout:\n{out}")
        self.assertIn("VERDICT: ACCEPT", out)

    def test_missing_validate_rejects_with_reason(self):
        """A NEW repo lacking dev/validate.py rejects with a named reason."""
        _make_repo(self.old, PASS_OK)
        os.makedirs(self.new, exist_ok=True)  # no dev/validate.py
        rc, out, err = _run_acceptance(self.old, self.new, self.env)
        self.assertEqual(rc, 1)
        self.assertIn("REJECT", out)
        self.assertIn("NEW", out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
