import json
import os
import tempfile
import unittest
from pathlib import Path

from tst import lib
from tst.make.test_link_test_nodes import library


class TestDataReadsTest(unittest.TestCase):
    def test_cached_data_read_errors_omit_only_the_affected_suites(self):
        with tempfile.TemporaryDirectory(prefix="ay-data-reads-") as directory:
            root = Path(directory)
            lib.seed_root(root)
            (root / "ya.conf").write_text('[flags]\nOPENSOURCE = "yes"\n')
            for name, data in (
                ("spruce", "orchard"),
                ("willow", "orchard"),
                ("birch", "other"),
            ):
                project = root / name
                project.mkdir()
                (project / "ya.make").write_text(
                    f"EXECTEST()\nRUN(echo)\nDATA(arcadia/{data})\nEND()\n"
                )
            (root / "orchard").mkdir()
            blocked = root / "orchard" / "seed.bin"
            blocked.write_bytes(bytes(4902))
            (root / "orchard" / "tail.txt").write_text("after the unreadable file\n")
            (root / "other").mkdir()
            (root / "other" / "seed.bin").write_text("readable\n")

            def suites(*args):
                result = lib.run(
                    "make", "-G", "-j0", "--sandboxing", "-k", "-t", "--source-root", root,
                    "--target-platform", "default-linux-aarch64",
                    "--host-platform", "default-linux-x86_64",
                    *args, "spruce", "willow", "birch",
                )
                graph = json.loads(result.stdout)
                return {
                    output.split("/")[1]
                    for node in graph["graph"] for output in node["outputs"]
                    if output.endswith("/test-results/exectest/meta.json")
                }

            self.assertEqual(suites("--cache-tests"), {"spruce", "willow", "birch"})
            blocked.chmod(0)
            if os.access(blocked, os.R_OK):
                self.skipTest("the current user can read mode 000 files")
            self.assertEqual(suites("--cache-tests"), {"birch"})
            self.assertEqual(suites(), {"spruce", "willow", "birch"})
            self.assertEqual(suites("--cache-tests", "--test-tag", "include"), set())
            for flags in ([], ["--ignore-nodes-exit-code"]):
                with self.subTest(flags=flags):
                    failed = lib.run_process(
                        "make", "-G", "-j0", "--sandboxing", "-t", "--cache-tests",
                        "--source-root", root, "--target-platform", "default-linux-aarch64",
                        "--host-platform", "default-linux-x86_64", *flags, "spruce",
                    )
                    self.assertEqual(failed.returncode, 1)
                    self.assertEqual(failed.stdout, "")
                    self.assertIn("suite configuration failed for spruce:", failed.stderr)

    def test_imprint_file_eligibility_and_overlapping_data_roots(self):
        with tempfile.TemporaryDirectory(prefix="ay-data-imprint-") as directory:
            root = Path(directory)
            lib.seed_root(root)
            (root / "ya.conf").write_text('[flags]\nOPENSOURCE = "yes"\n')
            for name, data in (
                ("first", "arcadia/garden/nested"),
                ("second", "arcadia/garden arcadia/missing sbr://123=resource"),
                ("third", "arcadia/leaf-link arcadia/broken-link"),
                ("fourth", "arcadia/directory-link"),
            ):
                project = root / name
                project.mkdir()
                (project / "ya.make").write_text(
                    f"EXECTEST()\nRUN(echo)\nDATA({data})\nEND()\n"
                )
            nested = root / "garden" / "nested"
            nested.mkdir(parents=True)
            (nested / "leaf.txt").write_bytes(bytes(2 * 1024 * 1024 + 1))
            for excluded in (".svn", ".cache", ".idea", "unsearchable"):
                path = root / "garden" / excluded
                path.mkdir()
                (path / "blocked.txt").write_text("not hashed\n")
                (path / "blocked.txt").chmod(0)
                if excluded == "unsearchable":
                    path.chmod(0)
            (root / "garden" / "bytecode.pyc").touch(mode=0)
            (root / "garden" / "symlink").symlink_to(".cache/blocked.txt")
            (root / "garden" / "directory-symlink").symlink_to(".cache", target_is_directory=True)
            (root / "leaf-link").symlink_to("garden/.cache/blocked.txt")
            (root / "broken-link").symlink_to("missing")
            (root / "directory-link").symlink_to("garden/nested", target_is_directory=True)
            if hasattr(os, "mkfifo"):
                os.mkfifo(root / "garden" / "pipe")
            result = lib.run(
                "make", "-G", "-j0", "--sandboxing", "-t", "--cache-tests",
                "--source-root", root, "--target-platform", "default-linux-aarch64",
                "--host-platform", "default-linux-x86_64", "first", "second", "third", "fourth",
            )
            self.assertEqual(result.stderr, "")
            graph = json.loads(result.stdout)
            for project in ("first", "second", "third", "fourth"):
                lib.node_by_output(graph, f"$(B)/{project}/test-results/exectest/meta.json")

    def test_direct_data_file_and_hidden_directory_are_hashed(self):
        with tempfile.TemporaryDirectory(prefix="ay-data-hidden-") as directory:
            root = Path(directory)
            lib.seed_root(root)
            (root / "ya.conf").write_text('[flags]\nOPENSOURCE = "yes"\n')
            (root / "project").mkdir()
            (root / "project" / "ya.make").write_text(
                "EXECTEST()\nRUN(echo)\nDATA(arcadia/.keep/leaf.txt)\nEND()\n"
            )
            (root / ".keep").mkdir()
            blocked = root / ".keep" / "leaf.txt"
            blocked.write_text("unreadable\n")
            blocked.chmod(0)
            if os.access(blocked, os.R_OK):
                self.skipTest("the current user can read mode 000 files")
            for data in (".keep/leaf.txt", ".keep", "directory-link"):
                with self.subTest(data=data):
                    if data == "directory-link":
                        (root / data).symlink_to(".keep", target_is_directory=True)
                    (root / "project" / "ya.make").write_text(
                        f"EXECTEST()\nRUN(echo)\nDATA(arcadia/{data})\nEND()\n"
                    )
                    result = lib.run(
                        "make", "-G", "-j0", "--sandboxing", "-k", "-t", "--cache-tests",
                        "--source-root", root, "--target-platform", "default-linux-aarch64",
                        "--host-platform", "default-linux-x86_64", "project",
                    )
                    self.assertIn("suite configuration failed for project:", result.stderr)
                    graph = json.loads(result.stdout)
                    self.assertFalse(any(
                        "test-results" in output
                        for node in graph["graph"] for output in node["outputs"]
                    ))

    def test_interrupted_reads_retry_and_io_errors_omit_suites(self):
        files = {
            "cedar/ya.make": "EXECTEST()\nRUN(echo)\nDATA(arcadia/orchard)\nEND()\n",
            "orchard/leaf.txt": "content that is read while computing the cached test UID\n",
        }
        baseline = lib.make(files, "cedar", "-t", "--cache-tests")
        self.assertEqual(
            lib.make(files, "cedar", "-t", "--cache-tests", env={"AY_CHAOS": "eintr-test-data=0"}),
            baseline,
        )
        result = lib.make_process(
            files, "cedar", "-t", "--cache-tests", "-k", env={"AY_CHAOS": "test-data-read-eio=0"},
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("suite configuration failed for cedar:", result.stderr)
        self.assertIn("input/output error", result.stderr)
        graph = json.loads(result.stdout)
        self.assertFalse(any(
            "test-results" in output for node in graph["graph"] for output in node["outputs"]
        ))

    def test_python_suite_failure_preserves_binary_and_source_producers(self):
        files = {
            "cedar/ya.make": (
                f"PY3TEST()\n{lib.BARE_MODULE}"
                "NO_CHECK_IMPORTS()\nNO_IMPORT_TRACING()\nNO_PYTHON_INCLUDES()\n"
                "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\n"
                "TEST_SRCS(test_leaf.py)\nDATA(arcadia/orchard)\nEND()\n"
            ),
            "cedar/test_leaf.py": "def test_leaf(): pass\n",
            "orchard/seed.txt": "data\n",
        }
        for peer in ("library/python/pytest", "library/python/runtime_py3/main", "library/cpp/resource"):
            files[f"{peer}/ya.make"] = library()
        for tool in ("tools/archiver", "tools/py3cc", "tools/py3cc/slow", "tools/rescompiler", "tools/rescompressor"):
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        args = ("-t", "--cache-tests", "--dist", "--output-only-tests", "--add-modules-to-results", "-k")
        env = {"YA_STRIP_IDLE_BUILD_RESULTS": "0", "YA_STRIP_PACKAGES_FROM_RESULTS": "1"}
        healthy = lib.make(files, "cedar", *args, env=env)
        blocked = lib.make(files, "cedar", *args, env={**env, "AY_CHAOS": "test-data-read-eio=0"})
        lib.node_by_output(healthy, "$(B)/cedar/test-results/py3test/meta.json")
        binary = lib.node_by_output(healthy, "$(B)/cedar/cedar")
        self.assertEqual(lib.node_by_output(blocked, "$(B)/cedar/cedar"), binary)
        for graph in (healthy, blocked):
            self.assertIn(lib.node_by_output(graph, "$(B)/cedar/cedar")["uid"], graph["result"])
            lib.node_by_output(graph, "$(B)/cedar/test-results/flake8/meta.json")
        self.assertFalse(any(
            "/test-results/py3test/" in output for node in blocked["graph"] for output in node["outputs"]
        ))
        def producers(graph):
            return {
                tuple(node["outputs"]): node for node in graph["graph"]
                if node.get("kv", {}).get("p") != "TS" and "$(B)/common_test.context" not in node["outputs"]
            }
        self.assertEqual(producers(blocked), producers(healthy))


if __name__ == "__main__":
    unittest.main(verbosity=2)
