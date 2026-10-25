import base64
import hashlib
import unittest

from tst import lib
from tst.make.test_python2 import python2_base
from tst.make.test_python_srcs import kvs, objcopy_nodes, python_base


def fixture():
    files = python_base()
    files["shared/entry.inc"] = (
        "PY_MAIN(${ENTRY})\nNO_CHECK_IMPORTS(${MASKS})\n"
        "PY_SRCS(real.py=public.entry)\n"
    )
    return files


def program(files, module, body, macro="PY3_PROGRAM"):
    files[f"{module}/ya.make"] = (
        f"{macro}(runner)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n{body}END()\n"
    )
    files[f"{module}/real.py"] = "def launch(): pass\n"
    files[f"{module}/__main__.py"] = ""


def closure(graph, node):
    nodes = {n["uid"]: n for n in graph["graph"]}
    seen = set()
    pending = list(node["deps"])
    while pending:
        uid = pending.pop()
        if uid not in seen:
            seen.add(uid)
            pending.extend(nodes[uid]["deps"])
    return seen


class PythonImportCheckerTest(unittest.TestCase):
    def assert_checker(self, graph, module, masks):
        work = f"$(B)/{module}/test-results/import_test"
        binary = f"$(B)/{module}/runner"
        run = lib.node_by_output(graph, work + "/meta.json")
        checker = "run_pyimports --markup"
        checker += "".join(" --skip " + mask for mask in masks)
        checker += " --trace-dir=" + work + "/testing_out_stuff"
        args = run["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("run_check") - 1:], [
            "$(TEST_TOOL_HOST)/test_tool", "run_check", "--source-root", "$(S)",
            "--checker", checker, "--check-name", "import_test", "--trace-path",
            work + "/ytest.report.trace", "--out-path", work + "/testing_out_stuff",
            "--log-path", work + "/testing_out_stuff/run_check.log", binary,
            "--no-snippet-from-stdout",
        ])
        self.assertEqual(run["inputs"], [binary, f"$(S)/{module}"])
        linked = lib.node_by_output(graph, binary)
        self.assertEqual(linked["kv"]["p"], "LD")
        self.assertIn(linked["uid"], run["deps"])
        self.assertEqual(run["outputs"], [work + suffix for suffix in (
            "/meta.json", "/ytest.report.trace", "/run_test.log",
            "/testing_out_stuff.tar.zstd",
        )])
        return run, linked

    def assert_python(self, graph, module, source, logical, main, masks, version=3):
        run, linked = self.assert_checker(graph, module, masks)
        ext = ".yapyc3" if version == 3 else ".yapyc"
        bytecode = lib.node_by_output(graph, f"$(B)/{module}/{source}{ext}")
        self.assertEqual(bytecode["cmds"][0]["cmd_args"][-3:], [
            f"{module}/{source}-", f"$(S)/{module}/{source}", bytecode["outputs"][0],
        ])
        pycc = lib.node_by_output(graph, f"$(B)/tools/py{version}cc/py{version}cc")
        self.assertIn(pycc["uid"], bytecode["deps"])
        reachable = closure(graph, linked)
        self.assertIn(bytecode["uid"], reachable)
        key = (f"resfs/src/resfs/file/py/{logical.replace('.', '/')}.py"
               if version == 3 else f"resfs/src//py_modules/{logical}")
        packed = next(n for n in objcopy_nodes(graph, module)
                      if key + f"={module}/{source}" in kvs(n))
        self.assertIn(f"$(S)/{module}/{source}", packed["inputs"])
        self.assertIn(bytecode["outputs"][0], packed["inputs"])
        self.assertIn(bytecode["uid"], packed["deps"])
        self.assertIn(packed["uid"], reachable)
        for tool in ("rescompiler", "rescompressor"):
            producer = lib.node_by_output(graph, f"$(B)/tools/{tool}/{tool}")
            self.assertIn(producer["uid"], packed["deps"])
        resources = objcopy_nodes(graph, module)
        mains = [n for n in resources if any(v.startswith("PY_MAIN=") for v in kvs(n))]
        self.assertEqual([v for n in mains for v in kvs(n) if v.startswith("PY_MAIN=")],
                         [] if main is None else ["PY_MAIN=" + main])
        for node in mains:
            self.assertIn(node["uid"], reachable)
        if masks:
            value = " ".join(masks)
            digest = base64.b32encode(hashlib.md5(value.encode()).digest()).decode().lower().rstrip("=")
            exclusions = next(n for n in resources
                              if f"py/no_check_imports/{digest}={value}" in kvs(n))
            self.assertIn(exclusions["uid"], reachable)
        return run, linked

    def test_shared_lexical_include_keeps_each_programs_masks_in_both_root_orders(self):
        for macro in ("PY3_PROGRAM", "PY3_PROGRAM_BIN"):
            with self.subTest(macro=macro):
                files = fixture()
                for name in ("first", "second"):
                    program(files, "copper/" + name,
                            f"SET(ENTRY public.{name}:launch)\n"
                            f"SET(MASKS visible.{name} shared.*)\n"
                            "INCLUDE(${ARCADIA_ROOT}/shared/entry.inc)\n", macro)
                plans = []
                for order in (("first", "second"), ("second", "first")):
                    graph = lib.make(files, "copper/" + order[0], "copper/" + order[1], "-t")
                    nodes = []
                    for name in ("first", "second"):
                        run, linked = self.assert_python(
                            graph, "copper/" + name, "real.py", "public.entry",
                            f"public.{name}:launch", ["visible." + name, "shared.*"],
                        )
                        # Raw CLI graph UIDs are per-dump numeric references;
                        # validate their edges above and compare full plans.
                        nodes.extend((run["cmds"], run["inputs"], linked["cmds"], linked["inputs"]))
                    plans.append(nodes)
                self.assertEqual(*plans)

    def test_library_collection_cannot_replace_a_program_mask_with_none(self):
        files = fixture()
        program(files, "copper/checked", "NO_CHECK_IMPORTS(visible.entry)\nPY_SRCS(real.py)\n")
        # This is a valid mask list, distinct from the exact scalar default
        # sentinel "None". The first borrowed slot used to become --skip None.
        files["copper/library/ya.make"] = (
            f"PY3_LIBRARY()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS(None library.*)\nEND()\n"
        )
        plans = []
        for first, second in (("checked", "library"), ("library", "checked")):
            graph = lib.make(files, "copper/" + first, "copper/" + second, "-t")
            run, _ = self.assert_python(graph, "copper/checked", "real.py", "copper.checked.real",
                                        None, ["visible.entry"])
            self.assertFalse(any("/copper/library/test-results/import_test/" in n["outputs"][0]
                                 for n in graph["graph"]))
            self.assertTrue(any(v.endswith("=None library.*") for n in objcopy_nodes(graph, "copper/library")
                                for v in kvs(n) if v.startswith("py/no_check_imports/")))
            plans.append((run["cmds"], run["inputs"], run["env"]))
        self.assertEqual(*plans)

    def test_main_and_alias_forms_do_not_define_checker_masks(self):
        for body, source, logical, main in (
            ("PY_MAIN(visible/entry:launch)\nPY_SRCS(real.py=public.alias)\n",
             "real.py", "public.alias", "visible.entry:launch"),
            ("PY_SRCS(MAIN real.py=public.alias)\n",
             "real.py", "public.alias", "public.alias:main"),
            ("PY_SRCS(__main__.py)\n", "__main__.py", "copper.control.__main__", "copper.control.__main__"),
            ("PY_SRCS(real.py=public.alias)\n", "real.py", "public.alias", None),
        ):
            with self.subTest(body=body):
                files = fixture()
                program(files, "copper/control", body)
                self.assert_python(lib.make(files, "copper/control", "-t"), "copper/control",
                                   source, logical, main, [])

    def test_explicit_masks_are_independent_of_python2_or_python3_entry(self):
        files = python_base()
        files.update(python2_base())
        program(files, "copper/legacy", "PY_MAIN(legacy.cli:run)\n"
                "NO_CHECK_IMPORTS(legacy.config)\nPY_SRCS(real.py=legacy.api)\n", "PY2_PROGRAM")
        program(files, "copper/modern", "PY_SRCS(MAIN real.py=modern.api)\n"
                "NO_CHECK_IMPORTS(modern.config)\n")
        for first, second in (("legacy", "modern"), ("modern", "legacy")):
            graph = lib.make(files, "copper/" + first, "copper/" + second, "-t")
            self.assert_python(graph, "copper/legacy", "real.py", "legacy.api",
                               "legacy.cli:run", ["legacy.config"], 2)
            self.assert_python(graph, "copper/modern", "real.py", "modern.api",
                               "modern.api:main", ["modern.config"])

    def test_peer_library_masks_do_not_become_program_checker_metadata(self):
        files = fixture()
        program(files, "copper/consumer", "NO_CHECK_IMPORTS(consumer.mask)\n"
                "PY_SRCS(real.py)\nPEERDIR(copper/peer)\n")
        files["copper/peer/ya.make"] = (
            f"PY3_LIBRARY()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS(peer.mask)\nEND()\n"
        )
        graph = lib.make(files, "copper/consumer", "-t")
        _, linked = self.assert_python(graph, "copper/consumer", "real.py", "copper.consumer.real",
                                       None, ["consumer.mask"])
        peer = lib.node_by_output(graph, "$(B)/copper/peer/libpy3copper-peer.global.a")
        self.assertIn(peer["uid"], linked["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
