import functools
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile
import unittest
from compression import zstd

from tst.make import autocheck


PROJECT = "__ay_e2e__"
CANONDATA = Path(__file__).parent / "canondata"


@functools.cache
def nodejs_version():
    """The tree's default Node.js; fixtures must not pin a version the tree may drop."""
    erm = Path(os.environ["AY_E2E_ARCADIA"]) / "devtools/frontend_build_platform/erm/erm-packages.json"
    return json.loads(erm.read_text())["nodejs"]["default"]


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
        result = self.run_process(cmd, self.root, {"YA_TC": "no", "YA_NO_RESPAWN": "yes", **(env or {})})
        self.calls += 1
        (self.work / f"graph-{self.calls}.json").write_text(result.stdout)
        (self.work / f"graph-{self.calls}.stderr").write_text(result.stderr)
        return result

    def run_process(self, cmd, cwd, extra):
        env = os.environ.copy()
        for key in ("CC", "CXX", "CPP", "LD", "AR", "CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS"):
            env.pop(key, None)
        env.update({"PYTHONDONTWRITEBYTECODE": "1", **extra})
        proc = subprocess.Popen(
            cmd, cwd=cwd, env=env, start_new_session=True,
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
        return subprocess.CompletedProcess(cmd, proc.returncode, stdout, stderr)

    def autocheck_configure(self, *targets):
        """Generate the graph as an Autocheck graph-generation node does; return the configure output it consumes.

        Checks the node contract on the way: exit code 0 despite configure
        errors, every declared output written, ucompress streams decodable by
        contexts_difference and ygdiff, JSON profiles readable by ya.
        """
        self.calls += 1
        run = self.work / f"autocheck-{self.calls}"
        build_dir, output_dir, tmp_dir = run / "build", run / "result", run / "tmp"
        for directory in (build_dir, output_dir, tmp_dir):
            directory.mkdir(parents=True)
        cmd = autocheck.command(self.binary, list(targets) or [self.project_name], build_dir, output_dir, self.root)
        if self.backend == "ya":
            cmd.insert(cmd.index("make") + 1, "--no-locked")
        # A graph-generation node is never started by a coding agent. ya would
        # detect the agent running these tests, switch to its JSONL display and
        # skip --html-display.
        result = self.run_process(cmd, build_dir, {
            "YA_TC": "no", "YA_AUTO_DETECT_AGENT": "0", **autocheck.ENVIRONMENT, "TMPDIR": str(tmp_dir),
        })
        (run / "stdout").write_text(result.stdout)
        (run / "stderr").write_text(result.stderr)
        self.assertEqual(result.returncode, 0, f"{cmd!r}\n{result.stderr[-4000:]}")
        missing = [name for name in autocheck.OUTPUTS.values() if not (output_dir / name).is_file()]
        self.assertEqual(missing, [], f"graph generation did not write its declared outputs\n{result.stderr[-4000:]}")
        for name in ("context.profile.json", "context.stages.json"):
            json.loads((output_dir / name).read_text())
        autocheck.read_uc(output_dir / "generating.log.uc")
        zstd.decompress((output_dir / "generating.evlog.zst").read_bytes())
        graph = json.loads(autocheck.read_uc(output_dir / "graph.json.uc"))
        self.assertIn("graph", graph)
        context = json.loads(autocheck.read_uc(output_dir / "context.json.uc"))
        # ay's source root is a symlink farm inside the work directory, so it is replaced first.
        replacements = [(os.path.realpath(self.root), "$(SOURCE_ROOT)"), (str(self.root), "$(SOURCE_ROOT)"), (str(self.work), "$(WORK)")]
        return autocheck.configure_output(context, self.project_name, replacements)

    def assertConfigureLikeYa(self, name="", targets=()):
        """Compare the configure output Autocheck consumes with ya's canonical output for this check.

        Canonical outputs live in tst/make/canondata/<module>.json, keyed by
        class, test and name. Record them with
        `python3 -m tst.make --backend ya --canonize ...`; every other run of
        either backend compares against them.
        """
        output = self.autocheck_configure(*targets)
        key = f"{type(self).__name__}.{self._testMethodName}" + (f"/{name}" if name else "")
        path = CANONDATA / f"{type(self).__module__.rsplit('.', 1)[-1]}.json"
        canon = json.loads(path.read_text()) if path.exists() else {}
        if os.environ.get("AY_E2E_CANONIZE"):
            canon[key] = output
            path.parent.mkdir(exist_ok=True)
            path.write_text(json.dumps(canon, indent=2, sort_keys=True, ensure_ascii=False) + "\n")
            return
        self.assertIn(key, canon, f"no canonical ya output for {key}; record it with --backend ya --canonize")
        self.maxDiff = None
        self.assertEqual(output, canon[key])

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
