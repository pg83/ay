from tst.make.lib import MakeTest


class StatementConfigureErrorsTest(MakeTest):
    """Configure errors of ya.make statements (devtools/ymake/makefile_loader.cpp, module_builder.cpp)."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def unittest(self, *statements):
        self.write("ya.make", "UNITTEST()\nSRCS(test.cpp)\n" + "".join(statement + "\n" for statement in statements) + "END()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")

    def test_include_once_with_two_arguments(self):
        self.library("INCLUDE_ONCE(yes no)")
        self.assertConfigureLikeYa()

    def test_include_once_with_a_non_boolean_value(self):
        self.library("INCLUDE_ONCE(e2e)")
        self.assertConfigureLikeYa()

    def test_include_once_altered(self):
        self.library("INCLUDE_ONCE(yes)", "INCLUDE_ONCE(no)")
        self.assertConfigureLikeYa()

    def test_fatal_no_build_if(self):
        self.write("ya.make", "SET(E2E_FLAG yes)\nNO_BUILD_IF(FATAL_ERROR E2E_FLAG)\nLIBRARY()\nEND()\n")
        self.assertConfigureLikeYa()

    def test_fatal_build_only_if(self):
        self.write("ya.make", "BUILD_ONLY_IF(FATAL_ERROR E2E_FLAG)\nLIBRARY()\nEND()\n")
        self.assertConfigureLikeYa()

    def test_only_tags_outside_of_a_multimodule(self):
        self.library("ONLY_TAGS(CPP_PROTO)")
        self.assertConfigureLikeYa()

    def test_unittest_for_without_arguments(self):
        self.write("ya.make", "UNITTEST_FOR()\nSRCS(test.cpp)\nEND()\n")
        self.write("test.cpp", "int answer() { return 42; }\n")
        self.assertConfigureLikeYa()

    def test_data_with_a_missing_external_file(self):
        self.unittest("DATA(ext:missing.bin)")
        self.assertConfigureLikeYa()

    def test_data_autoupdated_without_a_configuration(self):
        self.unittest(f"DATA(sbr://123456789 AUTOUPDATED arcadia/{self.project_name}/test.cpp)")
        self.assertConfigureLikeYa()

    def test_data_autoupdated_without_a_resource(self):
        self.unittest("DATA(AUTOUPDATED)")
        self.assertConfigureLikeYa()

    def test_data_without_a_prefix(self):
        self.unittest("DATA(test.cpp)")
        self.assertConfigureLikeYa()

    def test_check_srcdir_with_a_build_directory(self):
        self.library(f"CHECK_SRCDIR(${{ARCADIA_BUILD_ROOT}}/{self.project_name})")
        self.assertConfigureLikeYa()

    def test_peerdir_with_a_relative_path(self):
        self.library("PEERDIR(../e2e)")
        self.assertConfigureLikeYa()

    def test_host_resources_bundle_with_a_non_integral_strip_prefix(self):
        self.library("DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(E2E sbr:123456789 FOR LINUX STRIP_PREFIX e2e)")
        self.assertConfigureLikeYa()

    def test_host_resources_bundle_with_an_incomplete_triple(self):
        self.library("DECLARE_EXTERNAL_HOST_RESOURCES_BUNDLE(E2E sbr:123456789 FOR)")
        self.assertConfigureLikeYa()

    def test_induced_deps_of_a_missing_file(self):
        self.library("INDUCED_DEPS(h missing.h)")
        self.assertConfigureLikeYa()
