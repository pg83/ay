import unittest

from tst import lib


BARE = lib.BARE_MODULE


def udf_fixture():
    files = {}
    for peer in (
        "yql/essentials/public/udf", "yql/essentials/public/udf/support",
        "yql/essentials/udfs/common/python/python_udf", "yql/essentials/udfs/common/python/main_py3",
        "library/python/runtime_py3", "library/python/runtime_py3/main", "library/python/type_info",
        "build/platform/python/ldflags", "contrib/tools/python3/Modules/_sqlite", "library/cpp/resource",
    ):
        files[f"{peer}/ya.make"] = f"LIBRARY()\n{BARE}SRCS(p.cpp)\nEND()\n"
        files[f"{peer}/p.cpp"] = ""
    files["build/scripts/gen_yql_python_udf.py"] = ""
    files["yql/essentials/public/udf/udfs_exports.exports"] = "C Register\n"
    files["yql/essentials/udfs/common/python/python_udf/python_udfs_exports.exports"] = "C Register\n"
    files["yql/essentials/public/udf/service/drop_policy_globals.py"] = ""
    files["contrib/libs/python/Include/Python.h"] = ""
    files["yql/essentials/udfs/common/python/python_udf/python_udf.h"] = ""
    files["yql/essentials/public/udf/udf_registrator.h"] = ""
    for tool in ("tools/fix_elf", "tools/archiver", "tools/rescompiler", "tools/rescompressor", "yql/essentials/tools/udf_probe"):
        lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
    return files


