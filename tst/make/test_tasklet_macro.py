import unittest

from tst import lib
from tst.make.test_proto_desc import desc_files
from tst.make.test_proto_py2_context import proto_world
from tst.make.test_python_proto import MYPY, WRAPPER, args


MODULE = "contracts/delivery"
INCLUDE = "lexical/task-macros.inc"
API = "tasklet/v1/api"
CPP_TOOL = "tasklet/v1/gen/cpp"
PY_TOOL = "tasklet/v1/gen/python"
CPP_BINARY = f"$(B)/{CPP_TOOL}/emit-native-tasks"
PY_BINARY = f"$(B)/{PY_TOOL}/emit-python-tasks"
RUNTIMES = ("tasklet/v1/domain/sandbox", "tasklet/v1/runtime", "sandbox/sdk2")


def tasklet_world(tags="", optimize=False):
    files = {**proto_world(), **desc_files()}
    for directory, binary in ((CPP_TOOL, "emit-native-tasks"), (PY_TOOL, "emit-python-tasks")):
        lib.tool_program(files, directory, binary)
    files[f"{CPP_TOOL}/ya.make"] = files[f"{CPP_TOOL}/ya.make"].replace(
        "END()", "INDUCED_DEPS(h+cpp ${ARCADIA_ROOT}/tasklet/v1/runtime/cpp/tasklet.h)\nEND()"
    )
    files["tasklet/v1/runtime/cpp/tasklet.h"] = "#pragma once\n"
    files[INCLUDE] = "TASKLET()\n"
    for directory in (MODULE, API):
        body = f"INCLUDE(${{ARCADIA_ROOT}}/{INCLUDE})\nPY_NAMESPACE(delivery.messages)\n" if directory == MODULE else ""
        files[f"{directory}/ya.make"] = (
            f"PROTO_LIBRARY({directory.rsplit('/', 1)[-1]})\n{lib.BARE_MODULE}" + (tags if directory == MODULE else "")
            + ("" if optimize else "NO_OPTIMIZE_PY_PROTOS()\n")
            + body + "SRCS(parcel.proto)\nEND()\n"
        )
        files[f"{directory}/parcel.proto"] = 'syntax = "proto2";\nmessage Parcel {}\n'
    for i, directory in enumerate(RUNTIMES):
        files[f"{directory}/ya.make"] = (
            f"PY23_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n"
            f"RESOURCE(DONT_COMPRESS - task_runtime_{i}=present)\nEND()\n"
        )
    for target, kind in (("courier2", "PY2_PROGRAM"), ("courier3", "PY3_PROGRAM"), ("native", "PROGRAM")):
        files[f"{target}/ya.make"] = (
            f"{kind}()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nPEERDIR({MODULE})\nEND()\n"
        )
    return files


def remove_modules(files, *directories):
    for path in list(files):
        if any(path.startswith(directory + "/") for directory in directories):
            del files[path]


