import tempfile
import unittest
from pathlib import Path

from tst import lib
from tst.make import test_exec_make as execution


class PackageResultsTest(unittest.TestCase):
    def test_execution_keeps_packages_required_by_a_surviving_result(self):
        files = {
            "drop/ya.make": "UNION()\nFILES(unused.txt)\nEND()\n",
            "drop/unused.txt": "unused\n",
            "keep/ya.make": "CI_GROUP()\nPEERDIR(needed)\nEND()\n",
            "needed/ya.make": "UNION()\nFILES(required.txt)\nEND()\n",
            "needed/required.txt": "required\n",
            "build/scripts/fs_tools.py": "import shutil,sys\nshutil.copyfile(sys.argv[2], sys.argv[3])\n",
        }
        workspace = execution.Workspace(self, files)
        workspace.make("-j", "0", "--strip-packages-from-results", targets=("keep", "drop"))
        process = workspace.make("-j", "1", "--strip-packages-from-results", targets=("keep", "drop"))
        self.assertIn(("UN", "$(B)/needed/needed.pkg.fake"), execution.built_kinds(process.stderr))
        self.assertTrue(workspace.meta_for("$(B)/needed/required.txt").is_file())
        self.assertFalse(any("$(B)/drop/unused.txt" in path.read_text() for path in workspace.uid_files()))
        unfiltered = workspace.make("-j", "1", targets=("keep", "drop"))
        self.assertIn(("UN", "$(B)/drop/drop.pkg.fake"), execution.built_kinds(unfiltered.stderr))
        self.assertTrue(workspace.meta_for("$(B)/drop/unused.txt").is_file())

    def test_execution_does_not_schedule_stripped_package_commands(self):
        with tempfile.TemporaryDirectory(prefix="ay-package-result-") as directory:
            build = Path(directory) / "build"
            files = {"pkg/ya.make": "UNION()\nFILES(payload.txt)\nEND()\n", "pkg/payload.txt": "payload\n"}
            process = lib.make_process(files, "pkg", "--sandboxing", "--strip-packages-from-results",
                "--jobs", "1", "--build-dir", str(build))
            self.assertEqual(process.returncode, 0, process.stderr)
            self.assertFalse((build / "pkg/payload.txt").exists())
            self.assertFalse((build / "pkg/pkg.pkg.fake").exists())

    def test_package_roots_are_removed_before_adding_surviving_modules(self):
        for module in ("UNION", "SDC_UNION", "PACKAGE"):
            for options, env in (
                (["--strip-packages-from-results"], {}),
                ([], {"YA_STRIP_PACKAGES_FROM_RESULTS": "1"}),
                ([], {"YA_STRIP_PACKAGES_FROM_RESULTS": "0"}),
                ([], {"YA_STRIP_PACKAGES_FROM_RESULTS": ""}),
            ):
                with self.subTest(module=module, options=options, env=env):
                    files = {
                        "pkg/ya.make": f"{module}()\nFILES(payload.txt)\nEND()\n",
                        "pkg/payload.txt": "payload\n",
                    }
                    graph = lib.make(files, "pkg", "--sandboxing", "--add-modules-to-results", *options, env=env)
                    self.assertEqual(graph["result"], [])
                    self.assertEqual(graph["graph"], [])

    def test_packages_needed_by_surviving_results_keep_their_dependencies(self):
        files = {
            "drop/ya.make": "UNION()\nFILES(unused.txt)\nEND()\n",
            "drop/unused.txt": "unused\n",
            "keep/ya.make": "CI_GROUP()\nPEERDIR(needed)\nEND()\n",
            "needed/ya.make": "PACKAGE()\nFILES(required.txt)\nEND()\n",
            "needed/required.txt": "required\n",
        }
        for targets in (("drop", "keep"), ("keep", "drop")):
            for extra in ([], ["--add-modules-to-results"]):
                with self.subTest(targets=targets, extra=extra):
                    graph = lib.make(files, *targets, "--sandboxing", "--strip-packages-from-results", *extra)
                    group = lib.node_by_output(graph, "$(B)/keep/keep.ci.pkg.fake")
                    package = lib.node_by_output(graph, "$(B)/needed/needed.final.pkg.fake")
                    source = lib.node_by_output(graph, "$(B)/needed/required.txt")
                    self.assertIn(package["uid"], group["deps"])
                    self.assertIn(source["uid"], package["deps"])
                    self.assertIn(group["uid"], graph["result"])
                    self.assertEqual(package["uid"] in graph["result"], bool(extra))
                    self.assertFalse(any(output.startswith("$(B)/drop/")
                        for node in graph["graph"] for output in node["outputs"]))


if __name__ == "__main__":
    unittest.main(verbosity=2)
