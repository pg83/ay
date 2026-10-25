import unittest

from tst import lib
from tst.make.resource_dependencies import fetched_uri


PYTHON = "$(B)/resources/YMAKE_PYTHON3/bin/python3"
CLANG = "$(B)/resources/CLANG20"


def llvm_bc_files(body, sources):
    files = {
        "mod/ya.make": (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nUSE_LLVM_BC20()\n"
            + body + "END()\n"
        ),
        "build/platform/clang/ya.make": (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:1)\nEND()\n"
        ),
        "build/platform/python/ymake_python3/ya.make": (
            "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:2)\nEND()\n"
        ),
        **sources,
    }
    lib.tool_program(files, "tools/rescompiler", "rescompiler")
    lib.tool_program(files, "tools/rescompressor", "rescompressor")
    return files


def make(files):
    return lib.make(files, "mod", "--target-platform", "default-linux-x86_64")


def nodes_of_kind(graph, kind):
    return [node for node in graph["graph"] if node["kv"].get("p") == kind]


class LlvmBcTest(unittest.TestCase):
    def test_root_bitcode_can_feed_another_bitcode_pipeline(self):
        files = llvm_bc_files(
            "LLVM_BC(base.cpp NAME first)\nLLVM_BC(base.cpp.bc NAME second)\n",
            {"base.cpp": "int value;\n"},
        )
        files["ya.make"] = files.pop("mod/ya.make")
        graph = lib.make(files, ".", "--target-platform", "default-linux-x86_64")
        first, second = nodes_of_kind(graph, "BC")
        self.assertIn(first["outputs"][0], second["inputs"])
        self.assertIn(first["uid"], second["deps"])
        self.assertNotIn("$(S)/base.cpp.bc", second["inputs"])

    def test_generated_root_source_keeps_its_producer(self):
        files = llvm_bc_files(
            "RUN_PROGRAM(tools/seed OUT_NOAUTO ${ARCADIA_BUILD_ROOT}/seed.cpp)\n"
            "LLVM_BC(seed.cpp NAME generated)\n", {},
        )
        files["ya.make"] = files.pop("mod/ya.make")
        lib.tool_program(files, "tools/seed", "seed")
        graph = lib.make(files, ".", "--target-platform", "default-linux-x86_64")
        source = lib.node_by_output(graph, "$(B)/seed.cpp")
        compiled = lib.node_by_output(graph, "$(B)/seed.cpp.bc")
        self.assertIn(source["outputs"][0], compiled["inputs"])
        self.assertIn(source["uid"], compiled["deps"])
        self.assertNotIn("$(S)/seed.cpp", compiled["inputs"])

    def test_relative_generated_source_keeps_its_producer(self):
        files = llvm_bc_files(
            "RUN_PROGRAM(tools/seed OUT_NOAUTO seed.cpp)\n"
            "LLVM_BC(./seed.cpp NAME generated)\n", {},
        )
        lib.tool_program(files, "tools/seed", "seed")
        graph = make(files)
        source = lib.node_by_output(graph, "$(B)/mod/seed.cpp")
        compiled, = nodes_of_kind(graph, "BC")
        self.assertIn(source["outputs"][0], compiled["inputs"])
        self.assertIn(source["uid"], compiled["deps"])
        self.assertNotIn("$(S)/mod/seed.cpp", compiled["inputs"])

    def test_c_bitcode_uses_c_compiler_and_platform_sdk_flags(self):
        graph = lib.make(llvm_bc_files(
            "LLVM_BC(a.c NAME c)\nNO_COMPILER_WARNINGS()\n", {"mod/a.c": ""},
        ), "mod", "--target-platform", "default-linux-x86_64", opensource=False)
        args = lib.node_by_output(graph, "$(B)/mod/a.c.bc")["cmds"][0]["cmd_args"]
        self.assertIn(f"{CLANG}/bin/clang", args)
        self.assertNotIn("-Wno-unknown-warning-option", args)
        self.assertFalse(any(a.startswith("-std=c++") for a in args))
        self.assertIn("--sysroot=$(B)/resources/OS_SDK_ROOT", args)
        self.assertIn("-B$(B)/resources/OS_SDK_ROOT/usr/bin", args)

    def test_direct_compile_is_lazy_and_tracks_headers_and_options(self):
        files = llvm_bc_files(
            "SET(LLVM_OPTS -O1)\n"
            "LLVM_COMPILE_CXX(a.cpp named.bc -DLOCAL)\n"
            "LLVM_COMPILE_CXX(unused.cpp unused.bc)\n"
            "RESOURCE(named.bc named)\nNO_COMPILER_WARNINGS()\n",
            {"mod/a.cpp": '#include "a.h"\n', "mod/a.h": "", "mod/unused.cpp": ""},
        )
        files["library/cpp/resource/ya.make"] = f"LIBRARY()\n{lib.BARE_MODULE}END()\n"
        graph = make(files)
        node = lib.node_by_output(graph, "$(B)/mod/named.bc")
        self.assertEqual(node["kv"], {"p": "BC", "pc": "light-green"})
        self.assertEqual(node["cmds"][0]["cmd_args"][-8:], [
            "-Wno-unknown-warning-option", "-O1", "-emit-llvm", "-c",
            "$(S)/mod/a.cpp", "-o", "$(B)/mod/named.bc", "-DLOCAL",
        ])
        self.assertIn("$(S)/mod/a.h", node["inputs"])
        self.assertFalse(any("$(B)/mod/unused.bc" in n["outputs"] for n in graph["graph"]))

    def test_sources_compile_link_optimize_and_embed_as_resource(self):
        graph = make(llvm_bc_files(
            "PEERDIR(contrib/libs/googleapis-common-protos)\n"
            "LLVM_BC(a.cpp b.c NAME foo SYMBOLS x y SUFFIX .s1)\n",
            {
                "mod/a.cpp": '#include "a.h"\n',
                "mod/a.h": "",
                "mod/b.c": '#include "build/scripts/fs_tools.py"\n',
                "build/scripts/fs_tools.py": "",
                "contrib/libs/googleapis-common-protos/ya.make": (
                    "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                    "ADDINCL(\n"
                    "  GLOBAL ${ARCADIA_BUILD_ROOT}/contrib/libs/googleapis-common-protos\n"
                    "  GLOBAL contrib/libs/googleapis-common-protos/inc\n"
                    ")\nEND()\n"
                ),
                "contrib/libs/googleapis-common-protos/inc/x.h": "",
            },
        ))

        compile_a = lib.node_by_output(graph, "$(B)/mod/a.cpp.s1.bc")
        self.assertEqual(compile_a["kv"], {"p": "BC", "pc": "light-green"})
        args = compile_a["cmds"][0]["cmd_args"]
        self.assertEqual(args[:6], [
            PYTHON, "$(S)/build/scripts/clang_wrapper.py", "no",
            f"{CLANG}/bin/clang++", "-I$(B)", "-I$(S)",
        ])
        self.assertEqual(args[6:8], [
            "-I$(B)/contrib/libs/googleapis-common-protos",
            "-I$(S)/contrib/libs/googleapis-common-protos/inc",
        ])
        self.assertEqual(args[-6:], [
            "-Wno-unknown-warning-option", "-emit-llvm", "-c",
            "$(S)/mod/a.cpp", "-o", "$(B)/mod/a.cpp.s1.bc",
        ])
        self.assertEqual(compile_a["inputs"], [
            "$(S)/build/scripts/clang_wrapper.py", "$(S)/mod/a.cpp", "$(S)/mod/a.h",
        ])
        compile_b = lib.node_by_output(graph, "$(B)/mod/b.c.s1.bc")

        merge = lib.node_by_output(graph, "$(B)/mod/foo_merged.s1.bc")
        self.assertEqual(merge["kv"], {"p": "LD", "pc": "light-red"})
        self.assertEqual(merge["cmds"][0]["cmd_args"], [
            f"{CLANG}/bin/llvm-link",
            "$(B)/mod/a.cpp.s1.bc", "$(B)/mod/b.c.s1.bc",
            "-o", "$(B)/mod/foo_merged.s1.bc",
        ])
        self.assertEqual(merge["inputs"], [
            "$(B)/mod/a.cpp.s1.bc", "$(B)/mod/b.c.s1.bc",
            "$(S)/build/scripts/fs_tools.py",
        ])
        self.assertIn(compile_a["uid"], merge["deps"])
        self.assertIn(compile_b["uid"], merge["deps"])

        optimize = lib.node_by_output(graph, "$(B)/mod/foo_optimized.s1.bc")
        self.assertEqual(optimize["kv"], {"p": "OP", "pc": "yellow"})
        self.assertEqual(optimize["cmds"][0]["cmd_args"], [
            PYTHON, "$(S)/build/scripts/llvm_opt_wrapper.py", f"{CLANG}/bin/opt",
            "$(B)/mod/foo_merged.s1.bc", "-o", "$(B)/mod/foo_optimized.s1.bc",
            "-internalize-public-api-list=x#y",
            '-passes="default<O2>,globalopt,globaldce,internalize"',
        ])
        self.assertEqual(optimize["inputs"][:2], [
            "$(B)/mod/foo_merged.s1.bc", "$(S)/build/scripts/llvm_opt_wrapper.py",
        ])
        self.assertNotIn('$(S)/mod/a.cpp', optimize['inputs'])
        self.assertNotIn('$(S)/mod/a.h', optimize['inputs'])
        self.assertNotIn('$(S)/mod/b.c', optimize['inputs'])
        self.assertIn(merge["uid"], optimize["deps"])

        objcopy = lib.node_by_output_prefix(graph, "$(B)/mod/objcopy_")
        args = objcopy["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--inputs") + 1], "$(B)/mod/foo_optimized.s1.bc")
        self.assertEqual(args[args.index("--keys") + 1], "L2xsdm1fYmMvZm9v")

    def test_symbols_are_values_even_in_upper_case(self):
        # yql/essentials/udfs/common/math lists the symbol EIR: build/plugins/
        # llvm_bc.py takes every SYMBOLS value up to the next keyword.
        graph = make(llvm_bc_files("LLVM_BC(a.cpp NAME foo SYMBOLS PiIR EIR)\n", {"mod/a.cpp": ""}))
        optimize = lib.node_by_output(graph, "$(B)/mod/foo_optimized.bc")
        self.assertIn("-internalize-public-api-list=PiIR#EIR", optimize["cmds"][0]["cmd_args"])

    def test_bitcode_names_take_the_module_object_suffix(self):
        # build/plugins/llvm_bc.py: obj_suf = SUFFIX or $OBJ_SUF; the ydb
        # yql/essentials/udfs/common/math static UDF (OBJ_SUF=.udfs) makes
        # Math_merged.udfs.bc and math_ir.cpp.udfs.bc.
        files = llvm_bc_files("", {"mod/a.cpp": ""})
        files["mod/ya.make"] = (
            "YQL_LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nUSE_LLVM_BC20()\n"
            "LLVM_BC(a.cpp NAME foo)\nEND()\n"
        )
        files["p/ya.make"] = "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nPEERDIR(mod)\nEND()\n"
        graph = lib.make(files, "p", "--target-platform", "default-linux-x86_64")
        for output in ("$(B)/mod/a.cpp.abi_stable.bc", "$(B)/mod/foo_merged.abi_stable.bc", "$(B)/mod/foo_optimized.abi_stable.bc"):
            lib.node_by_output(graph, output)

    def test_copied_source_keeps_root_relative_bitcode_path(self):
        graph = make(llvm_bc_files(
            "COPY_FILE(tpl.txt gen.cpp)\nLLVM_BC(gen.cpp NAME bar)\n",
            {"mod/tpl.txt": ""},
        ))
        copy = lib.node_by_output(graph, "$(B)/mod/gen.cpp")
        compile_node = lib.node_by_output(graph, "$(B)/gen.cpp.bc")
        self.assertEqual(compile_node["cmds"][0]["cmd_args"][-3:], [
            "$(B)/mod/gen.cpp", "-o", "$(B)/gen.cpp.bc",
        ])
        self.assertEqual(compile_node["inputs"], [
            "$(S)/build/scripts/clang_wrapper.py", "$(B)/mod/gen.cpp",
        ])
        self.assertIn(copy["uid"], compile_node["deps"])
        merge = lib.node_by_output(graph, "$(B)/mod/bar_merged.bc")
        self.assertEqual(merge["inputs"], ["$(B)/gen.cpp.bc"])
        optimize = lib.node_by_output(graph, "$(B)/mod/bar_optimized.bc")
        self.assertEqual(
            optimize["cmds"][0]["cmd_args"][-1],
            '-passes="default<O2>,globalopt,globaldce"',
        )

    def test_generate_machine_code_registers_no_bitcode_nodes(self):
        graph = make(llvm_bc_files(
            "LLVM_BC(a.cpp NAME foo GENERATE_MACHINE_CODE)\n", {"mod/a.cpp": ""},
        ))
        for kind in ("BC", "OP"):
            self.assertEqual(nodes_of_kind(graph, kind), [])
        self.assertFalse(any(
            output.endswith(".bc")
            for node in graph["graph"] for output in node["outputs"]
        ))

    def test_platform_cxxflags_and_unresolved_source_name(self):
        files = llvm_bc_files("LLVM_BC(a.cpp missing.cpp NAME foo)\n", {
            "mod/a.cpp": "",
            "build/internal/ya.conf": '[flags]\nCXXFLAGS = "-DFROM_INTERNAL_CONF"\n',
        })
        graph = make(files)
        compile_a = lib.node_by_output(graph, "$(B)/mod/a.cpp.bc")
        args = compile_a["cmds"][0]["cmd_args"]
        self.assertEqual(args.count("-DFROM_INTERNAL_CONF"), 1)
        self.assertLess(args.index("-DFROM_INTERNAL_CONF"), args.index("-emit-llvm"))
        missing = lib.node_by_output(graph, "$(B)/missing.cpp.bc")
        self.assertEqual(missing["inputs"], [
            "$(S)/build/scripts/clang_wrapper.py", "$(S)/mod/missing.cpp",
        ])
        merge = lib.node_by_output(graph, "$(B)/mod/foo_merged.bc")
        self.assertEqual(merge["inputs"], ["$(B)/mod/a.cpp.bc", "$(B)/missing.cpp.bc"])

    def test_root_module_uses_generated_source_registered_by_codegen(self):
        files = llvm_bc_files("", {})
        del files["mod/ya.make"]
        files["ya.make"] = (
            "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nUSE_LLVM_BC20()\n"
            "BASE_CODEGEN(tools/codegen gen)\nLLVM_BC(gen.cpp NAME foo)\nEND()\n"
        )
        files["gen.in"] = ""
        lib.tool_program(files, "tools/codegen", "codegen")
        python_module = "build/platform/python/ymake_python3/ya.make"
        files[python_module] = files[python_module].replace(
            "RESOURCES_LIBRARY()\n", "RESOURCES_LIBRARY()\nNO_YMAKE_PYTHON3()\n")
        files["library/cpp/resource/ya.make"] = "LIBRARY()\n" + lib.BARE_MODULE + "END()\n"
        files["library/cpp/resource/resource.h"] = ""
        graph = lib.make(files, ".", "--target-platform", "default-linux-x86_64")
        codegen = lib.node_by_output(graph, "$(B)/gen.cpp")
        self.assertEqual(codegen["outputs"], ["$(B)/gen.cpp", "$(B)/gen.h"])
        self.assertEqual(codegen["inputs"], ["$(B)/tools/codegen/codegen", "$(S)/gen.in"])
        generated = codegen["outputs"][0]
        compile_node = lib.node_by_output(graph, "$(B)/gen.cpp.bc")
        self.assertEqual(compile_node["inputs"][1], generated)
        self.assertEqual(compile_node["cmds"][0]["cmd_args"][-3:], [
            generated, "-o", "$(B)/gen.cpp.bc",
        ])
        self.assertIn(codegen["uid"], compile_node["deps"])
        merged = lib.node_by_output(graph, "$(B)/foo_merged.bc")
        self.assertEqual(merged["inputs"], [compile_node["outputs"][0]])
        self.assertIn(compile_node["uid"], merged["deps"])
        optimized = lib.node_by_output(graph, "$(B)/foo_optimized.bc")
        self.assertEqual(optimized["inputs"], [merged["outputs"][0], "$(S)/build/scripts/llvm_opt_wrapper.py"])
        self.assertIn(merged["uid"], optimized["deps"])
        resource = lib.node_by_output(graph, "$(B)/objcopy_72392759cbf08fee667cb14af3.o")
        self.assertEqual(resource["kv"]["p"], "PY")
        self.assertCountEqual(resource["inputs"], [
            optimized["outputs"][0], "$(B)/tools/rescompiler/rescompiler",
            "$(B)/tools/rescompressor/rescompressor", "$(S)/build/scripts/objcopy.py",
        ])
        resource_args = resource["cmds"][0]["cmd_args"]
        self.assertEqual(resource_args[resource_args.index("--output_obj") + 1], resource["outputs"][0])
        self.assertEqual(resource_args[resource_args.index("--inputs") + 1], optimized["outputs"][0])
        dependencies = [optimized["uid"]]
        for tool in ("rescompiler", "rescompressor"):
            dependencies.append(lib.node_by_output(graph, "$(B)/tools/" + tool + "/" + tool)["uid"])
        for name, uri in (("YMAKE_PYTHON3", "sbr:2"), ("CLANG20", "sbr:1")):
            self.assertEqual(fetched_uri(graph, name), uri)
            dependencies.append(lib.node_by_output(graph, "$(B)/resources/" + name)["uid"])
        self.assertCountEqual(resource["deps"], dependencies)
        self.assertIn(optimized["uid"], resource["deps"])
        archive = lib.node_by_output(graph, "$(B)/lib.global.a")
        self.assertIn(resource["outputs"][0], archive["inputs"])
        self.assertIn(resource["uid"], archive["deps"])
        self.assertEqual(graph["result"], [archive["uid"]])


if __name__ == "__main__":
    unittest.main(verbosity=2)