class TaskletMacroTest(unittest.TestCase):
    def test_cpp_composition_preserves_plugin_order_and_registered_header_edges(self):
        files = tasklet_world("ONLY_TAGS(CPP_PROTO)\n")
        remove_modules(files, PY_TOOL, *RUNTIMES)
        # Keep only the native tool's induced source header under runtime/.
        header = "$(S)/tasklet/v1/runtime/cpp/tasklet.h"
        files[header.removeprefix("$(S)/")] = "#pragma once\n"
        lib.tool_program(files, "generators/extra", "extra-backend")
        files[INCLUDE] = (
            "CPP_PROTO_PLUGIN(before generators/extra .before.h)\nTASKLET()\n"
            "CPP_PROTO_PLUGIN(after generators/extra .after.h)\n"
        )
        files["native/ya.make"] = files["native/ya.make"].replace("END()", "SRCS(use.cpp)\nEND()")
        files["native/use.cpp"] = f'#include "{MODULE}/parcel.tasklet.h"\n'
        graph = lib.make(files, "native")
        stem = f"$(B)/{MODULE}/parcel"
        producer = lib.node_by_output(graph, stem + ".tasklet.h")
        self.assertEqual(producer["outputs"], [stem + s for s in (".pb.h", ".pb.cc", ".before.h", ".tasklet.h", ".after.h")])
        self.assertEqual([a for a in args(producer) if a.startswith("--plugin=protoc-gen-")], [
            "--plugin=protoc-gen-cpp_styleguide=$(B)/contrib/tools/protoc/plugins/cpp_styleguide/cpp_styleguide",
            "--plugin=protoc-gen-before=$(B)/generators/extra/extra-backend",
            "--plugin=protoc-gen-tasklet_cpp=" + CPP_BINARY,
            "--plugin=protoc-gen-after=$(B)/generators/extra/extra-backend",
        ])
        self.assertIn("--tasklet_cpp_out=$(B)/", args(producer))
        self.assertIn(CPP_BINARY, producer["inputs"])
        self.assertIn(lib.node_by_output(graph, CPP_BINARY)["uid"], producer["foreign_deps"]["tool"])
        consumer = lib.node_by_output(graph, "$(B)/native/use.cpp.o")
        self.assertIn(producer["uid"], consumer["deps"])
        self.assertIn(stem + ".pb.h", consumer["inputs"])
        self.assertIn(stem + ".tasklet.h", consumer["inputs"])
        self.assertIn(header, consumer["inputs"])
        self.assertNotIn(f"$(S)/{MODULE}/parcel.tasklet.h", consumer["inputs"])
        self.assertIn(f"$(B)/{API}/libapi.a", lib.node_by_output(graph, "$(B)/native/native")["inputs"])
        self.assertFalse(any(PY_BINARY in n["inputs"] for n in graph["graph"]))

    def test_shared_lexical_call_in_both_selected_python_contexts_and_root_orders(self):
        for targets in (("courier2", "courier3"), ("courier3", "courier2")):
            with self.subTest(targets=targets):
                files = tasklet_world("ONLY_TAGS(PY_PROTO PY3_PROTO)\n")
                remove_modules(files, CPP_TOOL)
                graph = lib.make(files, *targets)
                tool = lib.node_by_output(graph, PY_BINARY)
                for version, target in (("py2", "courier2"), ("py3", "courier3")):
                    stem = f"$(B)/{MODULE}/parcel__int{version}__"
                    suffixes = ["_pb2.py", "_tasklet.py", "_sbtask.py", "_pb2.pyi"]
                    producer = lib.node_by_output(graph, stem + "_sbtask.py")
                    self.assertEqual(producer["outputs"], [stem + s for s in suffixes])
                    command = args(producer)
                    self.assertEqual(command[1:9], [WRAPPER, "--py_ver", version, "--suffixes", *suffixes])
                    self.assertIn("--plugin=protoc-gen-tasklet_py=" + PY_BINARY, command)
                    self.assertIn("--tasklet_py_out=$(B)/", command)
                    self.assertIn(PY_BINARY, producer["inputs"])
                    self.assertIn(MYPY, producer["inputs"])
                    self.assertIn(f"$(S)/{MODULE}/parcel.proto", producer["inputs"])
                    self.assertIn(tool["uid"], producer["foreign_deps"]["tool"])
                    self.assertEqual(sorted(producer["deps"]), sorted(producer["foreign_deps"]["tool"]))
                    for suffix in suffixes:
                        self.assertEqual(producer["kv"][f"ext_out_name_for_parcel__int{version}__{suffix}"], "parcel" + suffix)
                    for suffix in suffixes[:3]:
                        output = stem + suffix
                        bytecode = next(n for n in graph["graph"] if output in args(n)
                                        and any(o.endswith(".yapyc" if version == "py2" else ".yapyc3") for o in n["outputs"]))
                        self.assertIn(stem + "_pb2.py", bytecode["inputs"])
                        self.assertIn(producer["uid"], bytecode["deps"])
                        self.assertNotIn(f"$(S)/{MODULE}/parcel.proto", bytecode["inputs"])
                        key = ("resfs/src//py_modules/delivery.messages.parcel" + suffix.removesuffix(".py") + "="
                               if version == "py2" else "resfs/src/resfs/file/py/delivery/messages/parcel" + suffix + "=")
                        resource = next(n for n in graph["graph"] if key + output.removeprefix("$(B)/") in args(n))
                        self.assertIn(stem + "_pb2.py", resource["inputs"])
                        self.assertIn(producer["uid"], resource["deps"])
                    link = lib.node_by_output(graph, f"$(B)/{target}/{target}")
                    for directory in (API, *RUNTIMES):
                        self.assertTrue(any(i.startswith(f"$(B)/{directory}/") for i in link["inputs"]), directory)
                    lib.node_by_output(graph, f"$(B)/{API}/parcel__int{version}___pb2.py")
                self.assertFalse(any(CPP_BINARY in n["inputs"] for n in graph["graph"]))

    def test_python_optimization_keeps_native_sibling_composition(self):
        graph = lib.make(tasklet_world("EXCLUDE_TAGS(GO_PROTO JAVA_PROTO)\n", optimize=True), "courier3")
        lib.node_by_output(graph, f"$(B)/{MODULE}/parcel.tasklet.h")
        lib.node_by_output(graph, f"$(B)/{MODULE}/parcel__intpy3___tasklet.py")
        link = lib.node_by_output(graph, "$(B)/courier3/courier3")
        self.assertIn(f"$(B)/{MODULE}/libdelivery.a", link["inputs"])
        self.assertIn(f"$(B)/{MODULE}/libpy3delivery.global.a", link["inputs"])

    def test_ordinary_python_proto_caller_uses_the_same_composition(self):
        files = tasklet_world()
        remove_modules(files, CPP_TOOL)
        files["courier3/ya.make"] = (
            f"PY3_PROGRAM()\n{lib.BARE_MODULE}NO_CHECK_IMPORTS()\nNO_MYPY()\n"
            f"INCLUDE(${{ARCADIA_ROOT}}/{INCLUDE})\nPY_SRCS(letter.proto)\nEND()\n"
        )
        files["courier3/letter.proto"] = 'syntax = "proto2";\nmessage Letter {}\n'
        graph = lib.make(files, "courier3")
        producer = lib.node_by_output(graph, "$(B)/courier3/letter__intpy3___sbtask.py")
        self.assertEqual(producer["outputs"], [f"$(B)/courier3/letter__intpy3__{s}" for s in ("_pb2.py", "_tasklet.py", "_sbtask.py")])
        self.assertIn(PY_BINARY, producer["inputs"])
        link = lib.node_by_output(graph, "$(B)/courier3/courier3")
        for directory in (API, *RUNTIMES):
            self.assertTrue(any(i.startswith(f"$(B)/{directory}/") for i in link["inputs"]), directory)

    def test_descriptor_and_no_proto_callers_do_not_resolve_plugin_tools_or_python_deps(self):
        for target in ("descriptions", "plain"):
            with self.subTest(target=target):
                files = tasklet_world("ONLY_TAGS(DESC_PROTO)\n")
                remove_modules(files, CPP_TOOL, PY_TOOL, *RUNTIMES)
                files["descriptions/ya.make"] = f"PROTO_DESCRIPTIONS()\nPEERDIR({MODULE})\nEND()\n"
                files["plain/ya.make"] = (
                    f"PROGRAM()\n{lib.BARE_MODULE}INCLUDE(${{ARCADIA_ROOT}}/{INCLUDE})\nEND()\n"
                )
                graph = lib.make(files, target)
                if target == "descriptions":
                    lib.node_by_output(graph, f"$(B)/{MODULE}/parcel.proto.desc")
                else:
                    self.assertIn(f"$(B)/{API}/libapi.a", lib.node_by_output(graph, "$(B)/plain/plain")["inputs"])
                self.assertFalse(any("tasklet_py" in a or "tasklet_cpp" in a for n in graph["graph"] for a in args(n)))

    def test_selected_go_and_java_variants_do_not_resolve_sibling_plugins(self):
        from tst.make.test_proto_targets import target_files

        for tag, output in (("GO_PROTO", "parcel.pb.go"), ("JAVA_PROTO", "delivery.jar")):
            with self.subTest(tag=tag):
                files = {**target_files(), **tasklet_world(f"ONLY_TAGS({tag})\n")}
                remove_modules(files, CPP_TOOL, PY_TOOL, *RUNTIMES)
                graph = lib.make(files, MODULE)
                lib.node_by_output(graph, f"$(B)/{MODULE}/{output}")
                self.assertFalse(any("tasklet_py" in a or "tasklet_cpp" in a for n in graph["graph"] for a in args(n)))
                self.assertFalse(any(o.endswith((".tasklet.h", "_tasklet.py", "_sbtask.py"))
                                     for n in graph["graph"] for o in n["outputs"]))

    def test_tasklet_rejects_effective_arguments(self):
        files = tasklet_world("ONLY_TAGS(CPP_PROTO)\n")
        files[INCLUDE] = "SET(EXTRA surplus second)\nTASKLET(${EXTRA})\n"
        result = lib.make_process(files, "native")
        self.assertEqual(result.returncode, 1)
        self.assertIn("TASKLET expects no arguments, got 2", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
