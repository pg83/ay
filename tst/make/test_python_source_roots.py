import base64
import hashlib
import posixpath
import unittest

from tst import lib
from tst.make.test_python_srcs import keys, kvs, objcopy_nodes, python_base


MODULE = "grove/parcel/check"
BASE = lib.BARE_MODULE + "NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nDISABLE(PYTHON_SQLITE3)\n"
CASES = {
    "root_alias": ("${MODDIR}/../leaf.py=external_alias", "", "grove/parcel/leaf.py"),
    "root_namespace": ("${MODDIR}/../leaf.py", "", "grove/parcel/leaf.py"),
    "literal_root": ("grove/literal/./leaf.py=literal_alias", "", "grove/literal/leaf.py"),
    "module_wins": ("orchard/./seed.py=shadow_alias", "SRCDIR(search)\n", MODULE + "/orchard/seed.py"),
    "srcdir_wins": ("orchard/./seed.py=search_alias", "SRCDIR(search)\n", "search/orchard/seed.py"),
    "parent": ("../leaf.py=parent_alias", "", "grove/parcel/leaf.py"),
    "typed_source": ("${ARCADIA_ROOT}/grove/parcel/leaf.py=typed_alias", "ENABLE(PYBUILD_NO_PYC)\n", "grove/parcel/leaf.py"),
    "typed_build": ("${BINDIR}/generated.py=generated_alias", "COPY_FILE(seed.py generated.py)\n", MODULE + "/generated.py"),
    "build_srcdir": ("orchard/./seed.py=generated_search_alias", "COPY_FILE(seed.py generated/orchard/seed.py)\nSRCDIR(${ARCADIA_BUILD_ROOT}/grove/parcel/check/generated)\n", MODULE + "/generated/orchard/seed.py"),
}


def fixture(mode):
    args, before, _ = CASES[mode]
    files = python_base()
    files.update({
        MODULE + "/ya.make": "PY3_LIBRARY()\n" + BASE + before + "PY_SRCS(" + args + ")\nEND()\n",
        MODULE + "/seed.py": "# seed for the genuine build producer\n",
        "grove/parcel/leaf.py": "# source-root fallback\n",
        "grove/literal/leaf.py": "# unclean root-relative literal\n",
        "orchard/seed.py": "# competing source-root file\n",
        "search/orchard/seed.py": "# source-directory candidate\n",
    })
    if mode == "module_wins":
        files[MODULE + "/orchard/seed.py"] = "# module-local candidate\n"
    return files


def suffix():
    return base64.b32encode(hashlib.md5(("$S/" + MODULE).encode()).digest()).decode().lower()[:4]


def py_source_nodes(graph, source):
    return [n for n in graph["graph"] if n["outputs"][0].endswith(".yapyc3") and source in n["inputs"]]


class PythonSourceRootsTest(unittest.TestCase):
    def graph(self, mode):
        graph = lib.make(fixture(mode), MODULE)
        ids = {node["uid"] for node in graph["graph"]}
        self.assertTrue(graph["result"])
        self.assertLessEqual(set(graph["result"]), ids)
        self.assertTrue(all(set(node["deps"]) <= ids for node in graph["graph"]))
        return graph

    def test_unclean_root_relative_sources_keep_their_physical_input_and_lexical_output(self):
        for mode in ("root_alias", "literal_root"):
            with self.subTest(mode=mode):
                graph = self.graph(mode)
                raw, _, physical = CASES[mode]
                token = raw.split("=", 1)[0].replace("${MODDIR}", MODULE)
                output = "$(B)/" + posixpath.normpath(MODULE + "/" + token) + "." + suffix() + ".yapyc3"
                bytecode = lib.node_by_output(graph, output)
                self.assertEqual(bytecode["cmds"][0]["cmd_args"][-3:], [physical + "-", "$(S)/" + physical, output])
                alias = raw.split("=", 1)[1]
                source_key = "resfs/src/resfs/file/py/" + alias + ".py=" + physical
                packed = [n for n in objcopy_nodes(graph, MODULE) if source_key in kvs(n)]
                self.assertEqual(len(packed), 1)
                self.assertIn("$(S)/" + physical, packed[0]["inputs"])
                self.assertIn(bytecode["uid"], packed[0]["deps"])

    def test_namespace_binding_uses_resolved_path_and_original_token_length(self):
        graph = self.graph("root_namespace")
        raw = MODULE + "/../leaf.py"
        physical = "grove/parcel/leaf.py"
        module_name = MODULE.replace("/", ".") + "." + raw[:-3].replace("/", ".")
        mod_list_md5 = hashlib.md5(module_name.encode()).hexdigest()
        expected = "py/namespace/" + mod_list_md5 + "/=" + MODULE.replace("/", ".") + "."
        namespace = [n for n in objcopy_nodes(graph, MODULE) if expected in kvs(n)]
        self.assertEqual(len(namespace), 1)
        archive = next(n for n in graph["graph"] if n["kv"]["p"] == "AR" and n["outputs"][0].endswith(".global.a") and namespace[0]["uid"] in n["deps"])
        self.assertIn(namespace[0]["outputs"][0], archive["inputs"])
        resource_key = "resfs/file/py/" + module_name.replace(".", "/") + ".py"
        packed = [n for n in objcopy_nodes(graph, MODULE) if "--keys" in n["cmds"][0]["cmd_args"] and resource_key in keys(n)]
        self.assertEqual(len(packed), 1)
        self.assertIn("resfs/src/" + resource_key + "=" + physical, kvs(packed[0]))

    def test_module_and_source_directories_keep_priority_over_source_root(self):
        for mode in ("module_wins", "srcdir_wins", "parent"):
            with self.subTest(mode=mode):
                graph = self.graph(mode)
                physical = CASES[mode][2]
                self.assertEqual(len(py_source_nodes(graph, "$(S)/" + physical)), 1)
                self.assertFalse(py_source_nodes(graph, "$(S)/orchard/seed.py"))

    def test_typed_source_and_build_paths_preserve_genuine_producers(self):
        source_graph = self.graph("typed_source")
        self.assertFalse(any(n["outputs"][0].endswith(".yapyc3") for n in source_graph["graph"]))
        self.assertTrue(any("$(S)/grove/parcel/leaf.py" in n["inputs"] for n in objcopy_nodes(source_graph, MODULE)))
        for mode in ("typed_build", "build_srcdir"):
            with self.subTest(mode=mode):
                graph = self.graph(mode)
                output = "$(B)/" + CASES[mode][2]
                producer = lib.node_by_output(graph, output)
                bytecodes = py_source_nodes(graph, output)
                self.assertEqual(len(bytecodes), 1)
                self.assertIn(producer["uid"], bytecodes[0]["deps"])
                self.assertIn("$(S)/" + MODULE + "/seed.py", producer["inputs"])
                self.assertFalse(py_source_nodes(graph, "$(S)/orchard/seed.py"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
