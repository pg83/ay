from tst.make.lib import MakeTest, PROJECT


class GoTestForValidationTest(MakeTest):
    def test_go_test_for_requires_a_source_directory(self):
        self.write("ya.make", "GO_TEST_FOR()\nEND()\n")
        self.assertNotEqual(self.make("-t").returncode, 0)

    def test_go_test_for_rejects_the_current_directory(self):
        self.write("ya.make", f"GO_TEST_FOR({PROJECT})\nEND()\n")
        self.assertNotEqual(self.make("-t").returncode, 0)
