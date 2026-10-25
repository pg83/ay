import json
import unittest

from tst import lib
from tst.make.test_python_srcs import python_base


BARE = lib.BARE_MODULE


def fixture(mode):
    if mode == "java":
        return lib.java_tree(**{
            "build/conf/autoincludes.json": '["grove/java"]',
            "grove/java/linters.make.inc": 'SET_APPEND(JAVA_IGNORE_CLASSPATH_CLASH_VALUE own.Type)\n',
            "grove/java/ya.make": 'JAVA_LIBRARY_NON_SPLIT()\nCHECK_JAVA_DEPS(strict)\nJAVA_SRCS(Main.java)\nEND()\n',
            "grove/java/Main.java": 'class Main {}\n',
        }), "grove/java", []
    if mode == "cpp":
        return {
            "build/conf/autoincludes.json": '["grove/cpp"]',
            "grove/cpp/linters.make.inc": 'SET_APPEND(USER_CFLAGS -DAUTO_OWNER)\n',
            "grove/cpp/ya.make": 'PROGRAM()\n' + BARE + 'SRCS(main.cpp)\nEND()\n',
            "grove/cpp/main.cpp": 'int main() {return 0;}\n',
        }, "grove/cpp", []
    if mode in ("variants", "proto"):
        if mode == "variants":
            from tst.make.test_python2 import python2_base
            files = python_base()
            files.update(python2_base())
            shared = 'PY23_LIBRARY()\n' + BARE + 'NO_PYTHON_INCLUDES()\nPY_SRCS(item.py)\nEND()\n'
            files["grove/shared/item.py"] = ""
            native_macro = "PY2_PROGRAM"
            native_body = ""
        else:
            from tst.make.test_proto_py_variant import py_proto_files
            files = py_proto_files()
            shared = ('PROTO_LIBRARY()\n' + BARE + 'ONLY_TAGS(CPP_PROTO PY3_PROTO)\nSRCS(item.proto)\n'
                      'IF (CPP_PROTO)\nSRCS(native.cpp)\nENDIF()\nEND()\n')
            files.update({"grove/shared/item.proto": 'syntax = "proto3"; message Item {}\n',
                          "grove/shared/native.cpp": 'int native_value;\n',
                          "grove/shared/sentinel.py": 'value = 1\n'})
            native_macro = "PROGRAM"
            native_body = "SRCS(main.cpp)\n"
            files["grove/legacy/main.cpp"] = "int main() {return 0;}\n"
        files.update({
            "build/conf/autoincludes.json": '["grove"]',
            "grove/linters.make.inc": ('IF (MODULE_LANG == PY3)\nENABLE(DISABLE_FLAKE8)\n'
                                      'ELSE()\nDISABLE(DISABLE_FLAKE8)\nENDIF()\n'
                                      'SET_APPEND(USER_CFLAGS -DAUTO_${MODULE_TAG})\n'),
            "build/config/tests/py_style/default_configs.json": json.dumps({"flake8":"lint/f8.conf", "py2_flake8":"lint/f8.conf"}),
            "lint/f8.conf": "",
            "build/external_resources/flake8_py3/ya.make": lib.resource_lib("FLAKE8_PY3", 123),
            "build/external_resources/flake8_py2/ya.make": lib.resource_lib("FLAKE8_PY2", 124),
            "grove/shared/ya.make": shared,
        })
        if mode == "proto":
            files["grove/linters.make.inc"] = (
                'IF (MODULE_TAG == PY3_PROTO)\n'
                "SET(PY_LINTER_FILES '[\"$S/grove/shared/sentinel.py\"]')\nSTYLE_RUFF()\n"
                'ELSEIF (MODULE_TAG == CPP_PROTO)\nSET_APPEND(USER_CFLAGS -DAUTO_CPP_PROTO)\nENDIF()\n'
            )
            files["build/config/tests/py_style/default_configs.json"] = '{"ruff":"lint/ruff.toml"}'
            files["lint/ruff.toml"] = ""
            files["build/external_resources/ruff/ya.make"] = lib.resource_lib("RUFF", 456)
        for directory, macro, body in (("modern", "PY3_PROGRAM", ""), ("legacy", native_macro, native_body)):
            files[f"grove/{directory}/ya.make"] = (
                macro + '()\n' + BARE + 'NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nNO_CHECK_IMPORTS()\n'
                'DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\nPEERDIR(grove/shared)\n' + body + 'END()\n'
            ) if macro != "PROGRAM" else macro+'()\n'+BARE+'PEERDIR(grove/shared)\n'+body+'END()\n'
        return files, "grove/modern", ["grove/legacy", "-DADD_PEERDIRS_GEN_TESTS=yes"]
    if mode in ("directory", "discarded"):
        files, target, flags = fixture("cpp")
        files["build/conf/autoincludes.json"] = '["grove"]'
        files["grove/linters.make.inc"] = ('IF (NOT MODULE_KIND)\n'
                                           'MESSAGE(FATAL_ERROR wrong_autoinclude_owner)\nENDIF()\nSET_APPEND(USER_CFLAGS -DAUTO_OWNER)\n')
        files.pop("grove/cpp/linters.make.inc")
        if mode == "directory":
            files["grove/ya.make"] = 'RECURSE(cpp)\n'
            return files, "grove", []
        files["grove/rejected/ya.make"] = 'LIBRARY()\n' + BARE + 'BUILD_ONLY_IF(PERMIT)\nEND()\n'
        return files, "grove/cpp", ["grove/rejected"]
    files = python_base()
    initial = "ENABLE(DISABLE_FLAKE8)\n" if mode == "reenable" else ""
    final = "DISABLE(DISABLE_FLAKE8)\n" if mode == "reenable" else "ENABLE(DISABLE_FLAKE8)\n"
    if mode in ("ordinary", "directory-after"):
        final = ""
    if mode == "directory-after":
        initial = "ENABLE(DISABLE_FLAKE8)\n"
    files.update({
        "build/conf/autoincludes.json": '["grove"]',
        "grove/linters.make.inc": final,
        "build/config/tests/py_style/default_configs.json": json.dumps({
            "flake8": "lint/flake8.conf", "ruff": "lint/ruff.toml",
        }),
        "lint/flake8.conf": "", "lint/ruff.toml": "",
        "build/external_resources/flake8_py3/ya.make": (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(FLAKE8_PY3 sbr:123)\nEND()\n"
        ),
        "grove/leaf/ya.make": f"PY3_LIBRARY()\n{BARE}{initial}PY_SRCS(a.py b.py)\nEND()\n",
        "grove/leaf/a.py": "", "grove/leaf/b.py": "",
    })
    if mode in ("directory-after", "owner-post"):
        files["grove/leaf/ya.make"] += "DISABLE(DISABLE_FLAKE8)\n"
    if mode == "style":
        files["grove/linters.make.inc"] += "STYLE_RUFF()\n"
        files["build/external_resources/ruff/ya.make"] = lib.resource_lib("RUFF", 456)
    target = "grove/leaf"
    flags = []
    if mode in ("peer", "peer-ordinary"):
        files["grove/caller/ya.make"] = (
            f"PY3_PROGRAM()\n{BARE}NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nNO_CHECK_IMPORTS()\n"
            "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\nPEERDIR(grove/leaf)\nPY_SRCS(caller.py)\nEND()\n"
        )
        files["grove/caller/caller.py"] = "value = 1\n"
        target = "grove/caller"
        flags = ["-DADD_PEERDIRS_GEN_TESTS=yes"]
        if mode == "peer-ordinary":
            files["grove/linters.make.inc"] = ""
    if mode == "chunked":
        files["grove/leaf/ya.make"] = files["grove/leaf/ya.make"].replace("PY_SRCS", "SET(FLAKE8_FILE_PROCESSING_TIME 31)\nPY_SRCS")
    return files, target, flags


