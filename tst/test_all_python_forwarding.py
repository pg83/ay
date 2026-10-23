"""ALL_PY_SRCS must invoke the same source semantics as explicit PY_SRCS."""
import hashlib
import unittest

import lib
from test_python2 import python2_base
from test_python_import_checker import closure
from test_python_srcs import keys, kvs, make_failure, objcopy_nodes, python_base


def fixture(body, kind="PY3_PROGRAM", sources=None, module="cedar/runner"):
    files = python2_base() if kind.startswith("PY2") else python_base()
    files[f"{module}/ya.make"] = f"{kind}(runner)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n{body}\nEND()\n"
    for source in sources or ("__main__.py", "alpha.py"):
        files[f"{module}/{source}"] = "value = 7\n"
    return files


def complete_nodes(graph):
    nodes = {tuple(node["outputs"]): node for node in graph["graph"]}
    if len(nodes) != len(graph["graph"]):
        raise AssertionError("multiple producers have the same output identities")
    return nodes


class AllPythonForwardingTest(unittest.TestCase):
    def paired(self, body, explicit, *, kind="PY3_PROGRAM", sources=None, module="cedar/runner", options=()):
        all_graph = lib.make(fixture(body, kind, sources, module), module, *options)
        explicit_graph = lib.make(fixture(explicit, kind, sources, module), module, *options)
        # Full commands, hashes, ordered inputs, output identities and producer
        # UIDs must agree, including the native CC/AR/LD and metadata consumers.
        self.assertEqual(complete_nodes(all_graph), complete_nodes(explicit_graph))
        self.assertEqual(all_graph["result"], explicit_graph["result"])
        return all_graph

    def test_discovered_main_matches_explicit_complete_graph(self):
        graph = self.paired("ALL_PY_SRCS()", "PY_SRCS(__main__.py alpha.py)", options=("-t",))
        module = "cedar/runner"
        main = next(n for n in objcopy_nodes(graph, module) if "PY_MAIN=cedar.runner.__main__" in kvs(n))
        value = "PY_MAIN=cedar.runner.__main__"
        digest = hashlib.md5(("$S/" + module + "," + value + "PY3_BIN").encode()).hexdigest()[:26]
        self.assertEqual(main["outputs"], [f"$(B)/{module}/objcopy_{digest}.o"])
        self.assertEqual(kvs(main), [value])
        link = lib.node_by_output(graph, f"$(B)/{module}/runner")
        self.assertIn(main["outputs"][0], link["inputs"])
        self.assertIn(main["uid"], closure(graph, link))
        bytecode = lib.node_by_output(graph, f"$(B)/{module}/__main__.py.yapyc3")
        packed = next(n for n in objcopy_nodes(graph, module) if "resfs/src/resfs/file/py/cedar/runner/__main__.py=cedar/runner/__main__.py" in kvs(n))
        self.assertEqual(keys(packed), ["resfs/file/py/cedar/runner/__main__.py", "resfs/file/py/cedar/runner/__main__.py.yapyc3", "resfs/file/py/cedar/runner/alpha.py", "resfs/file/py/cedar/runner/alpha.py.yapyc3"])
        self.assertIn(bytecode["outputs"][0], packed["inputs"])
        self.assertIn(bytecode["uid"], packed["deps"])
        self.assertIn("$(S)/cedar/runner/__main__.py", packed["inputs"])
        for tool in ("rescompiler", "rescompressor"):
            producer = lib.node_by_output(graph, f"$(B)/tools/{tool}/{tool}")
            self.assertIn(producer["uid"], main["deps"])
            self.assertIn(producer["uid"], packed["deps"])
        for path in ("tools/py3cc/py3cc", "tools/py3cc/slow/slow"):
            producer = lib.node_by_output(graph, "$(B)/" + path)
            self.assertIn(producer["uid"], bytecode["deps"])
        self.assertIn(packed["uid"], closure(graph, link))

    def test_nested_namespaces_and_top_level(self):
        for setting, explicit in (
            ("NAMESPACE public.api", "NAMESPACE public.api"),
            ("TOP_LEVEL", "TOP_LEVEL"),
            ("TOP_LEVEL NAMESPACE ignored", "NAMESPACE ignored TOP_LEVEL"),
        ):
            with self.subTest(setting=setting):
                self.paired(f"ALL_PY_SRCS({setting} leaves)", f"PY_SRCS({explicit} leaves/__main__.py leaves/alpha.py)", sources=("leaves/__main__.py", "leaves/alpha.py"))

    def test_independent_groups_inline_resources_and_source_order(self):
        prefix = "PY_SRCS(TOP_LEVEL extra/first.py)\nRESOURCE(DONT_COMPRESS - note=fake)\n"
        suffix = "\nPY_SRCS(NAMESPACE last extra/last.py)"
        self.paired(prefix + "ALL_PY_SRCS()" + suffix,
                    prefix + "PY_SRCS(__main__.py alpha.py)" + suffix,
                    sources=("__main__.py", "alpha.py", "extra/first.py", "extra/last.py"))
        self.paired("ALL_PY_SRCS(NAMESPACE first one)\nPY_SRCS(TOP_LEVEL two/beta.py)",
                    "PY_SRCS(NAMESPACE first one/alpha.py)\nPY_SRCS(TOP_LEVEL two/beta.py)",
                    kind="PY3_LIBRARY", sources=("one/alpha.py", "two/beta.py"))

    def test_recursive_glob_filters_and_declared_directory_order(self):
        sources = ("alpha.py", "test_skip.py", "other_test.py", "one/beta.py", "one/deep/gamma.py", "two/delta.py")
        self.paired("ALL_PY_SRCS(RECURSIVE NO_TEST_FILES)",
                    "PY_SRCS(one/beta.py two/delta.py one/deep/gamma.py alpha.py)",
                    kind="PY3_LIBRARY", sources=sources)
        self.paired("ALL_PY_SRCS(TOP_LEVEL two one)", "PY_SRCS(TOP_LEVEL two/delta.py one/beta.py)",
                    kind="PY3_LIBRARY", sources=sources)

    def test_explicit_main_and_missing_main_match_shared_path(self):
        for position in ("before", "after"):
            with self.subTest(position=position):
                main = "PY_MAIN(custom.entry:run)"
                body = main + "\nALL_PY_SRCS()" if position == "before" else "ALL_PY_SRCS()\n" + main
                explicit = body.replace("ALL_PY_SRCS()", "PY_SRCS(__main__.py alpha.py)")
                graph = self.paired(body, explicit)
                self.assertIn("PY_MAIN=custom.entry:run", [v for n in objcopy_nodes(graph, "cedar/runner") for v in kvs(n)])
        self.paired("ALL_PY_SRCS()", "PY_SRCS(alpha.py)", sources=("alpha.py",))
        self.paired("ALL_PY_SRCS(missing)", "PY_SRCS()", sources=("alpha.py",))

    def test_py2_library_and_native_source_boundaries(self):
        for kind in ("PY2_PROGRAM", "PY2_LIBRARY", "PY3_LIBRARY", "LIBRARY"):
            with self.subTest(kind=kind):
                setting = "TOP_LEVEL " if kind.startswith("PY2") else ""
                self.paired(f"ALL_PY_SRCS({setting})", f"PY_SRCS({setting}__main__.py alpha.py)", kind=kind)

    def test_forwarding_uses_the_shared_source_tag_binding_point(self):
        # T113 owns the default's absent/empty behavior. This control pins the
        # common entry point without implementing another tag-binding phase.
        for prelude, options in (("", ()), ("SET(MODULE_TAG)\n", ()),
                                 ("SET(MODULE_TAG PRIVATE)\n", ()),
                                 ("", ("-DMODULE_TAG=",)),
                                 ("", ("-DMODULE_TAG=PRIVATE",))):
            with self.subTest(prelude=prelude, options=options):
                self.paired(prelude + "ALL_PY_SRCS()", prelude + "PY_SRCS(__main__.py alpha.py)",
                            kind="PY3_LIBRARY", options=options)

    def test_style_metadata_uses_forwarded_sources(self):
        from test_python_suites import fixture as style_fixture, values
        files = style_fixture("STYLE_PYTHON()\n")
        files["lib/ya.make"] = files["lib/ya.make"].replace("PY_SRCS(a.py)", "ALL_PY_SRCS()")
        graph = lib.make(files, "lib", "-t")
        files["lib/ya.make"] = files["lib/ya.make"].replace("ALL_PY_SRCS()", "PY_SRCS(a.py)")
        explicit = lib.make(files, "lib", "-t")
        self.assertEqual(complete_nodes(graph), complete_nodes(explicit))
        for name in ("black", "flake8"):
            run = lib.node_by_output(graph, f"$(B)/lib/test-results/{name}/meta.json")
            self.assertIn("$(S)/lib/a.py", run["inputs"])
            self.assertIn("$(S)/lib/a.py", run["cmds"][0]["cmd_args"])
            self.assertIn("$(S)/lib/a.py", values(run, "--test-related-path"))

    def test_shared_lexical_include_and_both_root_orders(self):
        for roots in (("copper", "silver"), ("silver", "copper")):
            files = python_base()
            files["shared/sources.inc"] = "ALL_PY_SRCS(NAMESPACE ${PUBLIC} sources)\n"
            for module, namespace in (("copper", "red.entry"), ("silver", "blue.entry")):
                files[f"{module}/ya.make"] = f"PY3_PROGRAM(runner)\nSET(PUBLIC {namespace})\nINCLUDE(${{ARCADIA_ROOT}}/shared/sources.inc)\nEND()\n"
                files[f"{module}/sources/__main__.py"] = ""
            graph = lib.make(files, roots[0], roots[1])
            for module, namespace in (("copper", "red.entry"), ("silver", "blue.entry")):
                mains = [v for n in objcopy_nodes(graph, module) for v in kvs(n) if v.startswith("PY_MAIN=")]
                self.assertEqual(mains, [f"PY_MAIN={namespace}.sources.__main__"])
                linked = lib.node_by_output(graph, f"$(B)/{module}/runner")
                other = "silver" if module == "copper" else "copper"
                self.assertFalse(any(path.startswith(f"$(B)/{other}/") for path in linked["inputs"]))

    def test_py23_library_keeps_python_version_contexts_in_both_root_orders(self):
        files = python2_base()
        for path, content in python_base().items():
            files.setdefault(path, content)
        files["shared/library/ya.make"] = "PY23_LIBRARY()\nALL_PY_SRCS()\nEND()\n"
        files["shared/library/leaf.py"] = "value = 1\n"
        for module, kind in (("old", "PY2_PROGRAM"), ("new", "PY3_PROGRAM")):
            files[f"{module}/ya.make"] = f"{kind}(runner)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nPEERDIR(shared/library)\nEND()\n"
        for roots in (("old", "new"), ("new", "old")):
            with self.subTest(roots=roots):
                graph = lib.make(files, roots[0], roots[1])
                explicit_files = dict(files)
                explicit_files["shared/library/ya.make"] = "PY23_LIBRARY()\nPY_SRCS(leaf.py)\nEND()\n"
                explicit = lib.make(explicit_files, roots[0], roots[1])
                self.assertEqual(complete_nodes(graph), complete_nodes(explicit))
                for module, extension in (("old", ".yapyc"), ("new", ".yapyc3")):
                    bytecode = lib.node_by_output(graph, "$(B)/shared/library/leaf.py" + extension)
                    linked = lib.node_by_output(graph, f"$(B)/{module}/runner")
                    self.assertIn(bytecode["uid"], closure(graph, linked))
                    other = ".yapyc3" if extension == ".yapyc" else ".yapyc"
                    wrong = lib.node_by_output(graph, "$(B)/shared/library/leaf.py" + other)
                    self.assertNotIn(wrong["uid"], closure(graph, linked))

    def test_rejected_top_level_main_and_missing_namespace(self):
        for call, message in (("ALL_PY_SRCS(TOP_LEVEL)", "TOP_LEVEL __main__.py is not allowed"),
                              ("ALL_PY_SRCS(NAMESPACE)", "ALL_PY_SRCS NAMESPACE expects a value")):
            with self.subTest(call=call):
                process = make_failure(fixture(call), "cedar/runner")
                self.assertNotEqual(process.returncode, 0)
                self.assertIn(message, process.stderr)


if __name__ == "__main__":
    unittest.main()
