import io
import json
import os
import re
import tempfile
import tarfile
import unittest
from pathlib import Path
from unittest import mock

import lib
from resource_dependencies import fetches, fetched_uri


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
            "  LLD_ROOT sbr:22 FOR DARWIN-ARM64 sbr:21 FOR LINUX\n"
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
    def test_json_bundle_with_single_any_platform(self):
        files = {
            "res/ya.make": resources_library("DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(PNPM resource.json)\n"),
            "res/resource.json": bundle_json({"any": "sbr:123"}),
        }
        files.update({"consumer/ya.make": f"LIBRARY()\n{BARE}PEERDIR(res)\nCFLAGS(-DPROBE=${{PNPM_RESOURCE_GLOBAL}})\nSRCS(main.cpp)\nEND()\n",
                      "consumer/main.cpp": "int value;\n"})
        self.assertEqual(fetched_uri(lib.make(files, "consumer"), "PNPM"), "sbr:123")

    def test_toolchain_resources_select_host_entries(self):
        graph = lib.make(toolchain_files(), "prog")
        self.assertEqual(fetched_uri(graph, "CLANG20"), "sbr:1")
        self.assertEqual(fetched_uri(graph, "LLD_ROOT"), "sbr:21")
        self.assertEqual(fetched_uri(graph, "YMAKE_PYTHON3"), "sbr:31")
        self.assertNotIn("$(B)/resources/EXTRA_TOOL", fetches(graph))
        self.assertEqual(len(fetches(graph)), 3)
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
        graph = json.loads(result.stdout)
        self.assertEqual(graph, lib.make(files, "lib"))
        compiled = lib.node_by_output(graph, "$(B)/lib/a.cpp.bc")
        merged = lib.node_by_output(graph, "$(B)/lib/kern_merged.bc")
        optimized = lib.node_by_output(graph, "$(B)/lib/kern_optimized.bc")
        self.assertEqual(merged["deps"], [compiled["uid"]])
        self.assertEqual(optimized["deps"], [merged["uid"]])
        self.assertIn(compiled["outputs"][0], merged["inputs"])
        self.assertIn(merged["outputs"][0], optimized["inputs"])
        lines = result.stderr.splitlines()
        self.assertEqual(lines[0], "ownership: 2 violating (field, site) pairs")
        rows, sites = [], set()
        for line in lines[1:]:
            match = re.fullmatch(
                r"ownership +(\d+) nodes +(\d+) backings  (\S+) @ emit_context\.go:([1-9]\d*) +e\.g\. (.+)",
                line,
            )
            self.assertIsNotNone(match, line)
            nodes, backings, field, site, example = match.groups()
            sites.add(int(site))
            rows.append((int(nodes), int(backings), field, example))
        self.assertEqual(len(sites), 1)
        # Rows with equal node counts come in no particular order.
        self.assertEqual(
            rows[0],
            (3, 3, "Cmd.CmdArgs.chunk", "$(B)/lib/a.cpp.bc"),
        )
        self.assertCountEqual(rows[1:], [
            (1, 1, "Inputs.chunk", "$(B)/lib/kern_merged.bc"),
        ])

    def test_host_bundle_json_path_follows_include_path_rules(self):
        modules = {
            "plain": "all.json",
            "curdir": "${CURDIR}/all.json",
            "root": "${ARCADIA_ROOT}/shared/all.json",
        }
        files = {
            "shared/all.json": bundle_json({"linux-x86_64": "sbr:7"}),
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}"
                f"PEERDIR({' '.join('res/' + name for name in modules)})\n"
                "END()\n"
            ),
        }
        for name, json_path in modules.items():
            files[f"res/{name}/all.json"] = bundle_json({"linux-x86_64": f"sbr:{len(name)}"})
            files[f"res/{name}/ya.make"] = resources_library(
                f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(B_{name.upper()} {json_path})\n"
            )
        files["lib/ya.make"] = files["lib/ya.make"].replace("END()",
            "CFLAGS(" + " ".join(f"-D{name}=${{B_{name.upper()}_RESOURCE_GLOBAL}}" for name in modules)
            + ")\nSRCS(main.cpp)\nEND()")
        files["lib/main.cpp"] = "int value;\n"
        graph = lib.make(files, "lib")
        self.assertEqual(
            {name: fetched_uri(graph, f"B_{name.upper()}") for name in modules},
            {"plain": "sbr:5", "curdir": "sbr:6", "root": "sbr:7"},
        )

    def test_unconsumed_host_bundles_do_not_require_a_host_entry(self):
        for body in (
            "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 AT LINUX)\n",
            "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 FOR DARWIN)\n",
        ):
            with self.subTest(body=body):
                files = {
                    "res/ya.make": resources_library(body),
                    "lib/ya.make": f"LIBRARY()\n{BARE}PEERDIR(res)\nEND()\n",
                }
                graph = lib.make(files, "lib")
                self.assertNotIn("$(B)/resources/T", fetches(graph))

    def test_json_host_bundle_strip_prefix_uses_uint16_limit(self):
        files = {
            "res/ya.make": resources_library(
                "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(T bundle.json)\n"
            ),
            "lib/ya.make": (
                f"PROGRAM()\n{BARE}PEERDIR(res)\n"
                "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n"
            ),
            "lib/main.cpp": "int main(){return 0;}\n",
        }
        for count in (65535, 65536):
            with self.subTest(strip_prefix=count):
                files["res/bundle.json"] = json.dumps({"by_platform": {
                    "linux-x86_64": {"uri": "sbr:1", "strip_prefix": count},
                }})
                if count == 65536:
                    self.assertIn("strip_prefix value 65536 is too big", make_error(files, "lib"))
                else:
                    graph = lib.make(files, "lib")
                    args = fetches(graph)["$(B)/resources/T"]
                    descriptor = json.loads(args[2])
                    self.assertEqual(fetched_uri(graph, "T"), "sbr:1")
                    self.assertEqual([entry["strip_prefix"] for entry in descriptor["resources"]], [65535])

    def test_host_bundle_uses_platform_triples_with_an_arbitrary_separator(self):
        files = {
            "res/ya.make": resources_library(
                "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 AT LINUX)\n"
            ),
            "lib/ya.make": (
                f"LIBRARY()\n{BARE}PEERDIR(res)\n"
                "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n"
            ),
            "lib/main.cpp": "int value;\n",
        }
        graph = lib.make(files, "lib")
        self.assertEqual(fetched_uri(graph, "T"), "sbr:1")
        compiled = lib.node_by_output(graph, "$(B)/lib/main.cpp.o")
        self.assertIn("-DPROBE=$(B)/resources/T", compiled["cmds"][0]["cmd_args"])

    def test_selected_host_bundle_rejects_incomplete_platform_triples(self):
        files = {
            "res/ya.make": resources_library(
                "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 FOR)\n"
            ),
            "lib/ya.make": (
                f"PROGRAM()\n{BARE}PEERDIR(res)\n"
                "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n"
            ),
            "lib/main.cpp": "int main(){return 0;}\n",
        }
        self.assertIn(
            "malformed DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE args",
            make_error(files, "lib"),
        )

    def test_unconsumed_host_bundle_rejects_incomplete_platform_triples(self):
        files = {
            "res/ya.make": resources_library(
                "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 FOR)\n"
            ),
            "lib/ya.make": f"LIBRARY()\n{BARE}PEERDIR(res)\nEND()\n",
        }
        result = lib.make_process(files, "lib")
        self.assertNotEqual(result.returncode, 0, result.stderr)
        self.assertIn("malformed DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE args", result.stderr)

    def test_host_bundle_accepts_strip_prefix_pairs_between_platform_entries(self):
        for entries in (
            "sbr:1 FOR LINUX STRIP_PREFIX 0",
            "sbr:1 FOR LINUX STRIP_PREFIX +0",
            "sbr:1 AT LINUX STRIP_PREFIX 0 sbr:2 FOR DARWIN STRIP_PREFIX 2",
        ):
            with self.subTest(entries=entries):
                files = {
                    "res/ya.make": resources_library(
                        f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T {entries})\n"
                    ),
                    "lib/ya.make": (
                        f"PROGRAM()\n{BARE}PEERDIR(res)\n"
                        "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n"
                    ),
                    "lib/main.cpp": "int main(){return 0;}\n",
                }
                graph = lib.make(files, "lib")
                self.assertEqual(fetched_uri(graph, "T"), "sbr:1")
                compiled = lib.node_by_output(graph, "$(B)/lib/main.cpp.o")
                self.assertIn("-DPROBE=$(B)/resources/T", compiled["cmds"][0]["cmd_args"])

    def test_selected_host_bundle_rejects_bad_or_missing_strip_prefix_counts(self):
        for suffix, message in (
            ("STRIP_PREFIX", "malformed DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE args"),
            ("STRIP_PREFIX bad", "STRIP_PREFIX must be integral"),
            ("STRIP_PREFIX -1", "STRIP_PREFIX must be integral"),
            ("STRIP_PREFIX -0", "STRIP_PREFIX must be integral"),
            ("STRIP_PREFIX 4294967296", "STRIP_PREFIX must be integral"),
            ("STRIP_PREFIX 1.0", "STRIP_PREFIX must be integral"),
        ):
            with self.subTest(suffix=suffix):
                files = {
                    "res/ya.make": resources_library(
                        f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 FOR LINUX {suffix})\n"
                    ),
                    "lib/ya.make": (
                        f"PROGRAM()\n{BARE}PEERDIR(res)\n"
                        "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n"
                    ),
                    "lib/main.cpp": "int main(){return 0;}\n",
                }
                self.assertIn(message, make_error(files, "lib"))

    def test_nonzero_host_bundle_strip_prefix_changes_fetch_action(self):
        actions = []
        for count in (0, 1):
            files = {
                "res/ya.make": resources_library(
                    f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 FOR LINUX STRIP_PREFIX {count})\n"
                ),
                "lib/ya.make": (
                    f"PROGRAM()\n{BARE}PEERDIR(res)\n"
                    "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n"
                ),
                "lib/main.cpp": "int main(){return 0;}\n",
            }
            graph = lib.make(files, "lib")
            actions.append(lib.node_by_output(graph, "$(B)/resources/T")["cmds"])
        self.assertNotEqual(actions[0], actions[1], "FETCH loses STRIP_PREFIX 1")

    def test_host_bundle_strip_prefix_extracts_a_local_tar(self):
        with tempfile.TemporaryDirectory(prefix="ay-strip-resource-") as directory:
            root = Path(directory)
            archive_path = root / "tool.tar"
            with tarfile.open(archive_path, "w") as archive:
                directory_entry = tarfile.TarInfo("prefix/")
                directory_entry.type = tarfile.DIRTYPE
                directory_entry.mode = 0o755
                archive.addfile(directory_entry)
                entry = tarfile.TarInfo("prefix/bin/tool")
                content = b"local tool\n"
                entry.size = len(content)
                entry.mode = 0o755
                archive.addfile(entry, io.BytesIO(content))

            source = root / "src"
            script = source / "build/scripts/fetch_from_sandbox.py"
            script.parent.mkdir(parents=True)
            script.write_text(
                "import shutil, sys\n"
                f"shutil.copyfile({str(archive_path)!r}, sys.argv[sys.argv.index('--copy-to') + 1])\n"
            )
            env = {**os.environ, "HOME": str(root / "home"), "YA_TOKEN": "",
                   "SSH_AUTH_SOCK": "", "AY_TEST_SSH_OAUTH": ""}
            for kind, count in (("bundle", 0), ("bundle", 1), ("bundle", 2),
                                ("bundle", 4294967295), ("any", 1), ("json", 1)):
                with self.subTest(kind=kind, strip_prefix=count):
                    declaration = (
                        f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 FOR "
                        f"{'ANY' if kind == 'any' else 'LINUX'} STRIP_PREFIX {count})\n"
                    )
                    if kind == "json":
                        declaration = "DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE_BY_JSON(T bundle.json)\n"
                    files = {
                        "res/ya.make": resources_library(declaration),
                        "res/bundle.json": json.dumps({"by_platform": {
                            "linux-x86_64": {"uri": "sbr:1", "strip_prefix": count},
                        }}),
                        "lib/ya.make": (
                            f"PROGRAM()\n{BARE}PEERDIR(res)\n"
                            "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n"
                        ),
                        "lib/main.cpp": "int main(){return 0;}\n",
                    }
                    graph = lib.make(files, "lib")
                    build = root / f"{kind}-{count}"
                    args = fetches(graph)["$(B)/resources/T"]
                    result = lib.run_process(*[
                        arg.replace("$(B)", str(build)).replace("$(S)", str(source)) for arg in args
                    ], env=env)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    output = build / "resources/T"
                    if count == 4294967295:
                        self.assertEqual(list(output.iterdir()), [])
                    else:
                        target = output.joinpath(*("prefix", "bin", "tool")[count:])
                        self.assertEqual(target.read_bytes(), content)
                        self.assertEqual(target.stat().st_mode & 0o777, 0o755)

    def test_missing_host_entry_is_rejected_when_fetch_runs(self):
        for platform in ("DARWIN", "LINUX-X86_64", "any"):
            with self.subTest(platform=platform):
                files = {
                    "res/ya.make": resources_library(
                        f"DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(T sbr:1 FOR {platform})\n"
                    ),
                    "lib/ya.make": (
                        f"PROGRAM()\n{BARE}PEERDIR(res)\n"
                        "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()\n"
                    ),
                    "lib/main.cpp": "int main(){return 0;}\n",
                }
                graph = lib.make(files, "lib")
                args = lib.node_by_output(graph, "$(B)/resources/T")["cmds"][0]["cmd_args"][1:]
                with tempfile.TemporaryDirectory(prefix="ay-host-resource-") as directory:
                    root = Path(directory)
                    output = root / "bld/resources/T"
                    output.mkdir(parents=True)
                    (output / "kept").write_text("untouched\n")
                    result = lib.run_process(*[
                        arg.replace("$(B)", str(root / "bld")).replace("$(S)", str(root / "src"))
                        for arg in args
                    ], env={**os.environ, "HOME": str(root / "home"), "YA_TOKEN": "",
                            "SSH_AUTH_SOCK": "", "AY_TEST_SSH_OAUTH": ""})
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn('resource "T" has no entry for host platform "linux-x86_64"', result.stderr)
                    self.assertEqual((output / "kept").read_text(), "untouched\n")

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
        files["lib/ya.make"] = files["lib/ya.make"].replace("END()",
            "CFLAGS(" + " ".join(f"-D{name}=${{R_{name.upper()}_RESOURCE_GLOBAL}}" for name in modules)
            + ")\nSRCS(main.cpp)\nEND()")
        files["lib/main.cpp"] = "int value;\n"
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
        files["lib/ya.make"] = files["lib/ya.make"].replace("END()", "CFLAGS(-DPROBE=${T_RESOURCE_GLOBAL})\nSRCS(main.cpp)\nEND()")
        files["lib/main.cpp"] = "int value;\n"
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

    def test_disabled_prebuilt_uses_source_tool_when_uri_is_unset(self):
        files = self.prebuilt_files(
            "IF (USE_PREBUILT_TOOLS)\n"
            "SET_RESOURCE_URI_FROM_JSON(SANDBOX_RESOURCE_URI resources.json)\nENDIF()\n"
            'IF (SANDBOX_RESOURCE_URI != "")\n'
            "PREBUILT_PROGRAM()\nDECLARE_EXTERNAL_RESOURCE(GEN ${SANDBOX_RESOURCE_URI})\n"
            "PRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/gen)\nEND()\nELSE()\n"
            f"PROGRAM()\n{BARE}SRCS(main.cpp)\nEND()\nENDIF()\n"
        )
        files["build/prebuilt/tools/gen/main.cpp"] = "int main() { return 0; }\n"
        graph = lib.make(files, "lib", "--host-platform-flag=USE_PREBUILT_TOOLS=no")
        tool = lib.node_by_output(graph, "$(B)/build/prebuilt/tools/gen/gen")
        self.assertEqual(tool["kv"]["p"], "LD")
        obj = lib.node_by_output(graph, "$(B)/build/prebuilt/tools/gen/main.cpp.pic.o")
        self.assertIn(obj["uid"], tool["deps"])

    def test_prebuilt_tool_copies_fetched_binary(self):
        graph = lib.make(self.prebuilt_files(
            "SET_RESOURCE_URI_FROM_JSON(SANDBOX_RESOURCE_URI resources.json)\n"
            'IF (SANDBOX_RESOURCE_URI != "")\n'
            "    PREBUILT_PROGRAM()\n"
            "    DECLARE_EXTERNAL_RESOURCE(GEN ${SANDBOX_RESOURCE_URI})\n"
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
        # The copy takes the components, not their generator script, as the
        # prebuilt copies of the devtools_yamaker reference do.
        tool = lib.node_by_output(graph, "$(B)/build/prebuilt/tools/gen/gen")
        self.assertEqual(tool["inputs"], [
            "$(S)/build/scripts/fs_tools.py",
            "$(B)/build/platform/python/ymake_python3/toolchain.component.sbom",
            "$(B)/build/prebuilt/tools/gen/gen.AGNOSTIC.component.sbom",
        ])
        for output in (
            "$(B)/resources/GEN",
            "$(B)/build/platform/python/ymake_python3/toolchain.component.sbom",
            "$(B)/build/prebuilt/tools/gen/gen.AGNOSTIC.component.sbom",
        ):
            self.assertIn(lib.node_by_output(graph, output)["uid"], tool["deps"])

    def test_prebuilt_primary_output_preserves_native_empty_expansion(self):
        for body, source_args in (
            (
                "DECLARE_EXTERNAL_RESOURCE(GEN sbr:5)\n",
                [],
            ),
            (
                "PRIMARY_OUTPUT(${GEN_RESOURCE_GLOBAL}/gen)\n",
                ["/gen"],
            ),
            (
                "DECLARE_EXTERNAL_RESOURCE(GEN sbr:5)\n"
                "PRIMARY_OUTPUT(${UNKNOWN_RESOURCE_GLOBAL}/gen)\n",
                ["/gen"],
            ),
        ):
            with self.subTest(body=body):
                files = self.prebuilt_files(f"PREBUILT_PROGRAM()\n{body}END()\n")
                graph = lib.make(files, "lib")
                tool = lib.node_by_output(graph, "$(B)/build/prebuilt/tools/gen/gen")
                self.assertEqual(tool["cmds"][0]["cmd_args"][2:], [
                    "copy", *source_args, "$(B)/build/prebuilt/tools/gen/gen",
                ])
                self.assertNotIn("$(B)/resources/GEN", fetches(graph))


if __name__ == "__main__":
    unittest.main(verbosity=2)
