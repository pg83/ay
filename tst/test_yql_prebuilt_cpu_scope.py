import unittest

import lib
from test_yql_resource_scope import world, resource_bindings, bindings


def fixture():
    files = world()
    files.update({
        "app/ya.make": (
            "YQL_PREBUILT_PROGRAM(starlight)\nSET(STARLIGHT_RESOURCE_DEFAULT 42)\n"
            "SET(USE_SSE4 yes)\nSET(CPU_CHECK yes)\nSET(VALIDATE_DATA yes)\n"
            "SRCS(main.cpp)\nEND()\n"
        ),
        "app/main.cpp": "int main() { return 0; }\n",
        "library/cpp/cpuid_check/ya.make": (
            f"LIBRARY()\n{lib.BARE_MODULE}PEERDIR(cpu_marker)\nEND()\n"
        ),
        "cpu_marker/ya.make": lib.resource_lib("CPU_SENTINEL", 51),
    })
    return files


class YqlPrebuiltCpuScopeTest(unittest.TestCase):
    def test_prebuilt_epilogue_allocator_disables_cpu_peer(self):
        graph = lib.make(fixture(), "app", "-t", opensource=False)
        suite = lib.node_by_output(graph, "$(B)/app/test-results/validate_resource/meta.json")
        self.assertEqual(resource_bindings(suite), bindings("YMAKE_PYTHON3"))
        fetch = lib.node_by_output(graph, "$(B)/app/starlight.prebuilt")
        copy = lib.node_by_output(graph, "$(B)/app/starlight")
        self.assertIn(fetch["uid"], copy["deps"])
        self.assertIn(fetch["outputs"][0], copy["inputs"])

    def test_from_sources_keeps_cpu_peer_and_compiler_resources(self):
        graph = lib.make(fixture(), "app", "-t", "-DBUILD_YQL_FROM_SOURCES=yes", opensource=False)
        self.assertFalse(any("starlight.prebuilt" in output for node in graph["graph"] for output in node["outputs"]))
        cpu = lib.node_by_output(graph, "$(B)/resources/CPU_SENTINEL")
        self.assertTrue(cpu["outputs"])
        lib.node_by_output(graph, "$(B)/app/main.cpp.o")


if __name__ == "__main__":
    unittest.main(verbosity=2)
