import json

from tst.make.lib import MakeTest


class ToolchainRequiredMetadataTest(MakeTest):
    def check_missing_field(self, field):
        config = json.loads((self.root / "build/ya.conf.json").read_text())
        removed = 0
        for chain in config["toolchain"].values():
            if "c++" not in chain.get("tools", {}):
                continue
            container = chain["tools"]["c++"] if field == "bottle" else chain["params"]
            if field in container:
                del container[field]
                removed += 1
        self.assertGreater(removed, 0)
        self.write_tree("build/ya.conf.json", json.dumps(config))
        self.write("ya.make", "LIBRARY()\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", "int answer() { return 42; }\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_bundled_cpp_toolchains_require_a_bottle_reference(self):
        self.check_missing_field("bottle")

    def test_bundled_cpp_toolchains_require_a_match_root(self):
        self.check_missing_field("match_root")
