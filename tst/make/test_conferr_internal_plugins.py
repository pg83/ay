from tst.make.lib import MakeTest

OFFERS = "api/proto/yandex/market/b2b/offers_catalog"


class InternalPluginConfigureErrorsTest(MakeTest):
    """Configure errors of build/internal/plugins: ros, offer_view_converters, uservices, sandbox_registry,
    fintech_risk_model, codegen35 and userver."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def test_ros_srcs_partial_without_a_library(self):
        self.library("ROS_SRCS(PARTIAL)")
        self.assertConfigureLikeYa()

    def test_ros_srcs_zero_copy_service(self):
        self.library("ROS_SRCS(ZERO_COPY value.srv)")
        self.write("value.srv", "int32 value\n---\n")
        self.assertConfigureLikeYa()

    def test_ros_srcs_unsupported_file(self):
        self.library("ROS_SRCS(value.e2e)")
        self.write("value.e2e", "value\n")
        self.assertConfigureLikeYa()

    def test_offer_converters_without_triples(self):
        self.library("MARKET_OFFER_CONVERTERS()")
        self.assertConfigureLikeYa()

    def test_offer_converters_with_a_foreign_header(self):
        self.library("MARKET_OFFER_CONVERTERS(--view-config e2e/value.pb.h Value)")
        self.assertConfigureLikeYa()

    def test_offer_converters_with_a_package_name_collision(self):
        self.library(f"MARKET_OFFER_CONVERTERS(--view-config {OFFERS}/a_b/first.pb.h First --view-config {OFFERS}/a/b/second.pb.h Second)")
        self.assertConfigureLikeYa()

    def test_usrv_files_with_malformed_dependencies(self):
        self.library("USRV_BUILD(FROM value.tar DEPS_FILE deps.json value.cpp)")
        self.write("deps.json", "not json\n")
        self.write("value.tar", "value\n")
        self.assertConfigureLikeYa()

    def test_usrv_files_without_dependencies_for_a_file(self):
        self.library("USRV_BUILD(FROM value.tar DEPS_FILE deps.json value.cpp)")
        self.write("deps.json", "{}\n")
        self.write("value.tar", "value\n")
        self.assertConfigureLikeYa()

    def test_sandbox_import_of_a_missing_path(self):
        self.library("REGISTER_SANDBOX_IMPORT(missing)")
        self.assertConfigureLikeYa()

    def test_sandbox_import_of_an_absolute_path(self):
        self.library(f"REGISTER_SANDBOX_IMPORT(/{self.project_name}/value.py)")
        self.write("value.py", "VALUE = 42\n")
        self.assertConfigureLikeYa()

    def test_risk_data_model_with_an_unknown_argument(self):
        self.library("RISK_GEN_DATA_MODEL(E2E)")
        self.assertConfigureLikeYa()

    def test_grpc_linter_data_outside_of_the_arcadia_root(self):
        self.library("C35_GRPC_LINTER_TESTS_DATA(e2e e2e)")
        self.assertConfigureLikeYa()

    def test_usrv_proto_structs_with_an_unknown_argument(self):
        self.library("USRV_GEN_PROTO_STRUCTS(E2E)")
        self.assertConfigureLikeYa()
