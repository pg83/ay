from tst.make.lib import MakeTest


class BuildPluginConfigureErrorsTest(MakeTest):
    """Configure errors of small build/plugins and plugin error messages: cmake_export, check_clang_warnings,
    ios_assets, yql_python_udf, linker_script, credits and ai_artifacts."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def test_cmake_target_name_from_path_without_a_root(self):
        self.library("CMAKE_EXPORTED_TARGET_NAME_FROM_PATH()")
        self.assertConfigureLikeYa()

    def test_cmake_target_name_from_path_outside_of_the_root(self):
        self.library("CMAKE_EXPORTED_TARGET_NAME_FROM_PATH(e2e)")
        self.assertConfigureLikeYa()

    def test_clang_warnings_disabling_a_warning(self):
        self.library("CLANG_WARNINGS(-Wno-e2e)")
        self.assertConfigureLikeYa()

    def test_clang_warnings_with_a_non_warning_flag(self):
        self.library("CLANG_WARNINGS(-O2)")
        self.assertConfigureLikeYa()

    def test_ios_assets_without_a_root(self):
        self.library("IOS_ASSETS(CONTENTS value.png)")
        self.write("value.png", "png\n")
        self.assertConfigureLikeYa()

    def test_ios_assets_outside_of_the_root(self):
        self.library("IOS_ASSETS(ROOT assets CONTENTS value.png)")
        self.write("value.png", "png\n")
        self.assertConfigureLikeYa()

    def test_python_udf_with_an_invalid_name(self):
        self.library("REGISTER_YQL_PYTHON_UDF(NAME E2E)")
        self.assertConfigureLikeYa()

    def test_python_udf_with_libra_modules_in_opensource(self):
        self.library("SET(OPENSOURCE yes)", "REGISTER_YQL_PYTHON_UDF(NAME CustomPython3 ADD_LIBRA_MODULES yes)")
        self.assertConfigureLikeYa()

    def test_linker_script_with_an_invalid_extension(self):
        self.library("LINKER_SCRIPT(value.txt)")
        self.write("value.txt", "value\n")
        self.assertConfigureLikeYa()

    def test_contrib_credits_without_license_texts(self):
        self.library(f"CHECK_CONTRIB_CREDITS({self.project_name})")
        self.assertConfigureLikeYa()

    def test_ai_reference_to_a_missing_path(self):
        self.library(f"AI_REFERENCE({self.project_name}/missing)")
        self.assertConfigureLikeYa()
