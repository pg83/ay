from tst.make.lib import MakeTest

DESCRIPTIONS = "${ARCADIA_ROOT}/sdg/sdc/ros/monitorings/diag_updater/etc/Descriptions"


class SdcConfigureErrorsTest(MakeTest):
    """Configure errors of the SDC plugins (build/internal/plugins/sdc.py, sdc_proto.py, sdc_diagnostics.py)."""

    def module(self, module, *statements):
        self.write("ya.make", f"{module}()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def test_sdc_install_with_an_unknown_kind(self):
        self.module("UNION", "SDC_INSTALL(E2E value.txt)")
        self.write("value.txt", "value\n")
        self.assertConfigureLikeYa()

    def test_sdc_install_target_without_a_path(self):
        self.module("UNION", "SDC_INSTALL(LIB TARGET)")
        self.assertConfigureLikeYa()

    def test_sdc_install_target_without_a_name_after_as(self):
        self.module("UNION", "SDC_INSTALL(LIB TARGET e2e/target AS)")
        self.assertConfigureLikeYa()

    def test_sdc_install_node_without_a_target(self):
        self.module("UNION", "SDC_INSTALL(LIB NODE node)")
        self.assertConfigureLikeYa()

    def test_sdc_install_program_starter_without_a_name(self):
        self.module("UNION", "SDC_INSTALL(COMMON_BIN PROGRAM_STARTER source)")
        self.assertConfigureLikeYa()

    def test_empty_proto_to_namespace(self):
        self.module("PY3_LIBRARY", "PROTO_TO_NAMESPACE()")
        self.assertConfigureLikeYa()

    def test_proto_to_namespace_with_an_unknown_prefix(self):
        self.module("PY3_LIBRARY", "PROTO_TO_NAMESPACE(IN_E2E)")
        self.assertConfigureLikeYa()

    def test_proto_to_namespace_with_two_prefixes(self):
        self.module("PY3_LIBRARY", "PROTO_TO_NAMESPACE(IN_MSG IN_PROTO)")
        self.assertConfigureLikeYa()

    def test_proto_to_namespace_prefix_without_a_module(self):
        self.module("PY3_LIBRARY", "PROTO_TO_NAMESPACE(IN_MSG)")
        self.assertConfigureLikeYa()

    def test_diagnostics_v4_without_arguments(self):
        self.module("LIBRARY", "SDC_DIAGS_SPLIT_GENERATOR_V4()")
        self.assertConfigureLikeYa()

    def test_diagnostics_v4_with_an_unsupported_mode(self):
        self.module("LIBRARY", "SET(_DIAG_GEN_MODE e2e)", f"SDC_DIAGS_SPLIT_GENERATOR_V4({DESCRIPTIONS}/E2E/value.yaml)")
        self.assertConfigureLikeYa()

    def test_diagnostics_v3_without_arguments(self):
        self.module("LIBRARY", "SDC_DIAGS_SPLIT_GENERATOR_V3()")
        self.assertConfigureLikeYa()

    def test_diagnostics_v3_with_several_paths(self):
        self.module("LIBRARY", f"SDC_DIAGS_SPLIT_GENERATOR_V3({DESCRIPTIONS}/E2E/first.yaml {DESCRIPTIONS}/E2E/second.yaml)")
        self.assertConfigureLikeYa()

    def test_diagnostics_path_outside_of_the_descriptions(self):
        self.module("LIBRARY", "SDC_DIAGS_SPLIT_GENERATOR_V3(value.yaml)")
        self.write("value.yaml", "value: 42\n")
        self.assertConfigureLikeYa()
