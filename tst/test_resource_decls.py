import json
import os
import unittest
from unittest import mock

import lib


BARE = "NO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
PLATFORM_BUNDLE = {
    "linux-x86_64": "sbr:1",
    "linux-aarch64": "sbr:2",
    "darwin-arm64": "sbr:3",
    "darwin-x86_64": "sbr:4",
    "win32-x86_64": "sbr:5",
}


def bundle_json(entries):
    return json.dumps({
        "by_platform": {key: {"uri": uri} for key, uri in entries.items()},
    })


def resources_library(body):
    return f"RESOURCES_LIBRARY()\n{body}END()\n"


def fetches(graph):
    result = {}
    for node in graph["graph"]:
        if node["kv"]["p"] != "FT":
            continue
        [output] = node["outputs"]
        if output in result:
            raise AssertionError(f"duplicate fetch node for {output}")
        result[output] = node["cmds"][0]["cmd_args"][1:]
    return result


def fetched_uri(graph, name):
    output = f"$(B)/resources/{name}"
    args = fetches(graph)[output]
    self_check = args[:3] + args[4:]
    if self_check != ["fetch", "$(B)", "$(S)", output]:
        raise AssertionError(f"unexpected fetch command {args!r}")
    return args[3]


def make_error(files, target):
    try:
        lib.make(files, target)
    except AssertionError as error:
        return str(error)
    raise AssertionError("ay make unexpectedly succeeded")


def toolchain_files():
    files = {
        "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}END()\n",
        "build/platform/clang/ya.make": resources_library(
            "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(CLANG20 clang20.json)\n"
            "DECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:777)\n"
        ),
        "build/platform/clang/clang20.json": bundle_json(PLATFORM_BUNDLE),
        "build/platform/lld/ya.make": resources_library(
            "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(\n"
            "  LLD_ROOT sbr:22 FOR DARWIN-ARM64 sbr:21 FOR LINUX-X86_64\n"
            ")\n"
        ),
        "build/platform/python/ymake_python3/ya.make": resources_library(
            "DECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:31 EXTRA_TOOL sbr:32)\n"
        ),
        "prog/ya.make": (
            f"PROGRAM()\n{BARE}SRCS(main.cpp)\nRESOURCE(data.txt key)\nEND()\n"
        ),
        "prog/main.cpp": "int main(){return 0;}\n",
        "prog/data.txt": "data\n",
    }
    lib.tool_program(files, "tools/rescompiler", "rescompiler")
    lib.tool_program(files, "tools/rescompressor", "rescompressor")
    return files


