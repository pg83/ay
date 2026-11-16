from tst.make.lib import MakeTest, PROJECT


class ModuleBuilderConfigureErrorsTest(MakeTest):
    """Configure errors of ymake's module builder (devtools/ymake/module_builder.cpp)."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def test_peerdir_to_its_own_directory(self):
        self.library(f"PEERDIR({PROJECT})")
        self.assertConfigureLikeYa()

    def test_addincl_to_a_missing_root_directory(self):
        self.library("ADDINCL(./include)")
        self.assertConfigureLikeYa()

    def test_relative_srcdir(self):
        self.library("SRCDIR(../sources)")
        self.assertConfigureLikeYa()

    def test_addincl_with_two_languages(self):
        self.library(f"ADDINCL(FOR proto FOR cython {PROJECT}/include)")
        self.assertConfigureLikeYa()

    def test_addincl_with_an_unknown_language(self):
        self.library(f"ADDINCL(FOR e2e_unknown {PROJECT}/include)")
        self.assertConfigureLikeYa()

    def test_addincl_tail_arguments(self):
        self.library(f"ADDINCL({PROJECT}/include GLOBAL)")
        self.assertConfigureLikeYa()

    def test_declare_external_resource_without_arguments(self):
        self.library("DECLARE_EXTERNAL_RESOURCE()")
        self.assertConfigureLikeYa()

    def test_declare_external_resource_without_a_uri(self):
        self.library("DECLARE_EXTERNAL_RESOURCE(E2E_RESOURCE)")
        self.assertConfigureLikeYa()

    def test_declare_external_resource_with_a_non_integral_strip_prefix(self):
        self.library("DECLARE_EXTERNAL_RESOURCE(E2E_RESOURCE sbr:123456789 STRIP_PREFIX many)")
        self.assertConfigureLikeYa()

    def test_declare_external_resource_without_a_strip_prefix_value(self):
        self.library("DECLARE_EXTERNAL_RESOURCE(E2E_RESOURCE sbr:123456789 STRIP_PREFIX)")
        self.assertConfigureLikeYa()

    def test_external_host_resources_pack_without_a_resource_name(self):
        self.library("DECLARE_EXTERNAL_HOST_RESOURCES_PACK(sbr:123456789 FOR linux)")
        self.assertConfigureLikeYa()

    def test_recurse_inside_a_module(self):
        self.library("RECURSE(child)")
        self.write("child/ya.make", "LIBRARY()\nEND()\n")
        self.assertConfigureLikeYa()

    def test_build_path_in_data_files(self):
        self.library(f"DATA_FILES(${{ARCADIA_BUILD_ROOT}}/{PROJECT}/data.txt)")
        self.assertConfigureLikeYa()
