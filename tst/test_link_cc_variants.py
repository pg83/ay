import json
import os
import tempfile
import unittest
from pathlib import Path

import lib


NO_PLATFORM = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"


def module(kind, body):
    return f"{kind}\n{NO_PLATFORM}{body}END()\n"


def fixture():
    files = {
        "m/ya.make": module(
            "LIBRARY()",
            "SRCDIR(m/extra)\n"
            "SRCS(sub/f.cpp ../up/u.cpp __x/y.cpp e.cpp)\n"
            "SRC(sub/flat.cpp -DFLAT)\n"
            "SRC(c/flat.c -DFLATC)\n"
            "SRC_C_NO_LTO(n.c)\n"
            "NO_WSHADOW()\n",
        ),
        "m/sub/f.cpp": "int f;\n",
        "up/u.cpp": "int u;\n",
        "m/__x/y.cpp": "int y;\n",
        "m/extra/e.cpp": "int e;\n",
        "m/sub/flat.cpp": "int flat;\n",
        "m/c/flat.c": "int flatc;\n",
        "m/n.c": "int n;\n",
        "nw/ya.make": module("LIBRARY()", "SRCS(a.c b.cpp)\nNO_COMPILER_WARNINGS()\n"),
        "nw/a.c": "int a;\n",
        "nw/b.cpp": "int b;\n",
        "py/ya.make": module("PY23_NATIVE_LIBRARY()", "SRCS(p.cpp)\n"),
        "py/p.cpp": "int p;\n",
        "udf/ya.make": module("YQL_UDF_YDB(u)", "SRCS(u.cpp)\n"),
        "udf/u.cpp": "int u;\n",
        "yql/essentials/public/udf/ya.make": module("LIBRARY()", ""),
        "yql/essentials/public/udf/support/ya.make": module("LIBRARY()", ""),
        "app/ya.make": module("PROGRAM()", "SRCS(main.cpp)\nPEERDIR(py udf)\n"),
        "app/main.cpp": "int main(){return 0;}\n",
    }
    lib.tool_program(files, "tools/archiver", "archiver")
    files["tools/archiver/ya.make"] = files["tools/archiver/ya.make"].replace(
        "SRCS(main.cpp)\n", "SRCS(main.cpp)\nPEERDIR(py udf)\n"
    )
    files["user/ya.make"] = module(
        "LIBRARY()", "SRCS(use.cpp)\nARCHIVE(NAME data.inc payload.lst)\n"
    )
    files["user/use.cpp"] = '#include "data.inc"\n'
    files["user/payload.lst"] = "row\n"
    return files


def cc_args(graph, output):
    return lib.node_by_output(graph, output)["cmds"][0]["cmd_args"]


def make_with_env(files, target, extra_env):
    with tempfile.TemporaryDirectory(prefix="ay-link-cc-") as directory:
        root = Path(directory)
        (root / ".arcadia.root").touch()
        (root / "ya.conf").write_text(
            '[flags]\nOPENSOURCE = "yes"\n\n[host_platform_flags]\nOPENSOURCE = "yes"\n'
        )
        for relative, content in files.items():
            path = root / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content)
        env = {
            key: value
            for key, value in os.environ.items()
            if key not in lib.TOOLCHAIN_ENV_VARS
        }
        env.update(extra_env)
        result = lib.run(
            "make", "-j0", "-G", "--sandboxing",
            "--source-root", root,
            "--target-platform", "default-linux-aarch64",
            "--host-platform", "default-linux-x86_64",
            target,
            env=env,
        )
        return json.loads(result.stdout)


