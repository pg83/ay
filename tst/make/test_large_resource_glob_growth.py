from tst.make.lib import MakeTest


class LargeResourceGlobGrowthTest(MakeTest):
    def test_large_resource_globs_keep_all_files_while_growing_their_seen_set(self):
        count = 65536
        matched = 8192
        self.write("ya.make", "LIBRARY()\nNO_UTIL()\nNO_RUNTIME()\nSET(NEED_PLATFORM_PEERDIRS no)\nALL_RESOURCE_FILES(txt PREFIX /data/ data)\nEND()\n")
        directory = self.project / "data"
        directory.mkdir()
        for index in range(count):
            suffix = "txt" if index < matched else "skip"
            (directory / f"{index:05d}.{suffix}").write_text("42\n")
        graph = self.graph(env={"AY_CHAOS": "intern-shuffle=0"})
        prefix = f"$(S)/{self.project_name}/data/"
        actual = {path for node in graph["graph"] for path in node["inputs"] if path.startswith(prefix)}
        expected = {prefix + f"{index:05d}.txt" for index in range(matched)}
        self.assertEqual(actual, expected)
