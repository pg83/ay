import unittest

import lib


class LuaCompileTest(unittest.TestCase):
    def test_lua_compilers_produce_global_objects_and_runtime_peers(self):
        for macro, runtime in (
            ("COMPILE_LUA", "luajit"),
            ("COMPILE_LUA_21", "luajit_21"),
            ("COMPILE_LUA_OPENRESTY", "luajit_openresty"),
        ):
            with self.subTest(macro=macro):
                peer = f"contrib/libs/{runtime}"
                files = {
                    "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(mod)\nEND()\n",
                    "mod/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}{macro}(sub/a.lua NAME pkg.a)\nEND()\n",
                    "mod/sub/a.lua": "return {}\n",
                    f"{peer}/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}SRCS(runtime.c)\nEND()\n",
                    f"{peer}/runtime.c": "",
                }
                lib.tool_program(files, f"{peer}/compiler", "compiler")
                graph = lib.make(files, "app")
                node = lib.node_by_output(graph, "$(B)/mod/sub/a.lua.o")
                self.assertEqual(node["kv"], {"p": "LJ", "pc": "light-cyan"})
                self.assertEqual(node["cmds"][0]["cmd_args"], [
                    f"$(B)/{peer}/compiler/compiler", "-b", "-g",
                    "$(S)/mod/sub/a.lua", "-n", "pkg.a", "$(B)/mod/sub/a.lua.o",
                ])
                self.assertEqual(node["cmds"][0]["cwd"], f"$(S)/{peer}")
                archive = lib.node_by_output(graph, "$(B)/mod/libmod.global.a")
                self.assertIn(node["uid"], archive["deps"])
                lib.node_by_output(graph, f"$(B)/{peer}/runtime.c.o")

class ConfigurationTest(unittest.TestCase):
    def test_build_root_addincl_preserves_root_type(self):
        graph = lib.make({
            "a/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}ADDINCL(GLOBAL ${{ARCADIA_BUILD_ROOT}})\nSRCS(a.cpp)\nEND()\n",
            "a/a.cpp": "",
        }, "a")
        args = lib.node_by_output(graph, "$(B)/a/a.cpp.o")["cmds"][0]["cmd_args"]
        self.assertNotIn("-I$(S)/$(B)", args)
        self.assertGreater(args.count("-I$(B)"), 1)

    def test_debian_version_is_a_lazy_header_generator(self):
        graph = lib.make({
            "a/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}DEB_VERSION(pkg/changelog)\nSRCS(a.cpp)\nEND()\n",
            "a/a.cpp": '#include "DebianVersion.h"\n',
            "pkg/changelog": "package (1.2) stable; urgency=low\n",
            "build/scripts/mkver.py": "",
        }, "a")
        node = lib.node_by_output(graph, "$(B)/a/DebianVersion.h")
        self.assertEqual(node["kv"], {"p": "CL", "pc": "yellow"})
        self.assertEqual(node["cmds"][0]["cmd_args"][-2:], [
            "$(S)/build/scripts/mkver.py", "$(S)/pkg/changelog",
        ])
        self.assertEqual(node["cmds"][0]["stdout"], "$(B)/a/DebianVersion.h")
        self.assertIn("$(S)/pkg/changelog", node["inputs"])
        self.assertIn(node["uid"], lib.node_by_output(graph, "$(B)/a/a.cpp.o")["deps"])

    def test_custom_clang_tidy_declares_resource_peer_and_variables(self):
        graph = lib.make({
            "a/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}SET_CUSTOM_CLANG_TIDY(tools/tidy CUSTOM)\n"
                "IF(CUSTOM_CLANG_TIDY)\nSRCS(a.cpp)\nENDIF()\n"
                "RUN_PYTHON3(gen.py ${CLANG_TIDY_BIN} ${_YAML2JSON_BIN} OUT gen.h)\nEND()\n"
            ),
            "a/a.cpp": '#include "gen.h"\n',
            "a/gen.py": "",
            "tools/tidy/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(CUSTOM sbr:42)\nEND()\n",
        }, "a")
        lib.node_by_output(graph, "$(B)/a/a.cpp.o")
        node = lib.node_by_output(graph, "$(B)/a/gen.h")
        args = node["cmds"][0]["cmd_args"]
        self.assertIn("$(B)/resources/CUSTOM/bin/clang-tidy", args)
        self.assertIn("$(B)/resources/CUSTOM/bin/yaml2json", args)

    def test_proto_package_name_and_custom_style_are_valid_in_cpp_variant(self):
        graph = lib.make({
            "a/ya.make": (
                f"LIBRARY()\n{lib.BARE_MODULE}TS_PROTO_PACKAGE_NAME(@scope/proto)\n"
                "STYLE_CPP_USER_SESSIONS(CONFIG_TYPE custom)\nSRCS(a.cpp)\nEND()\n"
            ),
            "a/a.cpp": "",
        }, "a")
        lib.node_by_output(graph, "$(B)/a/a.cpp.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
