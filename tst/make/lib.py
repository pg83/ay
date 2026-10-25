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
                if entry.name != PROJECT:
                    (self.root / entry.name).symlink_to(entry)
        else:
            self.root = arcadia
        self.project = self.root / PROJECT
        # Never reuse or remove a pre-existing directory in the user's tree.
        self.project.mkdir()
        self.addCleanup(shutil.rmtree, self.project)
        self.calls = 0

    def write(self, path, text):
        path = self.project / path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)

    def graph(self, *flags):
        cmd = [self.binary, "make", "-j0", "-G", "--sandboxing"]
        if self.backend == "ya":
            cmd += ["-xx", "--no-ymake-tool-servermode", "--no-ymake-pic-servermode", "--no-ymake-multiconfig"]
        cmd += [*flags, PROJECT]
        env = os.environ.copy()
        for key in ("CC", "CXX", "CPP", "LD", "AR", "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS"):
            env.pop(key, None)
        env.update(YA_TC="no", YA_NO_RESPAWN="yes", PYTHONDONTWRITEBYTECODE="1")
        proc = subprocess.Popen(
            cmd, cwd=self.root, env=env, start_new_session=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
        )
        try:
            stdout, stderr = proc.communicate(timeout=90)
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
        self.assertEqual(proc.returncode, 0, f"{cmd!r}\n{stderr}")
        graph = json.loads(stdout)
        # The two CLIs spell source/build root placeholders differently.
        for node in graph["graph"]:
            for field in ("inputs", "outputs"):
                node[field] = [p.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for p in node.get(field, [])]
        self.assertTrue(graph["result"], "generation returned no roots")
        return graph

    def node(self, graph, output):
        expected = f"$(B)/{PROJECT}/{output}"
        matches = [node for node in graph["graph"] if expected in node["outputs"]]
        self.assertEqual(len(matches), 1, f"expected one producer of {expected}")
        return matches[0]
