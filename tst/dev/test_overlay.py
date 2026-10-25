import concurrent.futures
import json
import os
from pathlib import Path
import selectors
import signal
import subprocess
import sys
import tempfile
import unittest

from tst import lib


class OverlayTest(unittest.TestCase):
    def setUp(self):
        work = tempfile.TemporaryDirectory()
        self.addCleanup(work.cleanup)
        self.work = Path(work.name)
        self.root = self.work / "source"
        self.root.mkdir()
        self.env = {**os.environ, "TMPDIR": str(self.work)}

    def run_overlay(self, script, *args):
        return subprocess.run(
            [str(lib.AY), "dev", "overlay", str(self.root), "--", sys.executable, "-c", script, *args],
            cwd=self.root, env=self.env, text=True, capture_output=True, timeout=15,
        )

    def test_parallel_copy_on_write_and_identity(self):
        (self.root / "shared").write_text("original")
        (self.root / "removed").write_text("keep")
        barrier = self.work / "barrier"
        barrier.mkdir()
        script = """
import json, os, pathlib, sys, time
root = pathlib.Path.cwd()
worker, barrier = sys.argv[1], pathlib.Path(sys.argv[2])
(root / 'shared').write_text(worker)
(root / 'removed').unlink()
(root / 'created').write_text(worker)
(barrier / worker).touch()
deadline = time.monotonic() + 5
while len(list(barrier.iterdir())) != 2:
    assert time.monotonic() < deadline, 'workers did not overlap'
    time.sleep(.01)
assert (root / 'shared').read_text() == worker
assert (root / 'created').read_text() == worker
assert not (root / 'removed').exists()
caps = dict(line.split(':', 1) for line in pathlib.Path('/proc/self/status').read_text().splitlines() if ':' in line)
print(json.dumps([os.getuid(), os.getgid(), os.readlink('/proc/self/ns/mnt'), int(caps['CapAmb'], 16)]))
"""
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
            results = list(pool.map(lambda worker: self.run_overlay(script, worker, str(barrier)), ("one", "two")))
        namespaces = set()
        for result in results:
            self.assertEqual(result.returncode, 0, result.stderr)
            uid, gid, namespace, ambient = json.loads(result.stdout)
            self.assertEqual((uid, gid), (os.getuid(), os.getgid()))
            self.assertEqual(ambient, 0)
            self.assertNotEqual(namespace, os.readlink("/proc/self/ns/mnt"))
            namespaces.add(namespace)
        self.assertEqual(len(namespaces), 2)
        self.assertEqual((self.root / "shared").read_text(), "original")
        self.assertEqual((self.root / "removed").read_text(), "keep")
        self.assertFalse((self.root / "created").exists())
        self.assertEqual(list(self.work.glob("ay-overlay-*")), [])

    def test_exit_code_and_missing_command(self):
        result = self.run_overlay("raise SystemExit(17)")
        self.assertEqual(result.returncode, 17, result.stderr)
        result = lib.run_process("dev", "overlay", self.root, "--", "/missing/ay-overlay-command", env=self.env)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no such file", result.stderr)
        self.assertEqual(list(self.work.glob("ay-overlay-*")), [])

    def test_signal_exit_and_forwarding(self):
        result = self.run_overlay("import os, signal; os.kill(os.getpid(), signal.SIGTERM)")
        self.assertEqual(result.returncode, 128 + signal.SIGTERM, result.stderr)
        script = "import signal,sys,time; signal.signal(signal.SIGTERM, lambda *args: sys.exit(23)); print('ready',flush=True); time.sleep(60)"
        with subprocess.Popen(
            [str(lib.AY), "dev", "overlay", str(self.root), "--", sys.executable, "-c", script],
            env=self.env, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        ) as proc:
            try:
                with selectors.DefaultSelector() as selector:
                    selector.register(proc.stdout, selectors.EVENT_READ)
                    self.assertTrue(selector.select(5), "child did not start")
                self.assertEqual(proc.stdout.readline(), "ready\n")
                proc.terminate()
                stdout, stderr = proc.communicate(timeout=5)
                self.assertEqual(proc.returncode, 23, (stdout, stderr))
            finally:
                if proc.poll() is None:
                    proc.kill()
        self.assertEqual(list(self.work.glob("ay-overlay-*")), [])

    def test_invalid_arguments_and_child_namespace_guard(self):
        for args in ([], [str(self.root)], [str(self.root), "--"], [str(self.root), "bad", "id"]):
            result = lib.run_process("dev", "overlay", *args, env=self.env)
            self.assertEqual(result.returncode, 2)
            self.assertIn("usage:", result.stderr)
        regular = self.work / "file"
        regular.touch()
        result = lib.run_process("dev", "overlay", regular, "--", "true", env=self.env)
        self.assertIn("must be a directory", result.stderr)
        comma = self.work / "comma,directory"
        comma.mkdir()
        result = lib.run_process("dev", "overlay", comma, "--", "true", env=self.env)
        self.assertIn("must not contain", result.stderr)
        result = lib.run_process("dev", "overlay", self.root, "--", "true", env={
            **self.env, "AY_OVERLAY_DIR": str(self.work),
            "AY_OVERLAY_PARENT_NS": os.readlink("/proc/self/ns/mnt"),
        })
        self.assertIn("refusing to mount", result.stderr)


if __name__ == "__main__":
    unittest.main()
