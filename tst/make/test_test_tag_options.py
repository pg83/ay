from tst.make.lib import MakeTest, PROJECT


class TestTagOptionsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "RECURSE(suite app)\n")
        self.write("suite/test_answer.py", "def test_answer(): assert True\n")
        self.write("app/ya.make", "PROGRAM(answer)\nSRCS(main.cpp)\nEND()\n")
        self.write("app/main.cpp", "int main() { return 0; }\n")

    def suite(self, tags=""):
        self.write("suite/ya.make", "PY3TEST()\nTEST_SRCS(test_answer.py)\n" + (f"TAG({tags})\n" if tags else "") + "END()\n")

    def test_environment_tag_filters_select_the_suite(self):
        self.suite("e2e")
        self.node(self.graph("-t", env={"YA_TEST_TAG": "e2e"}), "suite/test-results/py3test/meta.json")

    def test_empty_tag_filter_terms_do_not_hide_matching_suites(self):
        self.suite("e2e")
        self.node(self.graph("-t", "--test-tag=,e2e,,"), "suite/test-results/py3test/meta.json")

    def test_intersecting_include_and_exclude_tags_are_rejected(self):
        self.suite("e2e")
        self.assertNotEqual(self.make("-t", "--test-tag=e2e", "--test-tag=-e2e").returncode, 0)

    def test_anytag_includes_manual_suites(self):
        self.suite("ya:manual")
        self.node(self.graph("-t", "--test-tag=ya:anytag"), "suite/test-results/py3test/meta.json")

    def test_excluded_tags_remove_the_run_node(self):
        self.suite("e2e")
        graph = self.graph("-t", "--test-tag=-e2e")
        outputs = [path for node in graph["graph"] for path in node["outputs"]]
        self.assertNotIn(f"$(B)/{PROJECT}/suite/test-results/py3test/meta.json", outputs)
