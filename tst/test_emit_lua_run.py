import unittest

import lib


class EmitLuaRunTest(unittest.TestCase):
    def test_generated_rl6_feeds_ragel(self):
        files = {
            "lib/dtr/ya.make": (
                "LIBRARY(dtr)\n"
                "SRCS(plain.cpp)\n"
                "RUN_LUA(\n"
                "  gen.lua patterns.rl6\n"
                "  IN data.rl6\n"
                "  OUT patterns.rl6\n"
                ")\n"
                "END()\n"
            ),
            "lib/dtr/plain.cpp": "int p(){return 0;}\n",
            "lib/dtr/gen.lua": "-- stub\n",
            "lib/dtr/data.rl6": "%%{ machine d; }%%\n",
        }
        lib.tool_program(files, "tools/lua", "lua")
        lib.tool_program(files, "contrib/tools/ragel6", "ragel6")
        graph = lib.make(files, "lib/dtr")
        lua = lib.node_by_output(graph, "$(B)/lib/dtr/patterns.rl6")
        ragel = lib.node_by_output(graph, "$(B)/lib/dtr/patterns.rl6.cpp")
        self.assertIn("$(B)/lib/dtr/patterns.rl6", ragel["inputs"])
        self.assertIn(lua["uid"], ragel["deps"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
