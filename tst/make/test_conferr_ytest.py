from tst.make.lib import MakeTest


class YtestConfigureErrorsTest(MakeTest):
    """Configure errors of the test plugin (build/plugins/ytest.py, validate_test)."""

    def unittest(self, *statements):
        self.write("ya.make", "UNITTEST()\nSRCS(test.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")

    def test_test_data_is_removed(self):
        self.unittest("TEST_DATA(data.txt)")
        self.assertConfigureLikeYa()

    def test_invalid_requirement_syntax(self):
        self.unittest("REQUIREMENTS(e2e)")
        self.assertConfigureLikeYa()

    def test_system_tag_with_an_unknown_prefix(self):
        self.unittest("TAG(e2e:value)")
        self.assertConfigureLikeYa()

    def test_fat_tag_on_a_small_test(self):
        self.unittest("TAG(ya:fat)")
        self.assertConfigureLikeYa()

    def test_privileged_tag_on_a_small_test(self):
        self.unittest("TAG(ya:privileged)")
        self.assertConfigureLikeYa()

    def test_large_test_without_a_fat_tag(self):
        self.unittest("SIZE(LARGE)")
        self.assertConfigureLikeYa()

    def test_timeout_above_the_size_limit(self):
        self.unittest("TIMEOUT(120)")
        self.assertConfigureLikeYa()

    def test_negative_timeout(self):
        self.unittest("TIMEOUT(-1)")
        self.assertConfigureLikeYa()

    def test_split_factor_without_forking(self):
        self.unittest("SPLIT_FACTOR(2)")
        self.assertConfigureLikeYa()

    def test_split_factor_above_the_limit(self):
        self.unittest("FORK_TESTS()", "SPLIT_FACTOR(2000)")
        self.assertConfigureLikeYa()

    def test_noretries_tag_on_a_small_test(self):
        self.unittest("TAG(ya:noretries)")
        self.assertConfigureLikeYa()

    def test_exectest_without_run(self):
        self.write("ya.make", "EXECTEST()\nEND()\n")
        self.assertConfigureLikeYa()
