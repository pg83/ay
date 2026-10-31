from tst.make.lib import MakeTest


class MallocApiRuntimeFlagsTest(MakeTest):
    def test_malloc_api_builds_without_exported_runtime_flags(self):
        api = "library/cpp/malloc/api"
        self.write_tree(api + "/ya.make", "LIBRARY(api)\nNO_RUNTIME()\nNO_UTIL()\nSET(NEED_PLATFORM_PEERDIRS no)\nSET(COMMON_LINK_SETTINGS no)\nSRCS(__ay_e2e_api__.cpp)\nEND()\n")
        self.write_tree(api + "/__ay_e2e_api__.cpp", "int e2e_malloc_api() { return 42; }\n")
        self.write("ya.make", f"PROGRAM(answer)\nPEERDIR({api})\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int main() { return 0; }\n")
        graph = self.graph()
        compiled = [node for node in graph["graph"] if f"$(S)/{api}/__ay_e2e_api__.cpp" in node["inputs"] and any(path.endswith(".o") for path in node["outputs"])]
        self.assertEqual(len(compiled), 1)
        args = [arg for command in compiled[0]["cmds"] for arg in command["cmd_args"]]
        self.assertIn("-nostdinc++", args)
