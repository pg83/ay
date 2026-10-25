import unittest

from tst import lib
from tst.make.test_python_srcs import python_base


RUNNER = "tools/forecast"
CLIENT = "clients/terminal"
PACKAGE = "packages/weather/legacy"
BINARY = f"$(B)/{RUNNER}/weather_cli"


def world():
    files = python_base()
    files.update({
        "checks/ya.make": "EXECTEST()\nRUN(true)\nDEPENDS(collections/release)\nEND()\n",
        "collections/release/ya.make": "UNION()\nINCLUDE(${ARCADIA_ROOT}/shared/collection.inc)\nEND()\n",
        "shared/collection.inc": f"PEERDIR({RUNNER})\n",
        f"{RUNNER}/ya.make": (
            f"PY3_PROGRAM(weather_cli)\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\n"
            f"PEERDIR({CLIENT})\nCOPY_FILE(main.py __main__.py)\nPY_SRCS(__main__.py)\nEND()\n"
        ),
        f"{RUNNER}/main.py": "",
        f"{CLIENT}/ya.make": (
            f"PY23_LIBRARY()\n{lib.BARE_MODULE}IF (PYTHON2)\nPEERDIR(compat/terminal)\nELSE()\n"
            f"PEERDIR({PACKAGE})\nPY_SRCS(NAMESPACE terminal cli.py)\nENDIF()\nEND()\n"
        ),
        f"{CLIENT}/cli.py": "",
        f"{PACKAGE}/ya.make": (
            f"PY23_LIBRARY()\n{lib.BARE_MODULE}PEERDIR(packages/certificates)\n"
            "IF (PYTHON2)\nPEERDIR(compat/backport)\nENDIF()\n"
            "SRCS(accelerator.c)\nPY_SRCS(TOP_LEVEL weather/__init__.py weather/loop.py)\nEND()\n"
        ),
        f"{PACKAGE}/weather/__init__.py": "",
        f"{PACKAGE}/weather/loop.py": "",
        f"{PACKAGE}/accelerator.c": "int speedup;\n",
    })
    for directory in ("compat/terminal", "compat/backport", "packages/certificates"):
        files[f"{directory}/ya.make"] = (
            f"PY23_LIBRARY()\n{lib.BARE_MODULE}PY_SRCS(marker.py)\nEND()\n"
        )
        files[f"{directory}/marker.py"] = ""
    return files


def outputs(graph):
    return [output for node in graph["graph"] for output in node["outputs"]]


def compiled_python(graph, path):
    return next(node for node in graph["graph"]
                if f"$(S)/{path}" in node["inputs"]
                and any(output.endswith(".yapyc3") for output in node["outputs"]))


