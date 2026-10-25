import base64
import hashlib
import unittest

from tst import lib
from tst.make.test_python2 import python2_base, section
from tst.make.test_python_srcs import keys, kvs, make_failure, objcopy_nodes, objcopy_with_kv, python_base


def resource_kvs(graph):
    return [arg for node in graph["graph"] for cmd in node["cmds"]
            for arg in cmd["cmd_args"] if arg.startswith(("resfs/src/", "py/namespace/", "PY_MAIN="))]


def namespace_key(modules, root, value):
    digest = hashlib.md5("".join(modules).encode()).hexdigest()
    return f"py/namespace/{digest}/{root}={value}"


class PythonSourceAliasTest(unittest.TestCase):
    def test_python3_alias_bypasses_every_namespace_and_keeps_physical_paths(self):
        for scope in ("", "TOP_LEVEL", "NAMESPACE hidden.scope"):
            with self.subTest(scope=scope):
                files = python_base()
                files.update({
                    "ochre/library/ya.make": (
                        f"PY3_LIBRARY()\nPY_SRCS({scope} branch/real.py=public.api)\nEND()\n"
                    ),
                    "ochre/library/branch/real.py": "value = 1\n",
                })
                graph = lib.make(files, "ochre/library")
                packed = objcopy_with_kv(graph, "ochre/library",
                                        "resfs/src/resfs/file/py/public/api.py=ochre/library/branch/real.py")
                self.assertEqual(keys(packed), ["resfs/file/py/public/api.py",
                                                "resfs/file/py/public/api.py.yapyc3"])
                bytecode = lib.node_by_output_prefix(graph, "$(B)/ochre/library/branch/real.py.")
                self.assertTrue(bytecode["outputs"][0].endswith(".yapyc3"))
                self.assertEqual(bytecode["cmds"][0]["cmd_args"][-3:], [
                    "ochre/library/branch/real.py-", "$(S)/ochre/library/branch/real.py",
                    bytecode["outputs"][0],
                ])
                self.assertIn(bytecode["uid"], packed["deps"])
                self.assertIn(bytecode["outputs"][0], packed["inputs"])
                self.assertFalse(any(k.startswith("py/namespace/") for k in resource_kvs(graph)))

    def test_namespace_maps_exclude_aliases_but_hash_the_full_ordered_module_list(self):
        files = python_base()
        files["ochre/library/ya.make"] = (
            "PY3_LIBRARY()\nPY_SRCS(z.py=public.z a.py NAMESPACE custom b.py=other.b c.py)\n"
            "PY_SRCS(TOP_LEVEL plain.py x.py=public.x)\nPY_SRCS(only.py=isolated)\nEND()\n"
        )
        for name in ("z", "a", "b", "c", "plain", "x", "only"):
            files[f"ochre/library/{name}.py"] = ""
        graph = lib.make(files, "ochre/library")
        namespaces = [k for k in resource_kvs(graph) if k.startswith("py/namespace/")]
        self.assertEqual(namespaces, [
            namespace_key(["public.z", "ochre.library.a", "other.b", "custom.c"],
                          "ochre/library", "custom.:ochre.library."),
            namespace_key(["plain", "public.x"], "ochre/library", "."),
        ])
        packed = objcopy_with_kv(graph, "ochre/library",
                                "resfs/src/resfs/file/py/public/z.py=ochre/library/z.py")
        self.assertEqual([k for k in kvs(packed) if ".yapyc3=" not in k], [
            "resfs/src/resfs/file/py/public/z.py=ochre/library/z.py",
            "resfs/src/resfs/file/py/ochre/library/a.py=ochre/library/a.py",
            "resfs/src/resfs/file/py/other/b.py=ochre/library/b.py",
            "resfs/src/resfs/file/py/custom/c.py=ochre/library/c.py",
        ])

    def test_two_aliases_share_one_physical_source_and_bytecode_producer(self):
        for version, base in ((3, python_base), (2, python2_base)):
            with self.subTest(version=version):
                files = base()
                files.update({
                    "ochre/library/ya.make": (
                        f"PY{version}_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
                        "PY_SRCS(shared.py=visible.one shared.py=visible.two)\n"
                        "PY_SRCS(shared.py=visible.three)\nEND()\n"
                    ),
                    "ochre/library/shared.py": "",
                })
                graph = lib.make(files, "ochre/library")
                suffix = ".yapyc3" if version == 3 else ".yapyc"
                bytecodes = [n for n in graph["graph"]
                             if n["outputs"] == ["$(B)/ochre/library/shared.py" + suffix]]
                self.assertEqual(len(bytecodes), 1)
                resources = objcopy_nodes(graph, "ochre/library")
                self.assertEqual(len(resources), 2)
                for packed in resources:
                    self.assertIn(bytecodes[0]["uid"], packed["deps"])
                    self.assertIn("$(S)/ochre/library/shared.py", packed["inputs"])
                    self.assertIn(bytecodes[0]["outputs"][0], packed["inputs"])
                if version == 3:
                    actual = [key for packed in resources for key in keys(packed)]
                    expected = [f"resfs/file/py/visible/{name}.py{ext}"
                                for name in ("one", "two", "three") for ext in ("", ".yapyc3")]
                else:
                    actual = [base64.b64decode(key).decode() for packed in resources
                              for key in section(packed, "--keys")]
                    expected = [f"/{kind}/visible.{name}" for name in ("one", "two", "three")
                                for kind in ("py_modules", "py_code")]
                self.assertEqual(actual, expected)

    def test_empty_and_py_suffixed_aliases_are_logical_names_verbatim(self):
        files = python_base()
        files.update({
            "ochre/library/ya.make": "PY3_LIBRARY()\nPY_SRCS(real.py= other.py=public.py)\nEND()\n",
            "ochre/library/real.py": "", "ochre/library/other.py": "",
        })
        graph = lib.make(files, "ochre/library")
        packed = objcopy_with_kv(graph, "ochre/library", "resfs/src/resfs/file/py/.py=ochre/library/real.py")
        self.assertEqual(keys(packed), ["resfs/file/py/.py", "resfs/file/py/.py.yapyc3",
                                        "resfs/file/py/public/py.py", "resfs/file/py/public/py.py.yapyc3"])

    def test_generated_aliases_retain_original_producer_inputs_and_main_output(self):
        files = python_base()
        lib.tool_program(files, "tools/engrave", "engrave")
        files.update({
            "ochre/library/ya.make": (
                "PY3_LIBRARY()\nPY_SRCS(payload.py=public.payload "
                "${ARCADIA_BUILD_ROOT}/ochre/library/side.py=public.side)\n"
                "RUN_PROGRAM(tools/engrave --main manifest.txt --side side.py --payload payload.py "
                "IN seed.txt OUT_NOAUTO payload.py side.py manifest.txt)\nEND()\n"
            ),
            "ochre/library/seed.txt": "seed\n",
        })
        graph = lib.make(files, "ochre/library")
        producer = lib.node_by_output(graph, "$(B)/ochre/library/manifest.txt")
        self.assertEqual(producer["outputs"], ["$(B)/ochre/library/manifest.txt",
                                              "$(B)/ochre/library/side.py", "$(B)/ochre/library/payload.py"])
        self.assertIn("$(S)/ochre/library/seed.txt", producer["inputs"])
        tool = lib.node_by_output(graph, "$(B)/tools/engrave/engrave")
        self.assertIn(tool["uid"], producer["deps"])
        for name in ("payload", "side"):
            bytecode = lib.node_by_output_prefix(graph, f"$(B)/ochre/library/{name}.py.")
            self.assertIn(producer["uid"], bytecode["deps"])
            self.assertIn("$(B)/ochre/library/manifest.txt", bytecode["inputs"])
            self.assertIn(f"$(B)/ochre/library/{name}.py", bytecode["inputs"])
            expected = f"resfs/src/resfs/file/py/public/{name}.py=ochre/library/{name}.py"
            packed_nodes = [n for n in graph["graph"] if any(expected in c["cmd_args"] for c in n["cmds"])]
            self.assertEqual(len(packed_nodes), 1, expected)
            packed = packed_nodes[0]
            self.assertIn(producer["uid"], packed["deps"])
            code_key = f"resfs/src/resfs/file/py/public/{name}.py.yapyc3=" + bytecode["outputs"][0][5:]
            packed_code = next(n for n in graph["graph"] if any(code_key in c["cmd_args"] for c in n["cmds"]))
            self.assertIn(bytecode["uid"], packed_code["deps"])
            self.assertIn(bytecode["outputs"][0], packed_code["inputs"])
            self.assertFalse(any(f"$(S)/ochre/library/{name}.py" in n["inputs"] for n in graph["graph"]))

    def test_python2_aliases_use_dotted_keys_and_copy_producers(self):
        files = python2_base()
        files.update({
            "ochre/library/ya.make": (
                f"PY2_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
                "SRCDIR(physical)\nCOPY_FILE(template.py copied.py)\n"
                "PY_SRCS(NAMESPACE ignored.scope real.py=legacy.api copied.py=legacy.copy)\nEND()\n"
            ),
            "ochre/library/template.py": "", "physical/real.py": "",
        })
        graph = lib.make(files, "ochre/library")
        actual = resource_kvs(graph)
        self.assertIn("resfs/src//py_modules/legacy.api=physical/real.py", actual)
        self.assertIn("resfs/src//py_modules/legacy.copy=ochre/library/copied.py", actual)
        copy = lib.node_by_output(graph, "$(B)/ochre/library/copied.py")
        bytecode = lib.node_by_output(graph, "$(B)/ochre/library/copied.py.yapyc")
        self.assertIn(copy["uid"], bytecode["deps"])
        self.assertIn("$(B)/ochre/library/copied.py", bytecode["inputs"])
        self.assertIn("$(S)/ochre/library/template.py", copy["inputs"])

    def test_shared_include_keeps_module_context_aliases_and_namespace_hashes(self):
        files = python_base()
        files["shared/python.inc"] = "PY_SRCS(real.py=${PUBLIC_MODULE} plain.py)\n"
        for project, alias in (("ochre/left", "public.one"), ("ochre/right", "public.two")):
            files[f"{project}/ya.make"] = (
                f"PY3_LIBRARY()\nSET(PUBLIC_MODULE {alias})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/python.inc)\nEND()\n"
            )
            files[f"{project}/real.py"] = ""
            files[f"{project}/plain.py"] = ""
        for targets in (("ochre/left", "ochre/right"), ("ochre/right", "ochre/left")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets)
                for project, alias in (("ochre/left", "public.one"), ("ochre/right", "public.two")):
                    packed = objcopy_with_kv(graph, project,
                                            f"resfs/src/resfs/file/py/{alias.replace('.', '/')}.py={project}/real.py")
                    self.assertIn(f"$(S)/{project}/real.py", packed["inputs"])
                    namespace = project.replace("/", ".")
                    self.assertIn(namespace_key([alias, namespace + ".plain"], project, namespace + "."),
                                  resource_kvs(graph))

    def test_python_test_sources_share_the_test_package_with_explicit_aliases(self):
        files = python_base()
        files.update({
            "library/python/pytest/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}END()\n",
            "ochre/checks/ya.make": (
                f"PY3TEST()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\n"
                "PY_SRCS(setup.py=__tests__.conftest branch/__init__.py=__tests__.branch.__init__)\n"
                "TEST_SRCS(test_case.py)\nEND()\n"
            ),
            "ochre/checks/setup.py": "", "ochre/checks/branch/__init__.py": "",
            "ochre/checks/test_case.py": "",
        })
        graph = lib.make(files, "ochre/checks", "-t")
        packed = []
        for source, module in (("setup.py", "__tests__/conftest"),
                               ("branch/__init__.py", "__tests__/branch/__init__")):
            key = f"resfs/src/resfs/file/py/{module}.py=ochre/checks/{source}"
            nodes = [n for n in graph["graph"] if any(key in c["cmd_args"] for c in n["cmds"])]
            self.assertEqual(len(nodes), 1, key)
            self.assertIn(f"$(S)/ochre/checks/{source}", nodes[0]["inputs"])
            packed.append(nodes[0])
        self.assertEqual([k for k in resource_kvs(graph) if k.startswith("py/namespace/")], [
            namespace_key(["__tests__.test_case"], "ochre/checks", "__tests__."),
        ])
        linked = next(n for n in graph["graph"] if n["kv"].get("p") == "LD"
                      and n["outputs"][0].startswith("$(B)/ochre/checks/"))
        by_uid = {n["uid"]: n for n in graph["graph"]}
        pending = list(linked["deps"])
        seen = set()
        while pending:
            uid = pending.pop()
            if uid not in seen:
                seen.add(uid)
                pending.extend(by_uid[uid]["deps"])
        for node in packed:
            self.assertIn(node["uid"], seen)
        suite = lib.node_by_output(graph, "$(B)/ochre/checks/test-results/py3test/meta.json")
        self.assertIn(linked["uid"], suite["deps"])

    def test_aliases_obey_explicit_main_and_do_not_trigger_implicit_main(self):
        for version, base in ((3, python_base), (2, python2_base)):
            for main in (False, True):
                with self.subTest(version=version, main=main):
                    files = base()
                    files.update({
                        "ochre/app/ya.make": (
                            f"PY{version}_PROGRAM()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
                            f"PY_SRCS(NAMESPACE ignored {'MAIN ' if main else ''}__main__.py=public.entry)\nEND()\n"
                        ),
                        "ochre/app/__main__.py": "",
                    })
                    graph = lib.make(files, "ochre/app")
                    actual = resource_kvs(graph)
                    if main:
                        self.assertIn("PY_MAIN=public.entry:main", actual)
                    else:
                        self.assertFalse(any(k.startswith("PY_MAIN=") for k in actual))
                    key = ("resfs/src/resfs/file/py/public/entry.py" if version == 3
                           else "resfs/src//py_modules/public.entry")
                    self.assertIn(key + "=ochre/app/__main__.py", actual)

    def test_reserved_python3_main_module_reports_the_pinned_error(self):
        for source in ("real.py=__main__", "TOP_LEVEL __main__.py"):
            with self.subTest(source=source):
                files = python_base()
                files.update({"ochre/app/ya.make": f"PY3_PROGRAM()\nPY_SRCS({source})\nEND()\n",
                              "ochre/app/real.py": "", "ochre/app/__main__.py": ""})
                process = make_failure(files, "ochre/app")
                self.assertNotEqual(process.returncode, 0)
                self.assertIn("TOP_LEVEL __main__.py is not allowed in PY3_PROGRAM", process.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