class ResourceDeclsTest(unittest.TestCase):
    def test_toolchain_resources_select_host_entries(self):
        graph = lib.make(toolchain_files(), "prog")
        self.assertEqual(fetched_uri(graph, "CLANG20"), "sbr:1")
        self.assertEqual(fetched_uri(graph, "LLD_ROOT"), "sbr:21")
        self.assertEqual(fetched_uri(graph, "YMAKE_PYTHON3"), "sbr:31")
        self.assertEqual(fetched_uri(graph, "EXTRA_TOOL"), "sbr:32")
        self.assertEqual(len(fetches(graph)), 4)
        for node in graph["graph"]:
            if node["kv"]["p"] == "FT":
                self.assertEqual(node["platform"], "default-linux-x86_64")
                self.assertEqual(node["requirements"]["network"], "full")

        compile_node = lib.node_by_output(graph, "$(B)/prog/main.cpp.o")
        self.assertEqual(
            compile_node["cmds"][0]["cmd_args"][0],
            "$(B)/resources/CLANG20/bin/clang++",
        )
        objcopy = lib.node_by_output_prefix(graph, "$(B)/prog/objcopy_")
        args = objcopy["cmds"][0]["cmd_args"]
        self.assertEqual(args[:6], [
            "$(B)/resources/YMAKE_PYTHON3/bin/python3",
            "$(S)/build/scripts/objcopy.py",
            "--compiler",
            "$(B)/resources/CLANG20/bin/clang++",
            "--objcopy",
            "$(B)/resources/CLANG20/bin/llvm-objcopy",
        ])
        clang_fetch = lib.node_by_output(graph, "$(B)/resources/CLANG20")
        python_fetch = lib.node_by_output(graph, "$(B)/resources/YMAKE_PYTHON3")
        self.assertIn(clang_fetch["uid"], objcopy["deps"])
        self.assertIn(python_fetch["uid"], objcopy["deps"])

    def test_ownership_debug_mode_keeps_graph(self):
        plain = lib.make(toolchain_files(), "prog")
        with mock.patch.dict(os.environ, {"AY_DEBUG_OWNERSHIP": "1"}):
            debug = lib.make(toolchain_files(), "prog")
        self.assertEqual(debug["graph"], plain["graph"])

    def test_ownership_report_ranks_violations_by_node_count(self):
        files = {
            "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}END()\n",
            "lib/ya.make": f"LIBRARY()\n{BARE}USE_LLVM_BC20()\nLLVM_BC(a.cpp NAME kern SYMBOLS f)\nEND()\n",
            "lib/a.cpp": "int f(){return 0;}\n",
            "build/platform/clang/ya.make": resources_library("DECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:20)\n"),
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        result = lib.make_process(files, "lib", env={"AY_DEBUG_OWNERSHIP": "1"})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout), lib.make(files, "lib"))
        lines = result.stderr.splitlines()
        self.assertEqual(lines[0], "ownership: 3 violating (field, site) pairs")
        # Rows with equal node counts come in no particular order.
        self.assertEqual(
            lines[1],
            "ownership        3 nodes        3 backings  Cmd.CmdArgs.chunk @ emit_context.go:170     e.g. $(B)/lib/a.cpp.bc",
        )
        self.assertCountEqual(lines[2:], [
            "ownership        1 nodes        1 backings  DepRefs @ emit_context.go:170               e.g. $(B)/lib/kern_merged.bc",
            "ownership        1 nodes        1 backings  Inputs.chunk @ emit_context.go:170          e.g. $(B)/lib/kern_merged.bc",
        ])

    def test_host_bundle_rejects_malformed_or_missing_entries(self):
        for body, message in (
            (
                "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 AT LINUX)\n",
                "malformed DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE args",
            ),
            (
                "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 FOR DARWIN)\n",
                'resource "T" has no entry for host platform "linux-x86_64"',
            ),
        ):
            with self.subTest(body=body):
                files = {
                    "res/ya.make": resources_library(body),
                    "lib/ya.make": f"LIBRARY()\n{BARE}PEERDIR(res)\nEND()\n",
                }
                self.assertIn(message, make_error(files, "lib"))

    def test_resource_uri_from_json_follows_module_platform(self):
        modules = {
            "target": ("", "all.json"),
            "linux": ("DISABLE(ARCH_AARCH64)\n", "${ARCADIA_ROOT}/res/all.json"),
            "darwin_arm": ("ENABLE(OS_DARWIN)\n", "../all.json"),
            "darwin": ("ENABLE(OS_DARWIN)\nDISABLE(ARCH_ARM64)\n", "../all.json"),
            "win": ("ENABLE(OS_WINDOWS)\n", "../all.json"),
            "keep": ("SET(URI sbr:9)\n", "../linux_only.json"),
        }
        files = {
            "res/all.json": bundle_json(PLATFORM_BUNDLE),
            "res/linux_only.json": bundle_json({"linux": "sbr:8"}),
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                f"PEERDIR({' '.join('res/' + name for name in modules)})\n"
                "END()\n"
            ),
        }
        files["res/target/all.json"] = files["res/all.json"]
        for name, (prelude, json_path) in modules.items():
            files[f"res/{name}/ya.make"] = resources_library(
                prelude
                + f"SET_RESOURCE_URI_FROM_JSON(URI {json_path})\n"
                + f"DECLARE_EXTERNAL_RESOURCE(R_{name.upper()} ${{URI}})\n"
            )
        graph = lib.make(files, "lib")
        self.assertEqual(
            {name: fetched_uri(graph, f"R_{name.upper()}") for name in modules},
            {
                "target": "sbr:2",
                "linux": "sbr:1",
                "darwin_arm": "sbr:3",
                "darwin": "sbr:4",
                "win": "sbr:5",
                "keep": "sbr:9",
            },
        )

    def test_llvm_bc_clang_root_resolution(self):
        def files(prelude, clang):
            result = {
                "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}END()\n",
                "lib/ya.make": (
                    f"LIBRARY()\n{BARE}{prelude}"
                    "LLVM_BC(a.cpp NAME kern SYMBOLS f)\nEND()\n"
                ),
                "lib/a.cpp": "int f(){return 0;}\n",
            }
            if clang:
                result["build/platform/clang/ya.make"] = resources_library(
                    "DECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:20)\n"
                )
            lib.tool_program(result, "tools/rescompiler", "rescompiler")
            lib.tool_program(result, "tools/rescompressor", "rescompressor")
            return result

        for prelude, clang, root in (
            ("USE_LLVM_BC20()\n", True, "$(B)/resources/CLANG20"),
            (
                "SET(CLANG_BC_ROOT /opt/clang)\nSET(LLVM_LLC_TOOL tools/llc)\n",
                False,
                "/opt/clang",
            ),
        ):
            with self.subTest(root=root):
                graph = lib.make(files(prelude, clang), "lib")
                link = lib.node_by_output(graph, "$(B)/lib/kern_merged.bc")
                self.assertEqual(link["cmds"][0]["cmd_args"], [
                    f"{root}/bin/llvm-link",
                    "$(B)/lib/a.cpp.bc",
                    "-o",
                    "$(B)/lib/kern_merged.bc",
                ])
                opt = lib.node_by_output(graph, "$(B)/lib/kern_optimized.bc")
                self.assertIn(f"{root}/bin/opt", opt["cmds"][0]["cmd_args"])

        self.assertIn(
            '"$CLANG20_RESOURCE_GLOBAL" references resource global not in the PEERDIR closure',
            make_error(files("USE_LLVM_BC20()\n", False), "lib"),
        )

    def test_aarch64_host_selects_aarch64_bundle_entry(self):
        files = {
            "res/ya.make": resources_library(
                "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(\n"
                "  T sbr:1 FOR LINUX sbr:2 FOR LINUX-AARCH64\n"
                ")\n"
            ),
            "lib/ya.make": f"LIBRARY()\n{BARE}PEERDIR(res)\nEND()\n",
        }
        graph = lib.make(files, "lib", "--host-platform", "default-linux-aarch64")
        self.assertEqual(fetched_uri(graph, "T"), "sbr:2")
        fetch = lib.node_by_output(graph, "$(B)/resources/T")
        self.assertEqual(fetch["platform"], "default-linux-aarch64")

    def test_unittest_run_lists_sorted_resource_globals(self):
        files = {
            "build/platform/clang/ya.make": resources_library(
                "DECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:20)\n"
            ),
            "build/platform/python/ymake_python3/ya.make": resources_library(
                "DECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:31 AAA_TOOL sbr:1)\n"
            ),
            "library/cpp/testing/unittest_main/ya.make": f"LIBRARY()\n{BARE}END()\n",
            "lib/ya.make": f"LIBRARY()\n{BARE}SRCS(a.cpp)\nEND()\n",
            "lib/a.cpp": "int a(){return 0;}\n",
            "lib/ut/ya.make": f"UNITTEST_FOR(lib)\n{BARE}SRCS(a_ut.cpp)\nEND()\n",
            "lib/ut/a_ut.cpp": "int t(){return 0;}\n",
        }
        graph = lib.make(files, "lib/ut", "-t")
        run = lib.node_by_output(
            graph, "$(B)/lib/ut/test-results/unittest/meta.json",
        )
        args = run["cmds"][0]["cmd_args"]
        self.assertEqual(
            [args[i + 1] for i, arg in enumerate(args) if arg == "--global-resource"],
            [
                "AAA_TOOL_RESOURCE_GLOBAL::$(B)/resources/AAA_TOOL",
                "CLANG20_RESOURCE_GLOBAL::$(B)/resources/CLANG20",
                "YMAKE_PYTHON3_RESOURCE_GLOBAL::$(B)/resources/YMAKE_PYTHON3",
            ],
        )