class CcVariantsTest(unittest.TestCase):
    def test_object_paths_for_nested_parent_srcdir_and_flat_sources(self):
        graph = lib.make(fixture(), "m")
        objects = [
            node["outputs"][0]
            for node in graph["graph"]
            if node["kv"]["p"] == "CC"
        ]
        self.assertEqual(
            objects,
            [
                "$(B)/m/_/sub/f.cpp.o",
                "$(B)/m/__/up/u.cpp.o",
                "$(B)/m/__x/y.cpp.o",
                "$(B)/m/_/extra/e.cpp.o",
                "$(B)/m/sub/flat.cpp.o",
                "$(B)/m/c/flat.c.o",
                "$(B)/m/n.c.o",
            ],
        )
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/m/__/up/u.cpp.o")["inputs"],
            ["$(S)/up/u.cpp"],
        )
        self.assertEqual(
            lib.node_by_output(graph, "$(B)/m/_/extra/e.cpp.o")["inputs"],
            ["$(S)/m/extra/e.cpp"],
        )
        flat = cc_args(graph, "$(B)/m/sub/flat.cpp.o")
        self.assertIn("-DFLAT", flat)
        self.assertIn("-std=c++20", flat)
        flat_c = cc_args(graph, "$(B)/m/c/flat.c.o")
        self.assertIn("-DFLATC", flat_c)
        self.assertNotIn("-std=c++20", flat_c)
        for output in ("$(B)/m/n.c.o", "$(B)/m/_/sub/f.cpp.o"):
            with self.subTest(output=output):
                self.assertIn("-Wno-shadow", cc_args(graph, output))

    def test_no_compiler_warnings_for_c_and_cxx(self):
        graph = lib.make(fixture(), "nw")
        for output in ("$(B)/nw/a.c.o", "$(B)/nw/b.cpp.o"):
            with self.subTest(output=output):
                args = cc_args(graph, output)
                self.assertIn("-Wno-everything", args)
                self.assertNotIn("-Wall", args)
                self.assertNotIn("-Woverloaded-virtual", args)

    def test_python_native_and_udf_object_suffixes(self):
        graph = lib.make(fixture(), "app")
        outputs = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertIn("$(B)/py/p.cpp.py3.o", outputs)
        self.assertIn("$(B)/py/libpy3cpy.a", outputs)
        self.assertIn("$(B)/udf/u.cpp.udfs.o", outputs)
        self.assertIn("$(B)/udf/libu.global.a", outputs)

        graph = lib.make(fixture(), "user")
        outputs = {out for node in graph["graph"] for out in node["outputs"]}
        self.assertIn("$(B)/py/p.cpp.py3.pic.o", outputs)
        self.assertIn("$(B)/udf/u.cpp.udfs.pic.o", outputs)
        archiver = lib.node_by_output(graph, "$(B)/tools/archiver/archiver")
        link = " ".join(archiver["cmds"][2]["cmd_args"])
        self.assertIn("udf/libu.global.a", link)
        self.assertIn("py/libpy3cpy.a", link)

    def test_target_cxxflags_environment_is_appended_to_cxx_only(self):
        graph = make_with_env(
            fixture(), "nw", {"CXXFLAGS": "-DFROM_ENV_CXX", "CFLAGS": "-DFROM_ENV_C"}
        )
        cxx = cc_args(graph, "$(B)/nw/b.cpp.o")
        c = cc_args(graph, "$(B)/nw/a.c.o")
        self.assertIn("-DFROM_ENV_CXX", cxx)
        self.assertNotIn("-DFROM_ENV_CXX", c)
        self.assertIn("-DFROM_ENV_C", c)
        self.assertIn("-DFROM_ENV_C", cxx)

    def test_raw_source_names_are_normalized_for_cuda_c_and_asm(self):
        files = {
            "k/ya.make": module(
                "LIBRARY()",
                "SRCS(./a.cu ../k/b.cu sub/../c.cu sub/d.cu c.c sub/k.S)\n"
                "CONLYFLAGS(-DOWN_CONLY)\n",
            ),
            "k/a.cu": "\n",
            "k/b.cu": "\n",
            "k/c.cu": "\n",
            "k/sub/d.cu": "\n",
            "k/c.c": "int c;\n",
            "k/sub/k.S": "\n",
        }
        lib.tool_program(files, "tools/mtime0", "mtime0")
        lib.tool_program(files, "tools/custom_pid", "custom_pid")
        graph = lib.make(files, "k")
        expected = [
            ("CU", "$(B)/k/a.cu.o", "$(S)/k/a.cu"),
            ("CU", "$(B)/k/__/k/b.cu.o", "$(S)/k/b.cu"),
            ("CU", "$(B)/k/c.cu.o", "$(S)/k/c.cu"),
            ("CU", "$(B)/k/_/sub/d.cu.o", "$(S)/k/sub/d.cu"),
            ("CC", "$(B)/k/c.c.o", "$(S)/k/c.c"),
            ("AS", "$(B)/k/_/sub/k.S.o", "$(S)/k/sub/k.S"),
        ]
        objects = [
            (node["kv"]["p"], node["outputs"][0])
            for node in graph["graph"]
            if node["kv"]["p"] in ("CU", "CC", "AS")
            and node["outputs"][0].startswith("$(B)/k/")
        ]
        self.assertEqual(objects, [(kind, output) for kind, output, _ in expected])
        for _, output, source in expected:
            with self.subTest(output=output):
                self.assertIn(source, lib.node_by_output(graph, output)["inputs"])
        c_args = cc_args(graph, "$(B)/k/c.c.o")
        self.assertEqual(c_args[-2:], ["-DOWN_CONLY", "$(S)/k/c.c"])

    def test_raw_asm_source_names_map_into_module_output_dir(self):
        files = {
            "k/ya.make": module(
                "LIBRARY()", "SRCS(./a.S ./sub/b.S ../k/c.S)\nNO_WSHADOW()\n"
            ),
            "k/a.S": "\n",
            "k/sub/b.S": "\n",
            "k/c.S": "\n",
        }
        graph = lib.make(files, "k")
        for output, source in (
            ("$(B)/k/a.S.o", "$(S)/k/a.S"),
            ("$(B)/k/_/sub/b.S.o", "$(S)/k/sub/b.S"),
            ("$(B)/k/c.S.o", "$(S)/k/c.S"),
        ):
            with self.subTest(output=output):
                node = lib.node_by_output(graph, output)
                self.assertEqual(node["kv"]["p"], "AS")
                self.assertIn(source, node["inputs"])
                self.assertIn("-Wno-shadow", node["cmds"][0]["cmd_args"])

    def test_llvm_bc_compile_uses_cxx_standard_and_warning_bundles(self):
        for warnings, expected in (("", "-Woverloaded-virtual"), ("NO_COMPILER_WARNINGS()\n", "-Wno-everything")):
            with self.subTest(warnings=warnings):
                files = {
                    "bc/ya.make": module(
                        "LIBRARY()",
                        "SRCS(x.cpp)\nUSE_LLVM_BC20()\n"
                        "LLVM_BC(f.cpp NAME fbc SYMBOLS a b)\nPEERDIR(res peer)\n"
                        f"CXXFLAGS(-DOWNCXX)\n{warnings}",
                    ),
                    "bc/x.cpp": "int x;\n",
                    "bc/f.cpp": "int f;\n",
                    "res/ya.make": (
                        "RESOURCES_LIBRARY()\n"
                        "DECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:1)\nEND()\n"
                    ),
                    "peer/ya.make": module("LIBRARY()", "CXXFLAGS(GLOBAL -DPEERCXX)\n"),
                }
                lib.tool_program(files, "tools/rescompiler", "rescompiler")
                lib.tool_program(files, "tools/rescompressor", "rescompressor")
                graph = lib.make(files, "bc")
                args = cc_args(graph, "$(B)/bc/f.cpp.bc")
                self.assertEqual(args[3], "$(B)/resources/CLANG20/bin/clang++")
                self.assertEqual(args[-5:], ["-emit-llvm", "-c", "$(S)/bc/f.cpp", "-o", "$(B)/bc/f.cpp.bc"])
                std = args.index("-std=c++20")
                self.assertIn(expected, args[std + 1:])
                self.assertLess(std, args.index("-DOWNCXX"))
                self.assertIn("-DPEERCXX", args)

    def test_generated_source_of_another_module_maps_outside_its_dir(self):
        files = {
            "other/ya.make": module("LIBRARY()", "COPY_FILE(g.in gen.cpp)\n"),
            "other/g.in": "int gen;\n",
            "m/ya.make": module(
                "LIBRARY()",
                "PEERDIR(other)\nSRCS(${ARCADIA_BUILD_ROOT}/other/gen.cpp x.cpp)\n",
            ),
            "m/x.cpp": "int x;\n",
        }
        graph = lib.make(files, "m")
        copy = lib.node_by_output(graph, "$(B)/other/gen.cpp")
        compile_node = lib.node_by_output(graph, "$(B)/m/__/other/gen.cpp.o")
        self.assertEqual(compile_node["inputs"], ["$(B)/other/gen.cpp"])
        self.assertEqual(compile_node["deps"], [copy["uid"]])
        archive = lib.node_by_output(graph, "$(B)/m/libm.a")
        self.assertEqual(archive["inputs"][:2], ["$(B)/m/x.cpp.o", "$(B)/m/__/other/gen.cpp.o"])

    def test_generated_source_at_an_ancestor_path_of_the_module(self):
        # In the build tree x/a.cpp is a generated file; in the source tree it
        # is the directory of the module compiling it.
        files = {
            "x/ya.make": "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nRUN_PROGRAM(tools/gen OUT a.cpp)\nEND()\n",
            "x/a.cpp/m/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(x)\n"
                "SRCS(${ARCADIA_BUILD_ROOT}/x/a.cpp)\nEND()\n"
            ),
        }
        lib.tool_program(files, "tools/gen", "gen")
        graph = lib.make(files, "x/a.cpp/m")
        self.assertEqual(lib.node_by_output(graph, "$(B)/x/a.cpp/m/__.o")["inputs"], ["$(B)/x/a.cpp"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
