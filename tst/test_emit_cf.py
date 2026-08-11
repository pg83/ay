import unittest

import lib


class EmitConfigureFileTest(unittest.TestCase):
    def test_generated_template_and_includes_reach_consumer(self):
        files = {
            "prod/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SRCS(config.h.in)\nEND()\n"
            ),
            "prod/config.h.in": '#include "marker.h"\nint x = @V@;\n',
            "prod/marker.h": "// marker\n",
            "app/ya.make": (
                "PROGRAM()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "PEERDIR(prod)\nSRCS(use.cpp)\nEND()\n"
            ),
            "app/use.cpp": '#include <prod/config.h>\nint main(){return 0;}\n',
            "build/scripts/configure_file.py": "print('configure')\n",
        }
        graph = lib.make(files, "app")
        compile_node = lib.node_by_output(graph, "$(B)/app/use.cpp.o")
        for expected in (
            "$(B)/prod/config.h",
            "$(S)/prod/config.h.in",
            "$(S)/build/scripts/configure_file.py",
            "$(S)/prod/marker.h",
        ):
            self.assertIn(expected, compile_node["inputs"])

    def test_set_and_default_values_reach_configure_command(self):
        files = {
            "thelib/ya.make": (
                "LIBRARY()\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\n"
                "SET(MYVAR hello)\nDEFAULT(MYDEF world)\n"
                "SRCS(lib.cpp x.cpp.in)\nEND()\n"
            ),
            "thelib/lib.cpp": "int f(){return 0;}\n",
            "thelib/x.cpp.in": "int a = @MYVAR@;\nint b = @MYDEF@;\n",
            "build/scripts/configure_file.py": "print('configure')\n",
        }
        graph = lib.make(files, "thelib")
        configure = lib.node_by_output(graph, "$(B)/thelib/x.cpp")
        args = configure["cmds"][0]["cmd_args"]
        self.assertIn("MYVAR=hello", args)
        self.assertIn("MYDEF=world", args)


if __name__ == "__main__":
    unittest.main(verbosity=2)
