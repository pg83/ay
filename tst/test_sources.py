import unittest

import lib


MODULE = "geobase/library/abi"


def library(*sources):
    return (
        "LIBRARY()\n"
        "NO_LIBC()\n"
        "NO_RUNTIME()\n"
        "NO_UTIL()\n"
        f"SRCS({' '.join(sources)})\n"
        "END()\n"
    )


def cc_inputs(graph):
    return {
        source
        for node in graph["graph"]
        if node.get("kv", {}).get("p") == "CC"
        for source in node.get("inputs", [])
    }


class SourcesTest(unittest.TestCase):
    def test_root_qualified_and_module_relative_sources(self):
        graph = lib.make({
            f"{MODULE}/ya.make": library(
                "local.cpp",
                "geobase/library/asset.cpp",
            ),
            f"{MODULE}/local.cpp": "int local(){return 0;}\n",
            "geobase/library/asset.cpp": "int asset(){return 1;}\n",
        }, MODULE)
        inputs = cc_inputs(graph)
        self.assertIn("$(S)/geobase/library/abi/local.cpp", inputs)
        self.assertIn("$(S)/geobase/library/asset.cpp", inputs)
        self.assertNotIn(
            "$(S)/geobase/library/abi/geobase/library/asset.cpp",
            inputs,
        )

    def test_ambiguous_bare_source_prefers_module_relative(self):
        graph = lib.make({
            f"{MODULE}/ya.make": library("shared.cpp"),
            "shared.cpp": "int root_shared(){return 0;}\n",
            f"{MODULE}/shared.cpp": "int module_shared(){return 1;}\n",
        }, MODULE)
        inputs = cc_inputs(graph)
        self.assertIn("$(S)/geobase/library/abi/shared.cpp", inputs)
        self.assertNotIn("$(S)/shared.cpp", inputs)


if __name__ == "__main__":
    unittest.main(verbosity=2)
