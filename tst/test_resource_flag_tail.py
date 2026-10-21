import unittest

import lib


class ResourceFlagTailTest(unittest.TestCase):
    def test_link_flags_keep_literal_arguments_after_resource_expansion(self):
        files = {
            "app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}PEERDIR(resource)\nSRCS(main.cpp)\nLDFLAGS(-L${{SUPPORT_RESOURCE_GLOBAL}}/lib -lafter_resource)\nEND()\n",
            "app/main.cpp": "int main() {}\n",
            "resource/ya.make": "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(SUPPORT sbr:42)\nEND()\n",
        }
        graph = lib.make(files, "app")
        node = lib.node_by_output(graph, "$(B)/app/app")
        args = next(cmd["cmd_args"] for cmd in node["cmds"] if "$(S)/build/scripts/link_exe.py" in cmd["cmd_args"])
        start = args.index("-L$(B)/resources/SUPPORT/lib")
        self.assertEqual(args[start:start + 2], ["-L$(B)/resources/SUPPORT/lib", "-lafter_resource"])


if __name__ == "__main__":
    unittest.main(verbosity=2)
