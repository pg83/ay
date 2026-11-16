from tst.make.lib import MakeTest, PROJECT


class ConfFileConfigureErrorsTest(MakeTest):
    """Configure errors of build configuration files: sysincl lists (devtools/ymake/sysincl_conf.cpp), license
    properties (devtools/ymake/licenses_conf.cpp) and autoinclude lists (devtools/ymake/autoincludes_conf.cpp)."""

    def library(self):
        self.write("ya.make", "LIBRARY()\nSRCS(value.cpp)\nEND()\n")
        self.write("value.cpp", "int value() { return 42; }\n")

    def sysincl(self, text):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\nSYSINCL+={PROJECT}/sysincl.yml\n")
        self.write("sysincl.yml", text)
        self.library()

    def licenses(self, text):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\nLICENSES+={PROJECT}/license.json\n")
        self.write("license.json", text)
        self.library()

    def autoincludes(self, text):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\nAUTOINCLUDE_PATHS+={PROJECT}/autoincludes.json\n")
        self.write("autoincludes.json", text)
        self.library()

    def missing_conf_file(self, variable):
        core = self.root / "build/ymake.core.conf"
        self.write_tree("build/ymake.core.conf", core.read_text() + f"\n{variable}+={PROJECT}/missing.conf\n")
        self.library()

    def test_autoincludes_that_are_missing(self):
        self.missing_conf_file("AUTOINCLUDE_PATHS")
        self.assertConfigureLikeYa()

    def test_licenses_that_are_missing(self):
        self.missing_conf_file("LICENSES")
        self.assertConfigureLikeYa()

    def test_sysincl_that_is_missing(self):
        self.missing_conf_file("SYSINCL")
        self.assertConfigureLikeYa()

    def test_autoincludes_root_that_is_not_an_array(self):
        self.autoincludes('{"e2e": []}\n')
        self.assertConfigureLikeYa()

    def test_autoincludes_json_syntax_error(self):
        self.autoincludes('["e2e"\n')
        self.assertConfigureLikeYa()

    def test_sysincl_list_that_is_not_a_sequence(self):
        self.sysincl("e2e\n")
        self.assertConfigureLikeYa()

    def test_sysincl_section_that_is_not_a_section(self):
        self.sysincl("- includes:\n  - e2e_one.h\n- e2e\n")
        self.assertConfigureLikeYa()

    def test_sysincl_section_with_an_unsupported_key(self):
        self.sysincl("includes:\n- e2e_one.h\ne2e: yes\n")
        self.assertConfigureLikeYa()

    def test_sysincl_source_filter_that_is_not_a_scalar(self):
        self.sysincl("source_filter:\n- e2e\nincludes:\n- e2e_one.h\n")
        self.assertConfigureLikeYa()

    def test_sysincl_item_that_is_a_sequence(self):
        self.sysincl("- - e2e_one.h\n")
        self.assertConfigureLikeYa()

    def test_sysincl_target_that_is_not_a_scalar(self):
        self.sysincl("- e2e_one.h:\n  - - e2e_two.h\n")
        self.assertConfigureLikeYa()

    def test_sysincl_case_sensitive_that_is_not_a_boolean(self):
        self.sysincl("case_sensitive: e2e\nincludes:\n- e2e_one.h\n")
        self.assertConfigureLikeYa()

    def test_sysincl_yaml_syntax_error(self):
        self.sysincl("- e2e_one.h: [\n")
        self.assertConfigureLikeYa()

    def test_license_root_that_is_not_a_map(self):
        self.licenses("[]\n")
        self.assertConfigureLikeYa()

    def test_license_property_that_is_not_a_map(self):
        self.licenses('{"E2E": []}\n')
        self.assertConfigureLikeYa()

    def test_license_list_that_is_not_an_array(self):
        self.licenses('{"E2E": {"default": "MIT"}}\n')
        self.assertConfigureLikeYa()

    def test_license_name_that_is_not_a_string(self):
        self.licenses('{"E2E": {"default": [1]}}\n')
        self.assertConfigureLikeYa()

    def test_license_linking_type_that_is_unknown(self):
        self.licenses('{"E2E": {"e2e": ["MIT"]}}\n')
        self.assertConfigureLikeYa()

    def test_license_restricted_license_that_is_not_a_map(self):
        self.licenses('{"restricted": {"MIT": []}}\n')
        self.assertConfigureLikeYa()

    def test_license_restricted_properties_that_are_not_an_array(self):
        self.licenses('{"restricted": {"MIT": {"static": "E2E"}}}\n')
        self.assertConfigureLikeYa()

    def test_license_restricted_property_that_is_not_a_string(self):
        self.licenses('{"restricted": {"MIT": {"dynamic": [1]}}}\n')
        self.assertConfigureLikeYa()

    def test_license_restricted_linking_type_that_is_unknown(self):
        self.licenses('{"restricted": {"MIT": {"e2e": ["E2E"]}}}\n')
        self.assertConfigureLikeYa()

    def test_license_json_syntax_error(self):
        self.licenses('{"E2E": \n')
        self.assertConfigureLikeYa()
