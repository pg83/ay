import os
import tempfile
import unittest
from pathlib import Path

import lib
from test_go_package import PROGRAM_PEERS, STD, between, go_module, go_tool_args, go_world


PACKAGE = "services/dispatch"
OUTPUT = "$(B)/" + PACKAGE + "/dispatch"
MISSING = "$(S)/" + PACKAGE + "/entry/start.go"


def missing_program():
    files = go_world()
    files[f"{PACKAGE}/ya.make"] = "GO_PROGRAM()\nSRCS(entry/start.go)\nEND()\n"
    return files


class GoMissingSourcesTest(unittest.TestCase):
    def test_absent_program_source_retains_module_and_default_dependencies(self):
        graph = lib.make(missing_program(), PACKAGE)
        program = lib.node_by_output(graph, OUTPUT)
        args = go_tool_args(program)
        self.assertEqual(between(args, "++srcs", "++asm-flags"), [MISSING])
        self.assertIn(MISSING, program["inputs"])
        self.assertIn(OUTPUT + ".vet.txt", program["outputs"])
        self.assertEqual(between(args, "++peers", "++non-local-peers"), PROGRAM_PEERS)
        for peer in PROGRAM_PEERS:
            node = lib.node_by_output(graph, "$(B)/" + peer)
            self.assertIn(node["uid"], program["deps"])
        self.assertIn("$(B)/" + PACKAGE + "/__vcs_version__.go", args)
        self.assertEqual(args[args.index("++std-lib-prefix") + 1], STD + "/")
        self.assertTrue(any("vcs_info.py" in arg for cmd in program["cmds"] for arg in cmd["cmd_args"]))
        self.assertIn(graph["graph"].index(program), graph["result"])

    def test_missing_and_readable_sources_keep_import_and_explicit_peers(self):
        files = missing_program()
        go_module(files, "dependencies/declared")
        go_module(files, "dependencies/imported")
        go_module(files, STD + "/fmt")
        files[f"{PACKAGE}/ya.make"] = (
            "GO_PROGRAM()\nSRCDIR(shared/source)\nPEERDIR(dependencies/declared)\n"
            "SRCS(entry/start.go readable.go ${ARCADIA_ROOT}/shared/absent.go)\nEND()\n"
        )
        files["shared/source/readable.go"] = (
            'package main\nimport ("fmt"; "a.yandex-team.ru/dependencies/imported")\n'
        )
        graph = lib.make(files, PACKAGE)
        program = lib.node_by_output(graph, OUTPUT)
        args = go_tool_args(program)
        self.assertEqual(between(args, "++srcs", "++asm-flags"), [
            MISSING, "$(S)/shared/source/readable.go", "$(S)/shared/absent.go",
        ])
        for source in (MISSING, "$(S)/shared/source/readable.go", "$(S)/shared/absent.go"):
            self.assertIn(source, program["inputs"])
        for peer in (*PROGRAM_PEERS, "dependencies/declared/declared.a",
                     STD + "/fmt/fmt.a", "dependencies/imported/imported.a"):
            self.assertIn(peer, args)
            self.assertIn(lib.node_by_output(graph, "$(B)/" + peer)["uid"], program["deps"])

    def test_absent_generated_source_uses_its_declared_producer(self):
        for declaration, name in (
            ("COPY_FILE(template.in emitted.go)", "emitted.go"),
            ("COPY_FILE(template.in emitted.go)", "${BINDIR}/emitted.go"),
            ("RUN_PYTHON3(generator.py OUT_NOAUTO emitted.go)", "emitted.go"),
        ):
            with self.subTest(declaration=declaration, source=name):
                files = missing_program()
                go_module(files, STD + "/fmt")
                files[f"{PACKAGE}/ya.make"] = (
                    f"GO_PROGRAM()\n{declaration}\nSRCS({name})\nEND()\n"
                )
                files[f"{PACKAGE}/template.in"] = 'package main\nimport "fmt"\n'
                files[f"{PACKAGE}/generator.py"] = ""
                graph = lib.make(files, PACKAGE)
                program = lib.node_by_output(graph, OUTPUT)
                generated = "$(B)/" + PACKAGE + "/emitted.go"
                producer = lib.node_by_output(graph, generated)
                self.assertIn(generated, go_tool_args(program))
                self.assertIn(generated, program["inputs"])
                self.assertIn(producer["uid"], program["deps"])
                self.assertNotIn("$(S)/" + PACKAGE + "/emitted.go", program["inputs"])
                self.assertNotIn(STD + "/fmt/fmt.a", go_tool_args(program))

    def test_existing_source_open_errors_still_fail_the_module(self):
        for failure, message in (
            ("permission", "permission denied"),
            ("symlink-loop", "too many levels"),
            ("directory", "is a directory"),
        ):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory(prefix="ay-go-unreadable-") as directory:
                root = Path(directory)
                lib.seed_root(root)
                files = missing_program()
                files["ya.conf"] = '[flags]\nOPENSOURCE = "yes"\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
                for relative, content in files.items():
                    path = root / relative
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text(content)
                source = root / PACKAGE / "entry/start.go"
                source.parent.mkdir()
                if failure == "permission":
                    source.write_text("package main\n")
                    source.chmod(0)
                    self.addCleanup(lambda p=source: p.exists() and p.chmod(0o644))
                elif failure == "symlink-loop":
                    source.symlink_to(source.name)
                else:
                    source.mkdir()
                result = lib.run_process(
                    "make", "-j0", "-G", "--sandboxing", "--source-root", root,
                    "--target-platform", "default-linux-aarch64",
                    "--host-platform", "default-linux-x86_64", PACKAGE,
                    env={key: value for key, value in os.environ.items() if key not in lib.TOOLCHAIN_ENV_VARS},
                )
                self.assertNotEqual(result.returncode, 0)
                if failure != "directory":
                    self.assertIn(PACKAGE, result.stderr)
                self.assertIn(message, result.stderr.lower())
                self.assertNotIn("panic:", result.stderr)
                self.assertNotIn("no such file", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
