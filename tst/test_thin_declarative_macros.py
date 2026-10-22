import json
import sys
import unittest

import lib
from test_codegen_cuda import cuda_conf
from test_go_tests import go_world
from test_java_tests import test_nodes
from test_exec_make import Workspace


def native(body, module="LIBRARY()"):
    return f"{module}\n{lib.BARE_MODULE}{body}\nSRCS(probe.cpp)\nEND()\n"


def checks(graph):
    return {key: node for key, node in test_nodes(graph).items()
            if key.endswith(("/validate_data_sbr", "/validate_resource"))}


class ThinDeclarativeMacrosTest(unittest.TestCase):
    def test_validation_restart_changes_only_validation_identity(self):
        for declaration in ("PROGRAM()", "EXECTEST()\nRUN(probe)"):
            with self.subTest(declaration=declaration):
                def fixture(restart):
                    return {
                        "samples/identity/ya.make": (
                            f"{declaration}\n{lib.BARE_MODULE}"
                            "SET(VALIDATE_DATA yes)\nSET(REVISION restart-seven)\n"
                            + restart + "\nDATA(sbr://901)\n"
                            "FROM_SANDBOX(FILE 902 OUT_NOAUTO unused.dat)\nEND()\n"
                        ),
                    }
                baseline = lib.make(fixture(""), "samples/identity", "--target-platform-tests")
                restarted = lib.make(fixture("VALIDATE_DATA_RESTART($REVISION)\nSET(REVISION later)"),
                                     "samples/identity", "--target-platform-tests")
                for key, before in checks(baseline).items():
                    after = checks(restarted)[key]
                    self.assertEqual(before, after)
                untouched = {node["outputs"][0]: node["uid"] for node in baseline["graph"]
                             if not node["kv"].get("path", "").endswith(("/validate_data_sbr", "/validate_resource"))}
                self.assertEqual(untouched, {node["outputs"][0]: node["uid"] for node in restarted["graph"]
                                            if not node["kv"].get("path", "").endswith(("/validate_data_sbr", "/validate_resource"))})
                replaced = lib.make(fixture("VALIDATE_DATA_RESTART(first)\nSET(SBR_UID_EXT restart-seven)"),
                                    "samples/identity", "--target-platform-tests")
                self.assertEqual(checks(restarted), checks(replaced))

    def test_validation_restart_changes_executor_cache_uid_without_command_changes(self):
        for assignment in ("VALIDATE_DATA_RESTART({})", "SET(SBR_UID_EXT {})"):
            with self.subTest(assignment=assignment):
                self.check_validation_restart_cache_uid(assignment)

    def check_validation_restart_cache_uid(self, assignment):
        workspace = Workspace(self, {
            "renamed/cache/ya.make": (
                "EXECTEST()\nRUN(probe)\nSET(VALIDATE_DATA yes)\n"
                "INCLUDE(${ARCADIA_ROOT}/shared/revision.inc)\n"
                "DATA(sbr://906)\nFROM_SANDBOX(FILE 907 OUT_NOAUTO unused.dat)\nEND()\n"
            ),
            "shared/revision.inc": assignment.format("alpha") + "\n",
            "build/scripts/append_file.py": (
                "from pathlib import Path\nimport sys\nPath(sys.argv[1]).write_text('context')\n"
            ),
            "fake_checker.py": (
                "from pathlib import Path\nimport sys\n"
                "for flag in ('--meta', '--trace', '--log-path', '--tar'):\n"
                "    if flag in sys.argv:\n"
                "        path = Path(sys.argv[sys.argv.index(flag) + 1])\n"
                "        path.parent.mkdir(parents=True, exist_ok=True)\n"
                "        path.write_text('checked')\n"
                "if 'download' in sys.argv:\n"
                "    root = Path(sys.argv[sys.argv.index('--storage-root') + 1])\n"
                "    resource = sys.argv[sys.argv.index('--id') + 1]\n"
                "    for name in ('resource', 'resource_info.json'):\n"
                "        path = root / 'sandbox-storage' / resource / name\n"
                "        path.parent.mkdir(parents=True, exist_ok=True)\n"
                "        path.write_text('downloaded')\n"
            ),
        })
        wrapper = workspace.src / "python-wrapper"
        wrapper.write_text(f'#!/bin/sh\nshift\nexec {sys.executable} "$@"\n')
        wrapper.chmod(0o755)
        prefix = f"test_tool={sys.executable} {workspace.src}/fake_checker.py"
        args = ("-j1", "--target-platform-tests", "--cmd-prefix", prefix,
                "--cmd-prefix", f"python3={wrapper}")
        workspace.make(*args, targets=("renamed/cache",))

        def identities():
            return {path.name: tuple(json.loads(path.read_text())) for path in workspace.uid_files()}

        before = identities()
        workspace.write("shared/revision.inc", assignment.format("beta") + "\n")
        workspace.make(*args, targets=("renamed/cache",))
        after = identities()
        added = [outputs for uid, outputs in after.items() if uid not in before]
        self.assertEqual(len(added), 2)
        self.assertEqual({output[0] for output in added}, {
            "$(B)/renamed/cache/test-results/validate_data_sbr/meta.json",
            "$(B)/renamed/cache/test-results/validate_resource/meta.json",
        })
        workspace.write("shared/revision.inc", assignment.format("alpha") + "\n")
        workspace.make(*args, targets=("renamed/cache",))
        self.assertEqual(after, identities())

    def test_shared_include_keeps_module_restart_and_acknowledgement_values(self):
        sources = {
            "shared/policy.inc": (
                "NO_NEED_CHECK(ARBITRARY_DEPRECATED_FLAG)\n"
                "IF (UNUSED_MACRO == \"yes\")\nVALIDATE_DATA_RESTART($REVISION)\nENDIF()\n"
            ),
        }
        for name, revision in (("first", "alpha"), ("second", "beta")):
            sources[f"renamed/{name}/ya.make"] = (
                f"EXECTEST()\nRUN(probe)\nSET(REVISION {revision})\n"
                "SET(VALIDATE_DATA yes)\nDISABLE(UNUSED_MACRO)\nINCLUDE(${ARCADIA_ROOT}/shared/policy.inc)\n"
                f"IF (SBR_UID_EXT == \"{revision}\")\nDATA(sbr://903)\nENDIF()\nEND()\n"
            )
        for order in (("renamed/first", "renamed/second"), ("renamed/second", "renamed/first")):
            graph = lib.make(sources, *order, "--target-platform-tests")
            self.assertEqual(set(checks(graph)), {name + "/validate_data_sbr" for name in order})
            for name, revision in (("first", "alpha"), ("second", "beta")):
                direct = dict(sources)
                direct[f"renamed/{name}/ya.make"] = direct[f"renamed/{name}/ya.make"].replace(
                    "INCLUDE(${ARCADIA_ROOT}/shared/policy.inc)", f"ENABLE(UNUSED_MACRO)\nSET(SBR_UID_EXT {revision})")
                expected = checks(lib.make(direct, *order, "--target-platform-tests"))
                self.assertEqual(checks(graph)[f"renamed/{name}/validate_data_sbr"],
                                 expected[f"renamed/{name}/validate_data_sbr"])

    def test_java_validation_uses_the_same_declarative_assignment(self):
        sources = lib.java_tree(**{
            "renamed/jvm/ya.make": (
                "EXTERNAL_JAVA_LIBRARY()\nLOCAL_JAR(payload.jar)\n"
                "SET(VALIDATE_DATA yes)\nSET(REVISION java-nine)\nVALIDATE_DATA_RESTART($REVISION)\n"
                "NO_NEED_CHECK(LEGACY_VALUE)\nIF (UNUSED_MACRO)\nDATA(sbr://904)\nENDIF()\n"
                "FROM_SANDBOX(FILE 905 OUT_NOAUTO unused.bin)\nEND()\n"
            ),
            "renamed/jvm/payload.jar": "jar",
        })
        graph = lib.make(sources, "renamed/jvm", "--target-platform-tests")
        self.assertEqual(len(checks(graph)), 2)
        sources["renamed/jvm/ya.make"] = sources["renamed/jvm/ya.make"].replace(
            "VALIDATE_DATA_RESTART($REVISION)", "SET(SBR_UID_EXT java-nine)")
        self.assertEqual(checks(graph), checks(lib.make(sources, "renamed/jvm", "--target-platform-tests")))

    def test_pack_final_value_adds_archive_output_and_command_option(self):
        for extension in ("tar", "tar.gz", "", "no"):
            with self.subTest(extension=extension):
                sources = {
                    "renamed/crate/ya.make": (
                        f"PACKAGE(delivery)\nSET(EXTENSION {extension or 'tar'})\nPACK($EXTENSION)\n"
                        + ("SET(PACKED_PACKAGE_EXT)\n" if not extension else "SET(EXTENSION later)\n") +
                        "FILES(payload.txt)\nEND()\n"
                    ),
                    "renamed/crate/payload.txt": "payload",
                }
                result = lib.make_process(sources, "renamed/crate")
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertNotIn("unmodelled", result.stderr)
                graph = json.loads(result.stdout)
                final = lib.node_by_output(graph, "$(B)/renamed/crate/delivery.final.pkg.fake")
                union = lib.node_by_output(graph, "$(B)/renamed/crate/delivery.pkg.fake")
                cmd = next(cmd["cmd_args"] for cmd in final["cmds"]
                           if "$(S)/build/scripts/copy_to_dir.py" in cmd["cmd_args"])
                if extension in ("tar", "tar.gz"):
                    archive = f"$(B)/renamed/crate/delivery.{extension}"
                    self.assertIn(archive, final["outputs"])
                    self.assertEqual(cmd[cmd.index("--dest-arch"):cmd.index("--dest-arch") + 2],
                                     ["--dest-arch", archive])
                    self.assertLess(cmd.index("--dest-arch"), cmd.index("$(B)/renamed/crate/payload.txt"))
                    self.assertNotIn(archive, union["outputs"])
                else:
                    self.assertNotIn("--dest-arch", cmd)
                copy = lib.node_by_output(graph, "$(B)/renamed/crate/payload.txt")
                self.assertIn(copy["uid"], final["deps"])
                self.assertIn(union["uid"], final["deps"])

    def test_cuda_ram_applies_to_src_cu_and_obeys_final_assignment(self):
        for declaration, ram in (("", 32), ("SET_CUDA_NODE_RAM_REQUIEREMENT($MEMORY)", 19),
                                 ("SET(_CU_NODE_MAX_RAM_REQUIREMENT ram:19)", 19),
                                 ("SET_CUDA_NODE_RAM_REQUIEREMENT(7)\nSET(_CU_NODE_MAX_RAM_REQUIREMENT ram:23)", 23),
                                 ("SET_CUDA_NODE_RAM_REQUIEREMENT(7)\nSET(_CU_NODE_MAX_RAM_REQUIREMENT no)", 32)):
            with self.subTest(declaration=declaration):
                sources = {
                    "build/ymake_conf.py": cuda_conf(),
                    "renamed/device/ya.make": native("SET(MEMORY 19)\n" + declaration + "\nSRCS(kernel.cu)"),
                    "renamed/device/kernel.cu": "",
                    "renamed/device/probe.cpp": "int native_value;\n",
                }
                lib.tool_program(sources, "tools/mtime0", "mtime0")
                lib.tool_program(sources, "tools/custom_pid", "custom_pid")
                graph = lib.make(sources, "renamed/device")
                node = lib.node_by_output(graph, "$(B)/renamed/device/kernel.cu.o")
                self.assertEqual(node["requirements"]["ram"], ram)
                self.assertEqual(node["kv"].get("local_ram_limiter"), "1" if ram != 32 else None)
                native_node = lib.node_by_output(graph, "$(B)/renamed/device/probe.cpp.o")
                self.assertNotIn("local_ram_limiter", native_node["kv"])
                self.assertNotIn("ram:19", node["cmds"][0]["cmd_args"])

    def test_pack_variable_assignment_reaches_the_archive_consumer(self):
        graph = lib.make({
            "renamed/direct/ya.make": "PACKAGE(shipment)\nSET(PACKED_PACKAGE_EXT tar)\nEND()\n",
        }, "renamed/direct")
        package = lib.node_by_output(graph, "$(B)/renamed/direct/shipment.tar")
        command = next(cmd["cmd_args"] for cmd in package["cmds"]
                       if "$(S)/build/scripts/copy_to_dir.py" in cmd["cmd_args"])
        self.assertEqual(command[command.index("--dest-arch") + 1], "$(B)/renamed/direct/shipment.tar")

    def test_packed_archive_has_a_registered_producer_for_build_inputs(self):
        sources = {
            "renamed/archive/ya.make": "PACKAGE(shipment)\nPACK(tar.gz)\nFILES(payload.txt)\nEND()\n",
            "renamed/archive/payload.txt": "payload",
            "renamed/inspect/ya.make": (
                "UNION()\nBUNDLE(renamed/archive NAME marker)\n"
                "RUN_PROGRAM(tools/reader IN ${ARCADIA_BUILD_ROOT}/renamed/archive/shipment.tar.gz OUT report.txt)\nEND()\n"
            ),
        }
        lib.tool_program(sources, "tools/reader", "reader")
        graph = lib.make(sources, "renamed/inspect")
        package = lib.node_by_output(graph, "$(B)/renamed/archive/shipment.tar.gz")
        consumer = lib.node_by_output(graph, "$(B)/renamed/inspect/report.txt")
        self.assertIn("$(B)/renamed/archive/shipment.tar.gz", consumer["inputs"])
        self.assertIn(package["uid"], consumer["deps"])

    def test_cuda_split_rules_do_not_consume_the_src_cu_ram_variable(self):
        sources = {
            "build/ymake_conf.py": cuda_conf(),
            "renamed/split/ya.make": native("SET(CUDA_ARCHITECTURES sm_80)\nCUDA_SRCS(split.cu)"),
            "renamed/split/probe.cpp": "int value;\n",
            "renamed/split/split.cu": "",
            "build/internal/platform/cuda/cuda_runtime_include.h": "",
        }
        lib.tool_program(sources, "tools/mtime0", "mtime0")
        lib.tool_program(sources, "tools/custom_pid", "custom_pid")
        before = lib.make(sources, "renamed/split")
        sources["renamed/split/ya.make"] = native(
            "SET_CUDA_NODE_RAM_REQUIEREMENT(2)\nSET(CUDA_ARCHITECTURES sm_80)\nCUDA_SRCS(split.cu)")
        after = lib.make(sources, "renamed/split")
        self.assertEqual(before, after)
        for node in after["graph"]:
            if node["kv"].get("p") == "CU":
                self.assertNotIn("local_ram_limiter", node["kv"])
                self.assertEqual(node["requirements"]["ram"], 32)
        sources["renamed/split/ya.make"] = native(
            "SET_CUDA_NODE_RAM_REQUIEREMENT(2)\nSET(CUDA_ARCHITECTURES sm_80)\n"
            "IF (_CU_NODE_MAX_RAM_REQUIREMENT == \"ram:2\")\nCUDA_SRCS(split.cu)\nENDIF()")
        self.assertEqual(after, lib.make(sources, "renamed/split"))

    def test_package_variants_keep_distinct_environment_assignments(self):
        sources = {
            "renamed/variants/ya.make": (
                "PACKAGE(variant)\n"
                "IF (MODULE_TAG == \"PACKAGE_UNION\")\nPACK(tar)\nNO_NEED_CHECK()\n"
                "ELSE()\nPACK(tar.gz)\nENDIF()\n"
                "IF (UNUSED_MACRO == \"yes\")\nFILES(union.txt)\nELSE()\nFILES(final.txt)\nENDIF()\nEND()\n"
            ),
            "renamed/variants/union.txt": "union",
            "renamed/variants/final.txt": "final",
        }
        graph = lib.make(sources, "renamed/variants")
        final = lib.node_by_output(graph, "$(B)/renamed/variants/variant.final.pkg.fake")
        union = lib.node_by_output(graph, "$(B)/renamed/variants/variant.pkg.fake")
        self.assertIn("$(B)/renamed/variants/variant.tar.gz", final["outputs"])
        self.assertNotIn("$(B)/renamed/variants/variant.tar", final["outputs"])
        self.assertIn("$(B)/renamed/variants/union.txt", union["inputs"])
        self.assertNotIn("$(B)/renamed/variants/final.txt", union["inputs"])
        self.assertIn("$(B)/renamed/variants/final.txt", final["inputs"])

    def test_no_cow_selects_common_link_peer_with_ordered_override(self):
        for setting, selected in (("", "on"), ("NO_COW()", "off"),
                                  ("SET(USE_COW no)", "off"),
                                  ("NO_COW()\nSET(USE_COW yes)", "on"), ("SET(USE_COW other)", None)):
            with self.subTest(setting=setting):
                sources = {"renamed/owner/ya.make": native(setting, "PROGRAM()"),
                           "renamed/owner/probe.cpp": "int main() { return 0; }\n"}
                for choice in ("on", "off"):
                    sources[f"build/cow/{choice}/ya.make"] = native("", "LIBRARY()")
                    sources[f"build/cow/{choice}/probe.cpp"] = "int cow_value;\n"
                graph = lib.make(sources, "renamed/owner")
                final = lib.node_by_output(graph, "$(B)/renamed/owner/owner")
                for choice in ("on", "off"):
                    output = f"$(B)/build/cow/{choice}/libbuild-cow-{choice}.a"
                    self.assertEqual(output in final["inputs"], choice == selected)
                    if choice == selected:
                        self.assertIn(lib.node_by_output(graph, output)["uid"], final["deps"])

    def test_go_no_cow_preserves_autocheck_peers(self):
        for autocheck in ("no", "yes"):
            with self.subTest(autocheck=autocheck):
                sources = go_world()
                sources.update({"renamed/goexe/ya.make": "GO_PROGRAM()\nNO_COW()\nSRCS(main.go)\nEND()\n",
                                "renamed/goexe/main.go": "package main\nfunc main() {}\n"})
                for path in ("build/cow/off", "contrib/libs/libeatmydata"):
                    sources[path + "/ya.make"] = native("")
                    sources[path + "/probe.cpp"] = "int peer_value;\n"
                graph = lib.make(sources, "renamed/goexe", f"-DAUTOCHECK={autocheck}")
                off = lib.node_by_output(graph, "$(B)/build/cow/off/libbuild-cow-off.a")
                self.assertTrue(any(off["outputs"][0] in node["inputs"] for node in graph["graph"]
                                    if node["outputs"][0].startswith("$(B)/renamed/goexe/")))
                outputs = {out for node in graph["graph"] for out in node["outputs"]}
                self.assertEqual("$(B)/contrib/libs/libeatmydata/libcontrib-libs-libeatmydata.a" in outputs,
                                 autocheck == "yes")

    def test_python3_addincl_models_native_environment_and_header_consumers(self):
        for settings, flags, include, system in (
            ("SET(CFLAGS -DEXISTING)\nSET(ADDINCL GLOBAL existing)", ["-DUSE_PYTHON3"], "contrib/libs/python/Include", False),
            ("SET(USE_PYTHON3_PREV yes)", ["-DUSE_PYTHON3", "-DUSE_PYTHON3_PREV"], "contrib/libs/python/Include_prev", False),
            ("SET(USE_ARCADIA_PYTHON no)\nSET(USE_SYSTEM_PYTHON 3.12)", [], None, True),
            ("SET(USE_ARCADIA_PYTHON no)\nSET(PYTHON_INCLUDE -Iexternal/headers)", ["-Iexternal/headers"], None, False),
        ):
            with self.subTest(settings=settings):
                sources = {
                    "renamed/headers/ya.make": native(settings + "\nPYTHON3_ADDINCL()\n"
                        "IF (PYTHON3 == \"yes\" AND PYTHON2 == \"no\" AND _PYTHON_VER == \"py3\" "
                        "AND PYTHON_TYPE_FOR_CYTHON == \"PY3\" AND MODULE_TAG == \"PY3_NATIVE\")\n"
                        "CFLAGS(-DNATIVE_ENV)\nENDIF()"),
                    "renamed/headers/probe.cpp": '#include "Python.h"\n',
                    "renamed/consumer/ya.make": native("PEERDIR(renamed/headers)", "PROGRAM()"),
                    "renamed/consumer/probe.cpp": '#include "Python.h"\n' if include else 'int value;\n',
                }
                if include:
                    sources[include + "/Python.h"] = "#pragma once\n"
                else:
                    sources["renamed/headers/probe.cpp"] = "int value;\n"
                if system:
                    sources["build/platform/python/ya.make"] = "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(SYSTEM_PYTHON sbr:910)\nEND()\n"
                graph = lib.make(sources, "renamed/consumer")
                own = lib.node_by_output(graph, "$(B)/renamed/headers/probe.cpp.o")
                args = own["cmds"][0]["cmd_args"]
                self.assertIn("-DNATIVE_ENV", args)
                for flag in flags:
                    self.assertIn(flag, args)
                peer = lib.node_by_output(graph, "$(B)/renamed/consumer/probe.cpp.o")
                self.assertNotIn("-DNATIVE_ENV", peer["cmds"][0]["cmd_args"])
                if include:
                    for node in (own, peer):
                        self.assertIn("$(S)/" + include + "/Python.h", node["inputs"])
                        self.assertIn("-I$(S)/" + include, node["cmds"][0]["cmd_args"])
                else:
                    self.assertNotIn("-DUSE_PYTHON3", args)
                self.assertFalse(any("runtime_py3" in out for node in graph["graph"] for out in node["outputs"]))
                if system:
                    self.assertIn("sbr:910", lib.node_by_output(graph, "$(B)/resources/SYSTEM_PYTHON")["cmds"][0]["cmd_args"])

    def test_acknowledgement_has_only_its_upstream_environment_effect(self):
        sources = {"renamed/policy/ya.make": native(""), "renamed/policy/probe.cpp": "int value;\n"}
        before = lib.make(sources, "renamed/policy")
        for args in ("", "IGNORED_SCALAR another"):
            with self.subTest(args=args):
                sources["renamed/policy/ya.make"] = native(f"NO_NEED_CHECK({args})")
                self.assertEqual(before, lib.make(sources, "renamed/policy"))

    def test_python3_addincl_expands_configured_header_paths_and_peer_tags(self):
        sources = {
            "renamed/custom/ya.make": native(
                "SET(HEADER_DIR renamed/sdk)\nSET(PY3_BASE_INCLUDE_DIR $HEADER_DIR)\n"
                "SET(_CPP_PROTO_COMPATIBLE_TAGS CPP_PROTO_DIRECT_HEADERS)\n"
                "SET(YQL_UDF_STATIC_PEERDIR_TAG STATIC_VARIANT)\nSET(YQL_ABI_PEERDIR_TAG ABI_VARIANT)\n"
                "PYTHON3_ADDINCL()\nSET(HEADER_DIR later)\n"
                "IF (PEERDIR_TAGS == \"CPP_PROTO_DIRECT_HEADERS CPP_PROTO_FROM_SCHEMA CPP_FBS CPP_ROS PY3_NATIVE STATIC_VARIANT ABI_VARIANT __EMPTY__ RESOURCE_LIB DLL_LIB\")\n"
                "CFLAGS(-DTAGS_OK)\nENDIF()"),
            "renamed/custom/probe.cpp": '#include "Python.h"\n',
            "renamed/sdk/Python.h": "#pragma once\n",
        }
        node = lib.node_by_output(lib.make(sources, "renamed/custom"), "$(B)/renamed/custom/probe.cpp.o")
        self.assertIn("$(S)/renamed/sdk/Python.h", node["inputs"])
        self.assertIn("-DTAGS_OK", node["cmds"][0]["cmd_args"])

    def test_declarative_scalar_macros_reject_missing_or_extra_arguments(self):
        for name in ("PACK", "VALIDATE_DATA_RESTART", "SET_CUDA_NODE_RAM_REQUIEREMENT"):
            for args in ("", "one two"):
                with self.subTest(name=name, args=args):
                    result = lib.make_process({"renamed/arity/ya.make": native(f"{name}({args})"),
                                               "renamed/arity/probe.cpp": "int value;\n"}, "renamed/arity")
                    self.assertNotEqual(result.returncode, 0)
                    self.assertIn(name + " expects one argument", result.stderr)

    def test_pack_is_restricted_to_package_modules(self):
        result = lib.make_process({"renamed/invalid/ya.make": native("PACK(tar)"),
                                   "renamed/invalid/probe.cpp": "int value;\n"}, "renamed/invalid")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("PACK is only allowed in PACKAGE modules", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
