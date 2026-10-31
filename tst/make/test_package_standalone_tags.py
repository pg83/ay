from tst.make.lib import MakeTest, PROJECT


class PackageStandaloneTagsTest(MakeTest):
    def test_union_tag_keeps_files_in_the_requested_final_package(self):
        self.write("ya.make", "PACKAGE()\nONLY_TAGS(PACKAGE_FINAL PACKAGE_UNION)\nFILES(value.txt)\nEND()\n")
        self.write("value.txt", "42\n")
        graph = self.graph()
        packages = [node for node in graph["graph"] if node["uid"] in graph["result"]]
        self.assertTrue(any(f"$(B)/{PROJECT}/value.txt" in node["inputs"] for node in packages))