class PrebuiltProgramTest(unittest.TestCase):
    def prebuilt_files(self, module_body):
        files = {
            "build/prebuilt/tools/gen/ya.make": module_body,
            "build/prebuilt/tools/gen/resources.json": bundle_json({
                "linux": "sbr:11",
                "linux-aarch64": "sbr:12",
                "darwin-arm64": "sbr:13",
                "win32": "sbr:15",
            }),
            "build/scripts/fs_tools.py": "",
            "library/cpp/resource/ya.make": f"LIBRARY()\n{BARE}END()\n",
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                "RUN_PROGRAM(build/prebuilt/tools/gen OUT_NOAUTO out.txt)\n"
                "RESOURCE(${BINDIR}/out.txt key)\n"
                "END()\n"
            ),
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        return files

    def test_prebuilt_tool_copies_fetched_binary(self):
        graph = lib.make(self.prebuilt_files(
            "SET_RESOURCE_URI_FROM_JSON(SANDBOX_RESOURCE_URI resources.json)\n"
            'IF (SANDBOX_RESOURCE_URI != "")\n'
            "    PREBUILT_PROGRAM()\n"
            "    DECLARE_EXTERNAL_RESOURCE(GEN ${SANDBOX_RESOURCE_URI})\n"
            "    DECLARE_EXTERNAL_RESOURCE(GEN sbr:999)\n"
            "    PRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/gen${MODULE_SUFFIX})\n"
            "    END()\n"
            "ENDIF()\n"
        ), "lib")
        self.assertEqual(fetched_uri(graph, "GEN"), "sbr:11")
        fetch = lib.node_by_output(graph, "$(B)/resources/GEN")
        tool = lib.node_by_output(graph, "$(B)/build/prebuilt/tools/gen/gen")
        self.assertEqual(tool["kv"], {"p": "ld", "pc": "light-blue", "show_out": "yes"})
        self.assertEqual(tool["platform"], "default-linux-x86_64")
        self.assertEqual(tool["cmds"][0]["cmd_args"], [
            "$(B)/resources/YMAKE_PYTHON3/bin/python3",
            "$(S)/build/scripts/fs_tools.py",
            "copy",
            "$(B)/resources/GEN/gen",
            "$(B)/build/prebuilt/tools/gen/gen",
        ])
        self.assertEqual(tool["inputs"], ["$(S)/build/scripts/fs_tools.py"])
        self.assertEqual(tool["deps"], [fetch["uid"]])
        run = lib.node_by_output(graph, "$(B)/lib/out.txt")
        self.assertEqual(run["cmds"][0]["cmd_args"], ["$(B)/build/prebuilt/tools/gen/gen"])
        self.assertEqual(run["deps"], [tool["uid"]])

    def test_windows_prebuilt_uses_exe_suffix(self):
        files = self.prebuilt_files(
            "PREBUILT_PROGRAM()\n"
            "DECLARE_EXTERNAL_RESOURCE(GEN sbr:15)\n"
            "PRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/gen${MODULE_SUFFIX})\n"
            "END()\n"
        )
        graph = lib.make(
            files,
            "build/prebuilt/tools/gen",
            "--target-platform", "default-windows-x86_64",
        )
        tool = lib.node_by_output(graph, "$(B)/build/prebuilt/tools/gen/gen")
        self.assertEqual(tool["platform"], "default-windows-x86_64")
        self.assertEqual(
            tool["cmds"][0]["cmd_args"][3],
            "$(B)/resources/GEN/gen.exe",
        )

    def test_prebuilt_sbom_components_feed_the_copy(self):
        files = self.prebuilt_files(
            "PREBUILT_PROGRAM()\n"
            "LICENSE(MIT)\n"
            "VERSION(1.2.3)\n"
            "DECLARE_EXTERNAL_RESOURCE(GEN sbr:5)\n"
            "PRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/gen${MODULE_SUFFIX})\n"
            "END()\n"
        )
        files.update({
            "build/internal/conf/sbom.conf": "",
            "build/internal/platform/clang_toolchain_info/ya.make": resources_library(
                "TOOLCHAIN(clang)\nVERSION(20)\n"
            ),
            "build/platform/python/ymake_python3/ya.make": resources_library(
                "TOOLCHAIN(python3)\nVERSION(3.12.6)\n"
                "DECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:31)\n"
            ),
        })
        graph = lib.make(files, "lib")
        tool = lib.node_by_output(graph, "$(B)/build/prebuilt/tools/gen/gen")
        self.assertEqual(tool["inputs"], [
            "$(S)/build/scripts/fs_tools.py",
            "$(B)/build/platform/python/ymake_python3/toolchain.component.sbom",
            "$(B)/build/prebuilt/tools/gen/gen.AGNOSTIC.component.sbom",
            "$(S)/build/internal/scripts/gen_sbom.py",
        ])
        for output in (
            "$(B)/resources/GEN",
            "$(B)/build/platform/python/ymake_python3/toolchain.component.sbom",
            "$(B)/build/prebuilt/tools/gen/gen.AGNOSTIC.component.sbom",
        ):
            self.assertIn(lib.node_by_output(graph, output)["uid"], tool["deps"])

    def test_prebuilt_program_requires_resolved_primary_output(self):
        for body, message in (
            (
                "DECLARE_EXTERNAL_RESOURCE(GEN sbr:5)\n",
                "PREBUILT_PROGRAM has no PRIMARY_OUTPUT/resource",
            ),
            (
                "PRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/gen)\n",
                "PREBUILT_PROGRAM has no PRIMARY_OUTPUT/resource",
            ),
            (
                "DECLARE_EXTERNAL_RESOURCE(GEN sbr:5)\n"
                "PRIMARY_OUTPUT(${UNKNOWN_RESOURCE_GLOBAL}/gen)\n",
                'PRIMARY_OUTPUT "${UNKNOWN_RESOURCE_GLOBAL}/gen" has an unresolved reference',
            ),
        ):
            with self.subTest(body=body):
                files = self.prebuilt_files(f"PREBUILT_PROGRAM()\n{body}END()\n")
                self.assertIn(message, make_error(files, "lib"))


if __name__ == "__main__":
    unittest.main(verbosity=2)