class PythonUnionPeerTest(unittest.TestCase):
    def assert_binary_closure(self, graph):
        binary = lib.node_by_output(graph, BINARY)
        native = lib.node_by_output(graph, f"$(B)/{PACKAGE}/accelerator.c.py3.o")
        self.assertEqual(binary["platform"], "default-linux-aarch64")
        self.assertEqual(native["platform"], binary["platform"])
        self.assertIn(f"$(S)/{PACKAGE}/accelerator.c", native["inputs"])
        for suffix in (".a", ".global.a"):
            path = f"$(B)/{PACKAGE}/libpy3packages-weather-legacy{suffix}"
            archive = lib.node_by_output(graph, path)
            self.assertIn(path, binary["inputs"])
            self.assertIn(archive["uid"], binary["deps"])
            if suffix == ".a":
                self.assertIn(native["outputs"][0], archive["inputs"])
                self.assertIn(native["uid"], archive["deps"])
        for path in (f"{PACKAGE}/weather/__init__.py", f"{PACKAGE}/weather/loop.py",
                     "packages/certificates/marker.py", f"{CLIENT}/cli.py"):
            node = compiled_python(graph, path)
            self.assertEqual(node["cmds"][0]["cmd_args"][0], "$(B)/tools/py3cc/py3cc")
            self.assertIn("$(B)/tools/py3cc/slow/slow", node["inputs"])
            resource = next(n for n in graph["graph"] if node["outputs"][0] in n["inputs"])
            self.assertIn(node["uid"], resource["deps"])
            if path.startswith(PACKAGE + "/"):
                logical = path[len(PACKAGE) + 1:]
                self.assertIn(f"resfs/src/resfs/file/py/{logical}={path}", resource["cmds"][0]["cmd_args"])
        self.assertFalse(any("/compat/" in p or p.endswith(".yapyc") for p in outputs(graph)))
        return binary

    def test_union_runtime_dependency_selects_binary_and_its_python_peer_closure(self):
        for nested in (False, True):
            with self.subTest(nested=nested):
                files = world()
                if nested:
                    files["collections/release/ya.make"] = "UNION()\nPEERDIR(collections/inner)\nEND()\n"
                    files["collections/inner/ya.make"] = "UNION()\nINCLUDE(${ARCADIA_ROOT}/shared/collection.inc)\nEND()\n"
                graph = lib.make(files, "checks", "-t", "--strip-packages-from-results")
                binary = self.assert_binary_closure(graph)
                suite = lib.node_by_output(graph, "$(B)/checks/test-results/exectest/meta.json")
                self.assertIn(binary["uid"], suite["deps"])
                self.assertIn(binary["uid"], graph["result"])
                self.assertIn("$(B)/tools/forecast", suite["env"]["PATH"].split(":"))
                archive = lib.node_by_output(graph, f"$(B)/{RUNNER}/libpy3weather_cli.global.a")
                self.assertNotIn(archive["uid"], suite["deps"])

    def test_other_collection_contexts_select_the_same_source_defined_binary_tag(self):
        for module in ("PACKAGE", "CI_GROUP"):
            with self.subTest(module=module):
                files = world()
                files["collections/release/ya.make"] = f"{module}(release)\nINCLUDE(${{ARCADIA_ROOT}}/shared/collection.inc)\nEND()\n"
                graph = lib.make(files, "collections/release")
                binary = self.assert_binary_closure(graph)
                artifact = ("$(B)/collections/release/release.final.pkg.fake" if module == "PACKAGE"
                            else "$(B)/collections/release/release.ci.pkg.fake")
                collection = lib.node_by_output(graph, artifact)
                self.assertIn(BINARY, collection["inputs"])
                self.assertIn(binary["uid"], collection["deps"])

    def test_generated_package_source_keeps_its_registered_producer_and_input(self):
        files = world()
        files[f"{PACKAGE}/ya.make"] = files[f"{PACKAGE}/ya.make"].replace(
            "END()", "RUN_PYTHON3(emit.py OUT_NOAUTO version.py)\nPY_SRCS(TOP_LEVEL version.py=weather.version)\nEND()")
        files[f"{PACKAGE}/emit.py"] = ""
        graph = lib.make(files, "checks", "-t")
        self.assert_binary_closure(graph)
        generated = lib.node_by_output(graph, f"$(B)/{PACKAGE}/version.py")
        bytecode = next(n for n in graph["graph"] if generated["outputs"][0] in n["inputs"]
                        and any(out.endswith(".yapyc3") for out in n["outputs"]))
        self.assertIn(generated["uid"], bytecode["deps"])
        self.assertIn(f"$(S)/{PACKAGE}/emit.py", generated["inputs"])
        self.assertNotIn(f"$(S)/{PACKAGE}/version.py", bytecode["inputs"])

    def test_python_peer_keeps_library_side_and_program_self_peer_remains_distinct(self):
        files = world()
        files["consumer/ya.make"] = f"PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR({RUNNER})\nEND()\n"
        graph = lib.make(files, "consumer")
        self.assertNotIn(BINARY, outputs(graph))
        archive = lib.node_by_output(graph, f"$(B)/{RUNNER}/libpy3weather_cli.global.a")
        consumer = lib.node_by_output(graph, "$(B)/consumer/consumer")
        self.assertIn(archive["uid"], consumer["deps"])
        graph = lib.make(files, "checks", "consumer", "-t")
        binary = self.assert_binary_closure(graph)
        self.assertEqual(outputs(graph).count(BINARY), 1)
        self.assertEqual(outputs(graph).count(archive["outputs"][0]), 1)
        main = next(n for n in graph["graph"]
                    if f"PY_MAIN={RUNNER.replace('/', '.')}.__main__" in n["cmds"][0]["cmd_args"])
        self.assertIn(main["uid"], binary["deps"])

    def test_unconsumed_union_does_not_build_python_peers_or_both_variants(self):
        graph = lib.make(world(), "collections/release")
        self.assertNotIn(BINARY, outputs(graph))
        self.assertFalse(any(p.endswith((".yapyc", ".yapyc3")) and f"/{PACKAGE}/" in p
                             for p in outputs(graph)))

    def test_shared_lexical_collection_include_preserves_each_program_context(self):
        files = world()
        files["checks/ya.make"] = "EXECTEST()\nRUN(true)\nDEPENDS(collections/release collections/preview)\nEND()\n"
        files["shared/collection.inc"] = "PEERDIR(${COLLECTION_RUNNER})\n"
        for collection, runner in (("release", RUNNER), ("preview", "tools/preview")):
            files[f"collections/{collection}/ya.make"] = (
                f"UNION()\nSET(COLLECTION_RUNNER {runner})\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/collection.inc)\nEND()\n"
            )
        files["tools/preview/ya.make"] = files[f"{RUNNER}/ya.make"].replace("weather_cli", "preview_cli")
        files["tools/preview/main.py"] = ""
        graph = lib.make(files, "checks", "-t")
        self.assert_binary_closure(graph)
        preview = lib.node_by_output(graph, "$(B)/tools/preview/preview_cli")
        suite = lib.node_by_output(graph, "$(B)/checks/test-results/exectest/meta.json")
        for path in (BINARY, preview["outputs"][0]):
            self.assertIn(lib.node_by_output(graph, path)["uid"], suite["deps"])
            self.assertEqual(outputs(graph).count(path), 1)

    def test_missing_collection_peer_retains_the_module_configuration_error(self):
        files = world()
        files["shared/collection.inc"] = "PEERDIR(absent)\n"
        process = lib.make_process(files, "checks", "-t")
        self.assertNotEqual(process.returncode, 0)
        self.assertIn("absent/ya.make: no such file or directory", process.stderr)
        self.assertNotIn("panic:", process.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
