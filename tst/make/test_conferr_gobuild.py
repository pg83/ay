from tst.make.lib import MakeTest


class GobuildConfigureErrorsTest(MakeTest):
    """Configure errors of the Go build plugin (build/plugins/gobuild.py)."""

    def go_library(self, *statements):
        self.write("ya.make", "GO_LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("value.go", "package value\n")

    def test_cgo_export_before_a_go_file(self):
        self.go_library("SRCS(CGO_EXPORT value.go)")
        self.assertConfigureLikeYa()

    def test_cgo_export_at_the_end_of_srcs(self):
        self.go_library("SRCS(value.go CGO_EXPORT)")
        self.assertConfigureLikeYa()

    def test_go_test_file_in_srcs(self):
        self.go_library("SRCS(value.go value_test.go)")
        self.write("value_test.go", "package value\n")
        self.assertConfigureLikeYa()

    def test_non_test_file_in_go_test_srcs(self):
        self.write("ya.make", "GO_TEST()\nGO_TEST_SRCS(helper.go)\nEND()\n")
        self.write("helper.go", "package value\n")
        self.assertConfigureLikeYa()

    def test_cgo_srcs_with_cgo_disabled(self):
        self.go_library("DISABLE(CGO_ENABLED)", "SRCS(value.go)", "CGO_SRCS(native.go)")
        self.write("native.go", "package value\n")
        self.assertConfigureLikeYa()

    def test_non_go_file_in_cgo_srcs(self):
        self.go_library("SRCS(value.go)", "CGO_SRCS(native.c)")
        self.write("native.c", "int native(void) { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_disabling_gofmt(self):
        self.go_library("SET(_GO_FMT_ADD_CHECK no)", "SRCS(value.go)")
        self.assertConfigureLikeYa()

    def test_resource_without_a_key(self):
        self.go_library("SRCS(value.go)", "RESOURCE(data.txt)")
        self.write("data.txt", "data\n")
        self.assertConfigureLikeYa()

    def test_resource_with_an_empty_key(self):
        self.go_library("SRCS(value.go)", 'RESOURCE(data.txt "")')
        self.write("data.txt", "data\n")
        self.assertConfigureLikeYa()

    def test_inline_resource_without_a_value(self):
        self.go_library("SRCS(value.go)", "RESOURCE(- e2e_key)")
        self.assertConfigureLikeYa()
