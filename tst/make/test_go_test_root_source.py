from tst.make.lib import MakeTest


class GoTestRootSourceTest(MakeTest):
    def test_go_test_rejects_the_source_root_as_a_test_source(self):
        self.write("ya.make", "GO_TEST(answer)\nGO_TEST_SRCS($(S)/ $(S)/__ay_root_e2e__.go)\nEND()\n")
        self.write_tree("__ay_root_e2e__.go", "package answer\n")
        result = self.make("-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(result.stderr)
