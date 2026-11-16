from tst.make import toolchains
from tst.make.lib import MakeTest


class ToolchainPlatformDefaultsTest(MakeTest):
    def test_a_missing_target_platform_defaults_to_the_selected_linux_host(self):
        selected = toolchains.default_linux_clang(self)
        self.assertIsNotNone(selected, "fixture tree needs a default Linux C++ toolchain")
        path, name, _, platform = selected
        files = toolchains.cxx_files(self)
        for config in files.values():
            config["toolchains"] = {key: value for key, value in config["toolchains"].items() if not toolchains.is_cxx(value) or key == name}
        chain = files[path]["toolchains"][name]
        chain["platforms"] = [dict(platform)]
        del chain["platforms"][0]["target"]
        toolchains.write(self, files)
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        compiled = self.node(self.graph("-DOS_SDK=local"), "answer.cpp.o")
        self.assertIn(f"$(S)/{self.project_name}/answer.cpp", compiled["inputs"])