def flake8_nodes(graph):
    return [node for node in graph["graph"]
            if any("/test-results/flake8/" in output for output in node["outputs"])]


class PythonLintFinalEligibilityTest(unittest.TestCase):
    def test_late_autoinclude_disable_controls_whole_flake8_suite_and_results(self):
        for mode, enabled in (("ordinary", True), ("disabled", False), ("reenable", True), ("chunked", False), ("directory-after", False), ("owner-post", False)):
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, "-t", "--cache-tests", *flags)
                self.assertEqual(bool(flake8_nodes(graph)), enabled)
                ids = {node["uid"] for node in graph["graph"]}
                self.assertTrue(set(graph["result"]) <= ids)
                self.assertTrue(graph["result"])
                lib.node_by_output(graph, "$(B)/grove/leaf/libpy3grove-leaf.global.a")
                lib.node_by_output(graph, "$(B)/grove/leaf/a.py.yapyc3")
                self.assertEqual(any("$(B)/resources/FLAKE8_PY3" in node["outputs"]
                                     for node in graph["graph"]), enabled)

    def test_peer_lint_eligibility_uses_its_own_final_module_environment(self):
        for mode, enabled in (("peer", False), ("peer-ordinary", True)):
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, "-t", "--cache-tests", *flags)
                self.assertEqual(bool(flake8_nodes(graph)), enabled)
                if enabled:
                    roots = {output.split("/test-results/")[0] for node in flake8_nodes(graph) for output in node["outputs"]}
                    self.assertEqual(roots, {"$(B)/grove/caller", "$(B)/grove/leaf"})
                lib.node_by_output(graph, "$(B)/grove/caller/caller")
                lib.node_by_output(graph, "$(B)/grove/leaf/libpy3grove-leaf.global.a")

    def test_other_autoinclude_fields_keep_their_own_flags_and_resource_peers(self):
        files, target, flags = fixture("style")
        graph = lib.make(files, target, "-t", "--cache-tests", *flags)
        self.assertFalse(flake8_nodes(graph))
        ruff = lib.node_by_output(graph, "$(B)/grove/leaf/test-results/ruff/meta.json")
        self.assertIn("RUFF_RESOURCE_GLOBAL::$(B)/resources/RUFF", ruff["cmds"][0]["cmd_args"])
        files, target, flags = fixture("cpp")
        node = lib.node_by_output(lib.make(files, target), "$(B)/grove/cpp/main.cpp.o")
        self.assertEqual(node["cmds"][0]["cmd_args"].count("-DAUTO_OWNER"), 1)
        files, target, flags = fixture("java")
        graph = lib.make(files, target, "--target-platform-tests")
        node = lib.node_by_output(graph, "$(B)/grove/java/test-results/grove-java-dependencies/meta.json")
        self.assertEqual(node["cmds"][0]["cmd_args"].count("ignore_class:own.Type"), 1)

    def test_each_python_and_proto_variant_runs_its_own_autoinclude_once(self):
        for mode in ("variants", "proto"):
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, "-t", "--cache-tests", *flags)
                self.assertFalse(flake8_nodes(graph))
                if mode == "variants":
                    lib.node_by_output(graph, "$(B)/grove/shared/test-results/py2_flake8/meta.json")
                    for suffix in ("yapyc", "yapyc3"):
                        lib.node_by_output(graph, "$(B)/grove/shared/item.py." + suffix)
                else:
                    native = lib.node_by_output(graph, "$(B)/grove/shared/native.cpp.o")
                    self.assertEqual(native["cmds"][0]["cmd_args"].count("-DAUTO_CPP_PROTO"), 1)
                    self.assertFalse(any("/grove/shared/test-results/ruff/" in output
                                         for node in graph["graph"] for output in node["outputs"]))
                    lib.node_by_output(graph, "$(B)/grove/shared/item__intpy3___pb2.py")

    def test_discarded_module_preserves_other_root_and_source_producers(self):
        for mode in ("discarded",):
            with self.subTest(mode=mode):
                files, target, flags = fixture(mode)
                graph = lib.make(files, target, *flags)
                compiled = lib.node_by_output(graph, "$(B)/grove/cpp/main.cpp.o")
                self.assertEqual(compiled["cmds"][0]["cmd_args"].count("-DAUTO_OWNER"), 1)



if __name__ == "__main__":
    unittest.main(verbosity=2)
