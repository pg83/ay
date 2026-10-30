import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import unittest


PROJECT = "__ay_e2e__"


class MakeTest(unittest.TestCase):
    project_name = PROJECT

    def setUp(self):
        self.backend = os.environ["AY_E2E_BACKEND"]
        arcadia = Path(os.environ["AY_E2E_ARCADIA"])
        self.binary = os.environ["AY_E2E_BINARY"]
        work = tempfile.TemporaryDirectory(dir=os.environ["AY_E2E_WORK_DIR"])
        self.addCleanup(work.cleanup)
        self.work = Path(work.name)
        if self.backend == "ay":
            self.root = self.work / "src"
            self.root.mkdir()
            for entry in arcadia.iterdir():
                if entry.name not in (self.project_name, ".arc"):
                    (self.root / entry.name).symlink_to(entry)
        else:
            self.root = arcadia
        self.project = self.root / self.project_name
        if self.backend == "ay":
            self.private_directory(Path(self.project_name).parent)
        # Never reuse or remove a pre-existing directory in the user's tree.
        self.project.mkdir()
        self.addCleanup(shutil.rmtree, self.project)
        self.calls = 0

    def private_directory(self, relative):
        current = self.root
        for part in Path(relative).parts:
            current = current / part
            if current.is_symlink():
                source = Path(os.readlink(current))
                current.unlink()
                current.mkdir()
                for entry in source.iterdir():
                    (current / entry.name).symlink_to(entry)
            else:
                current.mkdir(exist_ok=True)

    def write(self, path, text):
        path = self.project / path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def write_tree(self, path, text):
        """Write a tree file outside the fixture for this test only.

        ay sees the tree through a per-test symlink farm: every directory on
        the way is replaced by a directory of links, so nothing is written
        through to Arcadia. ya runs in a private overlay; the file is restored
        when the test ends.
        """
        target = self.root / path
        if self.backend == "ay":
            self.private_directory(Path(path).parent)
            if target.is_symlink():
                target.unlink()
            target.write_text(text)
            return
        created = [parent for parent in target.parents if not parent.exists()]
        original = target.read_bytes() if target.exists() else None

        def restore():
            if original is None:
                target.unlink()
            else:
                target.write_bytes(original)
            for directory in created:
                directory.rmdir()

        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text)
        self.addCleanup(restore)

    def make(self, *flags, dump_graph=True, env=None, sandboxing=True):
        cmd = [self.binary, "make", "-j0"]
        if sandboxing:
            cmd.append("--sandboxing")
        if dump_graph:
            cmd.append("-G")
        if self.backend == "ya":
            cmd += ["--build-dir", str(self.work / "build"), "-xx", "--no-locked", "--no-prefetch", "--no-ymake-tool-servermode", "--no-ymake-pic-servermode", "--no-ymake-multiconfig"]
        cmd += [*flags, self.project_name]
        extra = env or {}
        env = os.environ.copy()
        for key in ("CC", "CXX", "CPP", "LD", "AR", "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS"):
            env.pop(key, None)
        env.update(YA_TC="no", YA_NO_RESPAWN="yes", PYTHONDONTWRITEBYTECODE="1", **extra)
        proc = subprocess.Popen(
            cmd, cwd=self.root, env=env, start_new_session=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        )
        try:
            stdout, stderr = proc.communicate(timeout=600)
        except BaseException:
            os.killpg(proc.pid, signal.SIGTERM)
            try:
                proc.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(proc.pid, signal.SIGKILL)
                proc.communicate()
            raise
        self.calls += 1
        (self.work / f"graph-{self.calls}.json").write_text(stdout)
        (self.work / f"graph-{self.calls}.stderr").write_text(stderr)
        return subprocess.CompletedProcess(cmd, proc.returncode, stdout, stderr)

    def graph(self, *flags, env=None):
        result = self.make(*flags, env=env)
        self.assertEqual(result.returncode, 0, f"{result.args!r}\n{result.stderr}")
        graph = json.loads(result.stdout)
        # The two CLIs spell source/build root placeholders differently.
        for node in graph["graph"]:
            for field in ("inputs", "outputs"):
                node[field] = [p.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for p in node.get(field, [])]
        self.assertTrue(graph["result"], "generation returned no roots")
        return graph

    def node(self, graph, output):
        expected = f"$(B)/{self.project_name}/{output}"
        matches = [node for node in graph["graph"] if expected in node["outputs"]]
        outputs = [path for node in graph["graph"] for path in node["outputs"] if f"/{self.project_name}/" in path]
        self.assertEqual(len(matches), 1, f"expected one producer of {expected}; fixture outputs: {outputs[:80]}")
        return matches[0]
