from tst.make.lib import MakeTest


class PluginConfigureErrorsTest(MakeTest):
    """Configure errors of build/plugins: macros_with_error.py, large_files.py, res.py and _common.py."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def test_restricted_path_with_a_message(self):
        self.library("RESTRICT_PATH(contrib/ devtools/ MSG e2e libraries live in contrib)")
        self.assertConfigureLikeYa()

    def test_restricted_path_without_a_message(self):
        self.library("RESTRICT_PATH(contrib/)")
        self.assertConfigureLikeYa()

    def test_failed_assertion(self):
        self.library("SET(E2E_ASSERTED no)", "ASSERT(E2E_ASSERTED e2e assertion failed)")
        self.assertConfigureLikeYa()

    def test_recursive_mask_in_declare_in_dirs(self):
        self.library("DECLARE_IN_DIRS(E2E **/*.txt DIRS data)")
        self.write("data/value.txt", "value\n")
        self.assertConfigureLikeYa()

    def test_mask_in_declare_in_dirs_srcdir(self):
        self.library("DECLARE_IN_DIRS(E2E *.txt SRCDIR data* DIRS value)")
        self.write("data/value/value.txt", "value\n")
        self.assertConfigureLikeYa()

    def test_parent_directory_in_declare_in_dirs(self):
        self.library("DECLARE_IN_DIRS(E2E *.txt DIRS ../data)")
        self.assertConfigureLikeYa()

    def test_large_file_without_data_or_placeholder(self):
        self.library("LARGE_FILES(missing.bin)")
        self.assertConfigureLikeYa()

    def test_large_file_placeholder_without_a_storage(self):
        self.library("LARGE_FILES(value.bin)")
        self.write("value.bin.external", '{"resource_id": 123456789}\n')
        self.assertConfigureLikeYa()

    def test_large_file_placeholder_with_an_unknown_storage(self):
        self.library("LARGE_FILES(value.bin)")
        self.write("value.bin.external", '{"storage": "E2E", "resource_id": 123456789}\n')
        self.assertConfigureLikeYa()

    def test_wildcard_in_all_resource_files(self):
        self.library("ALL_RESOURCE_FILES(txt *)")
        self.assertConfigureLikeYa()

    def test_unsupported_no_lint_value(self):
        self.write("ya.make", "PY3_LIBRARY()\nSET(_NO_LINT_VALUE e2e)\nPY_SRCS(value.py)\nEND()\n")
        self.write("value.py", "VALUE = 42\n")
        self.assertConfigureLikeYa()