class YqlAbiTest(unittest.TestCase):
    def test_prebuilt_probe_tool_consumes_primary_output_and_is_a_tool(self):
        files = udf_fixture()
        files.update({
            "app/ya.make": f"LIBRARY()\n{BARE}RUN_PROGRAM(tools/probe OUT_NOAUTO data.out)\nRESOURCE(${{BINDIR}}/data.out data)\nEND()\n",
            "tools/probe/ya.make": "SET(SELECT_PREBUILT yes)\nIF (SELECT_PREBUILT)\nYQL_PREBUILT_TOOL()\n"
                "FROM_SANDBOX(FILE 42 RENAME RESOURCE OUT_NOAUTO probe.prebuilt EXECUTABLE)\n"
                "PRIMARY_OUTPUT(${BINDIR}/probe.prebuilt)\nEND()\nELSE()\n"
                f"PROGRAM()\n{BARE}SRCS(main.cpp)\nEND()\nENDIF()\n",
            "tools/probe/main.cpp": "",
        })
        graph = lib.make(files, "app")
        fetch = lib.node_by_output(graph, "$(B)/tools/probe/probe.prebuilt")
        self.assertEqual(fetch["cmds"][0]["cmd_args"][0], "$(B)/resources/YMAKE_PYTHON3/bin/python3")
        tool = lib.node_by_output(graph, "$(B)/tools/probe/probe")
        self.assertEqual(tool["cmds"][0]["cmd_args"][-3:], ["copy", "$(B)/tools/probe/probe.prebuilt", "$(B)/tools/probe/probe"])
        self.assertIn("$(B)/tools/probe/probe.prebuilt", tool["inputs"])
        self.assertIn(fetch["uid"], tool["deps"])
        self.assertEqual(tool["kv"], {"p": "ld", "pc": "light-blue", "show_out": "yes"})
        run = lib.node_by_output(graph, "$(B)/app/data.out")
        self.assertIn(tool["uid"], run["deps"])
        self.assertEqual(run["cmds"][0]["cmd_args"], ["$(B)/tools/probe/probe"])

    def test_prebuilt_yql_tool_primary_output_uses_existing_declared_producers(self):
        files = udf_fixture()
        files.update({
            "tool/ya.make": "YQL_PREBUILT_TOOL(named)\nCOPY_FILE(binary named.prebuilt)\nPRIMARY_OUTPUT(${BINDIR}/named.prebuilt)\nEND()\n",
            "tool/binary": "",
        })
        graph = lib.make(files, "tool")
        producer = lib.node_by_output(graph, "$(B)/tool/named.prebuilt")
        tool = lib.node_by_output(graph, "$(B)/tool/named")
        self.assertIn(producer["uid"], tool["deps"])
        self.assertIn(producer["outputs"][0], tool["inputs"])

    def test_python_and_cpp_consumers_reuse_single_module_codegen(self):
        files = udf_fixture()
        files.update({
            "cpp/ya.make": f"PROGRAM()\n{BARE}PEERDIR(shared)\nEND()\n",
            "py/ya.make": f"PY3_PROGRAM()\n{BARE}NO_PYTHON_INCLUDES()\nNO_IMPORT_TRACING()\nNO_CHECK_IMPORTS()\n"
                "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\nPEERDIR(shared)\nEND()\n",
            "shared/ya.make": f"LIBRARY()\n{BARE}ARCHIVE_ASM(NAME Blob data.bin)\nEND()\n",
            "shared/data.bin": "",
        })
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(files, "cpp", "py", "--target-platform", "default-linux-x86_64")
        producers = [n for n in graph["graph"] if "$(B)/shared/Blob.rodata" in n["outputs"]]
        self.assertEqual(len(producers), 1)
        for node in graph["graph"]:
            if "$(B)/shared/Blob.rodata.o" in node["outputs"]:
                self.assertEqual(len(node["deps"]), len(set(node["deps"])))

    def test_weak_discovery_then_link_reuses_declared_codegen(self):
        files = udf_fixture()
        files.update({
            "dll/ya.make": f"DLL(named)\n{BARE}PEERDIR(shared)\nEND()\n",
            "cpp/ya.make": f"PROGRAM()\n{BARE}PEERDIR(shared)\nEND()\n",
            "shared/ya.make": f"LIBRARY()\n{BARE}ARCHIVE_ASM(NAME Blob data.bin)\nEND()\n",
            "shared/data.bin": "",
        })
        lib.tool_program(files, "contrib/tools/yasm", "yasm")
        graph = lib.make(files, "cpp", "dll", "--target-platform", "default-linux-x86_64")
        lib.node_by_output(graph, "$(B)/cpp/cpp")
        nodes = {node["uid"]: node for node in graph["graph"]}
        rodata = [node for node in graph["graph"] if "$(B)/shared/Blob.rodata.o" in node["outputs"]]
        self.assertTrue(rodata)
        for node in rodata:
            producers = [ref for ref in node["deps"] if "$(B)/shared/Blob.rodata" in nodes[ref]["outputs"]]
            self.assertEqual(len(producers), 1)

    def test_direct_udf_configures_both_static_start_variants(self):
        files = udf_fixture()
        files.update({"udf/ya.make": f"YQL_UDF(named)\n{BARE}UDF_NO_PROBE()\nSRCS(body.cpp)\nEND()\n", "udf/body.cpp": ""})
        graph = lib.make(files, "udf")
        for suffix, archive in ((".udfs.o", "libnamed.global.a"), (".udfs.abi_current.o", "yql-abi-current-named.global.a")):
            obj = lib.node_by_output(graph, "$(B)/udf/body.cpp" + suffix)
            lib.node_by_output(graph, "$(B)/udf/" + archive)
            self.assertNotIn("-DBUILD_UDF", obj["cmds"][0]["cmd_args"])

    def test_contrib_udf_has_independent_shared_target_and_static_peer_contexts(self):
        files = udf_fixture()
        files.update({
            "udf/ya.make": f'YQL_UDF_CONTRIB(named)\n{BARE}UDF_NO_PROBE()\n'
                'COPY_FILE(template.cpp ${BINDIR}/${MODULE_TAG}.cpp)\n'
                'SRCS(body.cpp ${BINDIR}/${MODULE_TAG}.cpp)\n'
                'IF (MODULE_TAG == "YQL_UDF_SHARED")\nPEERDIR(shared_peer)\n'
                'ELSE()\nPEERDIR(static_peer)\nENDIF()\nEND()\n',
            "udf/body.cpp": "int body;\n",
            "udf/template.cpp": "int generated;\n",
            "app/ya.make": f"PROGRAM()\n{BARE}PEERDIR(udf)\nEND()\n",
        })
        for peer in ("shared_peer", "static_peer"):
            files[f"{peer}/ya.make"] = f"LIBRARY()\n{BARE}SRCS(peer.cpp)\nEND()\n"
            files[f"{peer}/peer.cpp"] = "int peer;\n"
        for targets in (("udf", "app"), ("app", "udf")):
            with self.subTest(targets=targets):
                graph = lib.make(files, *targets, "--target-platform", "default-linux-x86_64")
                shared = lib.node_by_output(graph, "$(B)/udf/libnamed.so")
                static = lib.node_by_output(graph, "$(B)/udf/libnamed.global.a")
                app = lib.node_by_output(graph, "$(B)/app/app")
                self.assertIn(static["uid"], app["deps"])
                self.assertNotIn(shared["uid"], app["deps"])
                results = set(graph["result"])
                self.assertIn(shared["uid"], results)
                self.assertNotIn(static["uid"], results)
                for tag, suffix, consumer, peer in (
                    ("YQL_UDF_SHARED", ".pic.o", shared, "shared_peer"),
                    ("YQL_UDF_STATIC", ".udfs.o", static, "static_peer"),
                ):
                    generated = lib.node_by_output(graph, f"$(B)/udf/{tag}.cpp")
                    compiled = lib.node_by_output(graph, f"$(B)/udf/{tag}.cpp{suffix}")
                    body = lib.node_by_output(graph, f"$(B)/udf/body.cpp{suffix}")
                    self.assertIn(generated["outputs"][0], compiled["inputs"])
                    self.assertIn(generated["uid"], compiled["deps"])
                    self.assertIn(compiled["uid"], consumer["deps"])
                    self.assertIn(body["uid"], consumer["deps"])
                    self.assertEqual("-DBUILD_UDF" in body["cmds"][0]["cmd_args"], tag == "YQL_UDF_SHARED")
                    archive = lib.node_by_output(graph, f"$(B)/{peer}/lib{peer}.a")
                    self.assertIn(archive["uid"], shared["deps"] if tag == "YQL_UDF_SHARED" else app["deps"])

    def test_udf_peer_selects_static_global_sources_and_implicit_dependencies(self):
        files = udf_fixture()
        files.update({
            "app/ya.make": f"PROGRAM()\n{BARE}PEERDIR(udf)\nEND()\n",
            "udf/ya.make": f"YQL_UDF(named 1 0)\n{BARE}SRCS(body.cpp)\nEND()\n",
            "udf/body.cpp": "",
        })
        graph = lib.make(files, "app")
        obj = lib.node_by_output(graph, "$(B)/udf/body.cpp.udfs.o")
        archive = lib.node_by_output(graph, "$(B)/udf/libnamed.global.a")
        self.assertIn(obj["uid"], archive["deps"])
        self.assertNotIn("-DBUILD_UDF", obj["cmds"][0]["cmd_args"])
        self.assertFalse(any("$(B)/udf/libnamed.so" in n["outputs"] for n in graph["graph"]))
        for peer in ("yql/essentials/public/udf", "yql/essentials/public/udf/support"):
            lib.node_by_output(graph, f"$(B)/{peer}/p.cpp.o")

    def test_udf_static_variants_expose_their_submodule_tags(self):
        for current in (False, True):
            with self.subTest(current=current):
                files = udf_fixture()
                abi = "YQL_CURRENT_ABI_VERSION()\n" if current else ""
                files.update({
                    "app/ya.make": f"PROGRAM()\n{BARE}{abi}PEERDIR(udf)\nEND()\n",
                    "udf/ya.make": f'YQL_UDF(named)\n{BARE}SRCS(body.cpp)\n'
                        'IF (MODULE_TAG == "YQL_UDF_STATIC_CURRENT")\nCXXFLAGS(-DTAG_CURRENT)\n'
                        'ELSEIF (MODULE_TAG == "YQL_UDF_STATIC")\nCXXFLAGS(-DTAG_STABLE)\nENDIF()\nEND()\n',
                    "udf/body.cpp": "",
                })
                suffix = ".udfs.abi_current.o" if current else ".udfs.o"
                command = lib.node_by_output(lib.make(files, "app"), "$(B)/udf/body.cpp" + suffix)["cmds"][0]["cmd_args"]
                self.assertIn("-DTAG_CURRENT" if current else "-DTAG_STABLE", command)

    def test_udf_direct_target_selects_shared_module_without_probe_when_disabled(self):
        files = udf_fixture()
        files.update({
            "udf/ya.make": f'YQL_UDF(named)\n{BARE}UDF_NO_PROBE()\nSRCS(body.cpp)\n'
                'IF (MODULE_TAG == "YQL_UDF_SHARED")\nCXXFLAGS(-DTAG_SHARED)\nENDIF()\nEND()\n',
            "udf/body.cpp": "",
        })
        graph = lib.make(files, "udf")
        obj = lib.node_by_output(graph, "$(B)/udf/body.cpp.pic.o")
        self.assertIn("-DBUILD_UDF", obj["cmds"][0]["cmd_args"])
        self.assertIn("-DTAG_SHARED", obj["cmds"][0]["cmd_args"])
        link = lib.node_by_output(graph, "$(B)/udf/libnamed.so")
        self.assertIn("$(S)/yql/essentials/public/udf/udfs_exports.exports", link["inputs"])
        plugin = next(n for n in graph["graph"] if n["kv"]["p"] == "CP" and
            "$(S)/yql/essentials/public/udf/service/drop_policy_globals.py" in n["inputs"])
        self.assertIn(plugin["outputs"][0], link["inputs"])
        self.assertFalse(any("$(B)/yql/essentials/tools/udf_probe/udf_probe" in c["cmd_args"] for c in link["cmds"]))

    def test_udf_dll_uses_first_version_argument_as_soname_alias(self):
        files = udf_fixture()
        files.update({"udf/ya.make": f"YQL_UDF(named 4 7)\n{BARE}UDF_NO_PROBE()\nSRCS(body.cpp)\nEND()\n", "udf/body.cpp": ""})
        link = lib.node_by_output(lib.make(files, "udf"), "$(B)/udf/libnamed.so")
        self.assertIn("$(B)/udf/libnamed.so.4", link["outputs"])
        command = next(c["cmd_args"] for c in link["cmds"] if "$(S)/build/scripts/link_dyn_lib.py" in c["cmd_args"])
        self.assertEqual(command[command.index("--soname") + 1], "$(B)/udf/libnamed.so.4")
        self.assertIn("-Wl,-soname,libnamed.so.4", command)
        self.assertNotIn("--objcopy-exe", command)
        self.assertNotIn("$(B)/udf/libnamed.so.4.7", link["outputs"])

    def test_python3_udf_is_a_pic_dll_with_python_runtime_and_custom_exports(self):
        files = udf_fixture()
        files.update({
            "udf/ya.make": f"YQL_PYTHON3_UDF(named)\n{BARE}UDF_NO_PROBE()\nDISABLE(PYTHON_SQLITE3)\nSRCS(body.cpp)\nEND()\n",
            "udf/body.cpp": "",
        })
        graph = lib.make(files, "udf")
        obj = lib.node_by_output(graph, "$(B)/udf/body.cpp.pic.o")
        self.assertIn("-DUSE_PYTHON3", obj["cmds"][0]["cmd_args"])
        link = lib.node_by_output(graph, "$(B)/udf/libnamed.so")
        self.assertIn("$(S)/yql/essentials/udfs/common/python/python_udf/python_udfs_exports.exports", link["inputs"])
        self.assertTrue(any(o.startswith("$(B)/library/python/runtime_py3/main/") for n in graph["graph"] for o in n["outputs"]))
        self.assertFalse(any("/Modules/_sqlite/" in o for n in graph["graph"] for o in n["outputs"]))

    def test_shared_udf_probe_obeys_configuration_and_is_a_tool_dependency(self):
        variants = [([], True), (["--target-platform", "default-linux-x86_64"], True), (["-DUDF_NO_PROBE=yes"], False), (["-DSANITIZER_TYPE=address"], False),
            (["-DUSE_ARCADIA_PYTHON=no"], False), (["-DMUSL=yes"], False), (["-DCPP_ANALYSIS_MODE=yes"], False)]
        for flags, enabled in variants:
            with self.subTest(flags=flags):
                files = udf_fixture()
                files.update({"udf/ya.make": f"YQL_UDF(named)\n{BARE}SRCS(body.cpp)\nEND()\n", "udf/body.cpp": ""})
                graph = lib.make(files, "udf", *flags)
                link = lib.node_by_output(graph, "$(B)/udf/libnamed.so")
                commands = [c["cmd_args"] for c in link["cmds"] if c["cmd_args"][0] == "$(B)/yql/essentials/tools/udf_probe/udf_probe"]
                self.assertEqual(commands, [["$(B)/yql/essentials/tools/udf_probe/udf_probe", "$(B)/udf/libnamed.so"]] if enabled else [])
                self.assertEqual(link["kv"]["p"], "UD" if enabled else "LD")
                if enabled:
                    probe = lib.node_by_output(graph, "$(B)/yql/essentials/tools/udf_probe/udf_probe")
                    self.assertIn(probe["uid"], link["deps"])
                    self.assertIn(probe["outputs"][0], link["inputs"])

    def test_udf_symbol_binding_and_probe_use_their_own_command_context(self):
        for declaration in ("YQL_UDF", "YQL_UDF_CONTRIB", "YQL_PYTHON3_UDF"):
            with self.subTest(declaration=declaration):
                files = udf_fixture()
                files.update({
                    "udf/ya.make": f"{declaration}(named)\n{BARE}DISABLE(PYTHON_SQLITE3)\n"
                        "LDFLAGS(-Wl,--own-link-marker)\nPEERDIR(peer)\nSRCS(body.cpp)\nEND()\n",
                    "udf/body.cpp": "int body;\n",
                    "peer/ya.make": f"LIBRARY()\n{BARE}LDFLAGS(GLOBAL -Wl,--peer-link-marker)\nEND()\n",
                })
                graph = lib.make(files, "udf", "--target-platform", "default-linux-x86_64")
                link = lib.node_by_output(graph, "$(B)/udf/libnamed.so")
                command = next(c["cmd_args"] for c in link["cmds"] if "$(S)/build/scripts/link_dyn_lib.py" in c["cmd_args"])
                with self.subTest(command="link"):
                    index = command.index("-Wl,-Bsymbolic")
                    self.assertEqual(command[index - 1:index + 2], ["-Wl,-z,notext", "-Wl,-Bsymbolic", "-fPIC"])
                    self.assertEqual(command.count("-Wl,-Bsymbolic"), 1)
                    self.assertLess(index, command.index("-Wl,--peer-link-marker"))
                    self.assertLess(command.index("-Wl,--peer-link-marker"), command.index("-Wl,--own-link-marker"))
                probe = lib.node_by_output(graph, "$(B)/yql/essentials/tools/udf_probe/udf_probe")
                self.assertIn(probe["uid"], link["deps"])
                self.assertIn(probe["outputs"][0], link["inputs"])
                self.assertEqual(link["cmds"][-1], {
                    "cmd_args": [probe["outputs"][0], "$(B)/udf/libnamed.so"],
                    "env": {"ARCADIA_ROOT_DISTBUILD": "$(S)"},
                })

    def test_python2_udf_uses_its_runtime_and_python2_headers(self):
        files = udf_fixture()
        files.update({
            "udf/ya.make": f"YQL_PYTHON_UDF(named)\n{BARE}UDF_NO_PROBE()\nDISABLE(PYTHON_SQLITE3)\nSRCS(body.cpp)\nEND()\n",
            "udf/body.cpp": "",
            "library/python/runtime/main/ya.make": f"LIBRARY()\n{BARE}SRCS(main.cpp)\nEND()\n",
            "library/python/runtime/main/main.cpp": "",
        })
        graph = lib.make(files, "udf")
        command = lib.node_by_output(graph, "$(B)/udf/body.cpp.pic.o")["cmds"][0]["cmd_args"]
        self.assertIn("-DARCADIA_PYTHON_UNICODE_SIZE=4", command)
        self.assertNotIn("-DUSE_PYTHON3", command)
        self.assertTrue(any(o.startswith("$(B)/library/python/runtime/main/") for n in graph["graph"] for o in n["outputs"]))

    def test_registration_defaults_and_system_flavor_follow_unit_configuration(self):
        for body, flavor, name in (("REGISTER_YQL_PYTHON_UDF()", "Arcadia", "CustomPython"),
            ("SET(USE_ARCADIA_PYTHON no)\nREGISTER_YQL_PYTHON_UDF(NAME SystemPython37)", "System", "SystemPython37")):
            with self.subTest(body=body):
                files = udf_fixture()
                files["udf/ya.make"] = f"LIBRARY()\n{BARE}{body}\nEND()\n"
                graph = lib.make(files, "udf")
                command = lib.node_by_output(graph, f"$(B)/udf/{name}.yql_python_udf.cpp")["cmds"][0]["cmd_args"]
                start = command.index("$(S)/build/scripts/gen_yql_python_udf.py")
                self.assertEqual(command[start + 1:start + 4], [flavor, name, name])
        files = udf_fixture()
        files["udf/ya.make"] = f"LIBRARY()\n{BARE}REGISTER_YQL_PYTHON_UDF(NAME Invalid)\nEND()\n"
        result = lib.make_process(files, "udf")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Invalid python UDF module name", result.stderr)

    def test_registration_uses_run_python_producer_and_declared_output_includes(self):
        files = udf_fixture()
        files["udf/ya.make"] = f"LIBRARY()\n{BARE}REGISTER_YQL_PYTHON_UDF(NAME CustomPythonOne RESOURCE_NAME Package)\nEND()\n"
        graph = lib.make(files, "udf")
        node = lib.node_by_output(graph, "$(B)/udf/CustomPythonOne.yql_python_udf.cpp")
        command = node["cmds"][0]["cmd_args"]
        start = command.index("$(S)/build/scripts/gen_yql_python_udf.py")
        self.assertEqual(command[start + 1:start + 4], ["Arcadia", "CustomPythonOne", "Package"])
        self.assertEqual(command[-1], "0")
        compiled = lib.node_by_output(graph, "$(B)/udf/CustomPythonOne.yql_python_udf.cpp.o")
        self.assertIn(node["uid"], compiled["deps"])
        self.assertIn("$(S)/yql/essentials/public/udf/udf_registrator.h", compiled["inputs"])

    def test_consumers_select_distinct_transitive_abi_variants(self):
        files = {
            "stable/ya.make": f"PROGRAM()\n{BARE}PEERDIR(a)\nEND()\n",
            "current/ya.make": f"PROGRAM()\n{BARE}YQL_CURRENT_ABI_VERSION()\nPEERDIR(a)\nEND()\n",
            "a/ya.make": f"YQL_LIBRARY()\n{BARE}SRCS(a.cpp)\nPEERDIR(b)\nEND()\n",
            "a/a.cpp": '#include "a.h"\n',
            "a/a.h": "",
            "b/ya.make": f"YQL_LIBRARY()\n{BARE}SRCS(b.cpp)\nEND()\n",
            "b/b.cpp": "",
        }
        graph = lib.make(files, "stable", "current")
        for variant in ("stable", "current"):
            for module in ("a", "b"):
                obj = lib.node_by_output(graph, f"$(B)/{module}/{module}.cpp.abi_{variant}.o")
                args = obj["cmds"][0]["cmd_args"]
                self.assertIn(f"-DUSE_{variant.upper()}_UDF_ABI_VERSION", args)
                self.assertNotIn(f"-DUSE_{'CURRENT' if variant == 'stable' else 'STABLE'}_UDF_ABI_VERSION", args)
                archive = lib.node_by_output(graph, f"$(B)/{module}/yql-abi-{variant}-{module}.a")
                self.assertIn(obj["uid"], archive["deps"])
                program = lib.node_by_output(graph, f"$(B)/{variant}/{variant}")
                self.assertIn(archive["outputs"][0], program["inputs"])
        stable = lib.node_by_output(graph, "$(B)/a/a.cpp.abi_stable.o")
        current = lib.node_by_output(graph, "$(B)/a/a.cpp.abi_current.o")
        self.assertEqual(stable["inputs"], current["inputs"])

    def test_abi_flag_is_appended_to_cxxflags(self):
        # yql_udf.conf: the YQL_LIBRARY variants SET_APPEND(CXXFLAGS
        # $YQL_ABI_FLAGS), after the configured CXXFLAGS; ydb
        # .abi_stable.o compiles end their C++ flags with -nostdinc++
        # -DCATBOOST_OPENSOURCE=yes -nostdinc++ -DUSE_STABLE_UDF_ABI_VERSION.
        files = {
            "p/ya.make": f"PROGRAM()\n{BARE}PEERDIR(a)\nEND()\n",
            "a/ya.make": f"YQL_LIBRARY()\n{BARE}CXXFLAGS(-DOWN)\nSRCS(a.cpp)\nEND()\n",
            "a/a.cpp": "",
        }
        args = lib.node_by_output(lib.make(files, "p"), "$(B)/a/a.cpp.abi_stable.o")["cmds"][0]["cmd_args"]
        flag = args.index("-DUSE_STABLE_UDF_ABI_VERSION")
        self.assertLess(args.index("-DOWN"), flag)
        self.assertNotIn("-nostdinc++", args[flag:])

    def test_abi_flag_closes_the_bitcode_cxxflags(self):
        # The LLVM_COMPILE_CXX of ydb yt_codec_bc.cpp.16.bc renders the same
        # CXXFLAGS order as the object compile.
        files = {
            "p/ya.make": f"PROGRAM()\n{BARE}PEERDIR(a)\nEND()\n",
            "a/ya.make": f"YQL_LIBRARY()\n{BARE}USE_LLVM_BC20()\nLLVM_BC(a.cpp NAME n)\nEND()\n",
            "a/a.cpp": "",
            "build/platform/clang/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CLANG20 sbr:1)\nEND()\n"
            ),
            "build/platform/python/ymake_python3/ya.make": (
                "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(YMAKE_PYTHON3 sbr:2)\nEND()\n"
            ),
        }
        lib.tool_program(files, "tools/rescompiler", "rescompiler")
        lib.tool_program(files, "tools/rescompressor", "rescompressor")
        graph = lib.make(files, "p", "--target-platform", "default-linux-x86_64")
        args = lib.node_by_output(graph, "$(B)/a/a.cpp.abi_stable.bc")["cmds"][0]["cmd_args"]
        self.assertNotIn("-nostdinc++", args[args.index("-DUSE_STABLE_UDF_ABI_VERSION"):])

    def test_direct_library_selects_stable_variant(self):
        graph = lib.make({
            "a/ya.make": f"YQL_LIBRARY()\n{BARE}SRCS(a.cpp)\nEND()\n",
            "a/a.cpp": "",
        }, "a")
        lib.node_by_output(graph, "$(B)/a/a.cpp.abi_stable.o")
        self.assertFalse(any("abi_current" in o for n in graph["graph"] for o in n["outputs"]))

    def test_prebuilt_program_falls_back_to_declared_sources(self):
        graph = lib.make({
            "app/ya.make": (
                f"YQL_PREBUILT_PROGRAM(named)\n{BARE}SET(NAMED_RESOURCE_DEFAULT 42)\n"
                "SRCS(main.cpp)\nPEERDIR(dep)\nEND()\n"
            ),
            "app/main.cpp": "",
            "dep/ya.make": f"LIBRARY()\n{BARE}SRCS(dep.cpp)\nEND()\n",
            "dep/dep.cpp": "",
        }, "app")
        lib.node_by_output(graph, "$(B)/app/named")
        lib.node_by_output(graph, "$(B)/app/main.cpp.o")
        lib.node_by_output(graph, "$(B)/dep/dep.cpp.o")

    def test_prebuilt_program_selects_sanitizer_resource_and_discards_source_peers(self):
        for sanitizer, resource in (("", "42"), ("address", "43"), ("memory", "44"), ("thread", "45"), ("undefined", "46")):
            with self.subTest(sanitizer=sanitizer):
                graph = lib.make({
                    "app/ya.make": (
                        "YQL_PREBUILT_PROGRAM(named)\n"
                        "SET(NAMED_RESOURCE_DEFAULT 42)\nSET(NAMED_RESOURCE_ASAN 43)\n"
                        "SET(NAMED_RESOURCE_MSAN 44)\nSET(NAMED_RESOURCE_TSAN 45)\nSET(NAMED_RESOURCE_UBSAN 46)\n"
                        "SRCS(missing.cpp)\nPEERDIR(missing/peer)\nEND()\n"
                    ),
                }, "app", f"-DSANITIZER_TYPE={sanitizer}", opensource=False)
                fetch = lib.node_by_output(graph, "$(B)/app/named.prebuilt")
                args = fetch["cmds"][0]["cmd_args"]
                self.assertEqual(args[0], "$(B)/resources/YMAKE_PYTHON3/bin/python3")
                self.assertEqual(args[args.index("--resource-id") + 1], resource)
                self.assertIn("--executable", args)
                program = lib.node_by_output(graph, "$(B)/app/named")
                self.assertIn(fetch["uid"], program["deps"])
                self.assertIn("$(B)/app/named.prebuilt", program["inputs"])
                self.assertEqual(program["cmds"][0]["cmd_args"][-3:], [
                    "copy", "$(B)/app/named.prebuilt", "$(B)/app/named",
                ])

    def test_prebuilt_program_falls_back_to_sources_without_a_matching_resource(self):
        for resource, sanitizer in (("", ""), ("None", ""), ("42", "unsupported")):
            with self.subTest(resource=resource, sanitizer=sanitizer):
                files = udf_fixture()
                files.update({
                    "app/ya.make": f"YQL_PREBUILT_PROGRAM(named)\n{BARE}SET(NAMED_RESOURCE_DEFAULT {resource})\nSRCS(main.cpp)\nEND()\n",
                    "app/main.cpp": "int main() { return 0; }\n",
                })
                graph = lib.make(files, "app", f"-DSANITIZER_TYPE={sanitizer}", opensource=False)
                obj = lib.node_by_output(graph, "$(B)/app/main.cpp.o")
                binary = lib.node_by_output(graph, "$(B)/app/named")
                self.assertIn(obj["uid"], binary["deps"])
                self.assertFalse(any("$(B)/app/named.prebuilt" in n["outputs"] for n in graph["graph"]))

    def test_python_udfs_include_sqlite_runtime_by_default(self):
        for module, sqlite, runtime in (
            ("YQL_PYTHON_UDF", "contrib/tools/python/src/Modules/_sqlite", "library/python/runtime/main"),
            ("YQL_PYTHON3_UDF", "contrib/tools/python3/Modules/_sqlite", "library/python/runtime_py3/main"),
        ):
            with self.subTest(module=module):
                files = udf_fixture()
                files["udf/ya.make"] = f"{module}(named)\n{BARE}UDF_NO_PROBE()\nSRCS(body.cpp)\nEND()\n"
                files["udf/body.cpp"] = ""
                for peer in (sqlite, runtime):
                    files[f"{peer}/ya.make"] = f"LIBRARY()\n{BARE}SRCS(p.cpp)\nEND()\n"
                    files[f"{peer}/p.cpp"] = ""
                graph = lib.make(files, "udf")
                lib.node_by_output(graph, f"$(B)/{sqlite}/p.cpp.pic.o")
                link = lib.node_by_output(graph, "$(B)/udf/libnamed.so")
                self.assertTrue(any(p.startswith(f"$(B)/{sqlite}/") for p in link["inputs"]))

    def test_internal_libra_registration_adds_header_and_libraries(self):
        files = udf_fixture()
        files["app/ya.make"] = f"PROGRAM()\n{BARE}PEERDIR(udf)\nEND()\n"
        files["udf/ya.make"] = f"LIBRARY()\n{BARE}REGISTER_YQL_PYTHON_UDF(NAME CustomPythonLibra ADD_LIBRA_MODULES yes)\nEND()\n"
        for peer in ("quality/user_sessions/libra_arc/noyql", "yql/udfs/quality/libra/module"):
            files[f"{peer}/ya.make"] = f"LIBRARY()\n{BARE}SRCS(p.cpp)\nEND()\n"
            files[f"{peer}/p.cpp"] = ""
        files["yql/udfs/quality/libra/module/module.h"] = ""
        graph = lib.make(files, "app", opensource=False)
        generated = lib.node_by_output(graph, "$(B)/udf/CustomPythonLibra.yql_python_udf.cpp")
        self.assertEqual(generated["cmds"][0]["cmd_args"][-1], "1")
        obj = lib.node_by_output(graph, "$(B)/udf/CustomPythonLibra.yql_python_udf.cpp.o")
        self.assertIn("$(S)/yql/udfs/quality/libra/module/module.h", obj["inputs"])
        for peer in ("quality/user_sessions/libra_arc/noyql", "yql/udfs/quality/libra/module"):
            lib.node_by_output(graph, f"$(B)/{peer}/p.cpp.o")

    def test_stable_abi_macros_follow_source_configuration_revision(self):
        for modern in (False, True):
            files = {
                "a/ya.make": f"LIBRARY()\n{BARE}YQL_LAST_ABI_VERSION()\nYQL_ABI_VERSION(2 40 1)\nSRCS(a.cpp)\nEND()\n",
                "a/a.cpp": "",
            }
            if modern:
                files["build/conf/project_specific/yql_udf.conf"] = "macro YQL_STABLE_ABI_VERSION() {\n}\n"
            node = lib.node_by_output(lib.make(files, "a"), "$(B)/a/a.cpp.o")
            args = node["cmds"][0]["cmd_args"]
            self.assertIn("-DUSE_STABLE_UDF_ABI_VERSION" if modern else "-DUSE_CURRENT_UDF_ABI_VERSION", args)
            self.assertIn("-DMIN_UDF_ABI_VERSION_MINOR=40" if modern else "-DUDF_ABI_VERSION_MINOR=40", args)

    def test_udf_test_is_a_python3_pytest_program_with_the_yql_test_tools(self):
        files = {
            "udf/ya.make": f"PROGRAM()\n{BARE}SRCS(u.cpp)\nEND()\nRECURSE_FOR_TESTS(test)\n",
            "udf/u.cpp": "int main(){return 0;}\n",
            "udf/test/ya.make": (
                f"YQL_UDF_TEST()\n{BARE}NO_CHECK_IMPORTS()\nNO_IMPORT_TRACING()\nNO_PYTHON_INCLUDES()\n"
                "DISABLE(PYTHON_SQLITE3)\nALLOCATOR(FAKE)\nSIZE(MEDIUM)\nDEPENDS(udf)\nEND()\n"
            ),
        }
        for peer in ["library/python/pytest", "yql/essentials/tests/common/udf_test", "library/python/runtime_py3/main", "library/cpp/resource"]:
            files[f"{peer}/ya.make"] = f"LIBRARY()\n{BARE}SRCS(p.cpp)\nEND()\n"
            files[f"{peer}/p.cpp"] = ""
        tools = ["yql/essentials/tools/astdiff", "yql/tools/yqlrun", "yql/essentials/tools/udf_resolver"]
        for tool in ["tools/archiver", "tools/py3cc", "tools/py3cc/slow", "tools/rescompiler", "tools/rescompressor", *tools]:
            lib.tool_program(files, tool, tool.rsplit("/", 1)[-1])
        graph = lib.make(files, "udf", "-t")
        binary = lib.node_by_output(graph, "$(B)/udf/test/udf-test")
        self.assertIn("$(B)/library/python/pytest/liblibrary-python-pytest.a", binary["inputs"])
        self.assertIn("$(B)/yql/essentials/tests/common/udf_test/libtests-common-udf_test.a", binary["inputs"])
        self.assertIn("PY_MAIN=library.python.pytest.main:main", " ".join(a for n in graph["graph"] for c in n["cmds"] for a in c["cmd_args"]))
        run = lib.node_by_output(graph, "$(B)/udf/test/test-results/py3test/meta.json")
        self.assertIn(binary["uid"], run["deps"])
        dep_outputs = {o for n in graph["graph"] if n["uid"] in run["deps"] for o in n["outputs"]}
        for dep in [*tools, "udf"]:
            self.assertIn(f"$(B)/{dep}/{dep.rsplit('/', 1)[-1]}", dep_outputs)
        args = run["cmds"][0]["cmd_args"]
        self.assertEqual(args[args.index("--test-type") + 1], "py3test")
        self.assertEqual([args[i + 1] for i, a in enumerate(args) if a == "--env"], [
            "YQL_ASTDIFF_PATH=yql/essentials/tools/astdiff/astdiff",
            "YQL_CONFIG_DIR=yql/essentials/cfg/udf_test",
            "YQL_YQLRUN_PATH=yql/tools/yqlrun/yqlrun",
            "YQL_SQL2YQL_PATH=yql/essentials/tools/sql2yql/sql2yql",
            "YQL_UDFRESOLVER_PATH=yql/essentials/tools/udf_resolver/udf_resolver",
        ])
        self.assertIn("$(S)/yql/essentials/cfg/udf_test", [args[i + 1] for i, a in enumerate(args) if a == "--test-related-path"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
