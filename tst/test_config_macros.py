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


if __name__ == "__main__":
    unittest.main(verbosity=2)
