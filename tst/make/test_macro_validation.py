"""Invalid build declarations must fail through the public make command."""
import unittest

from tst import lib
from tst.make.test_mlaunch import files as mlaunch_files
from tst.make.test_ros import ros_files


class MacroValidationTest(unittest.TestCase):
    def test_invalid_ros_sources(self):
        for body, message in (
            ("ROS_DIR()", "expects one directory"),
            ("ROS_PARTIAL()", "expects one name"),
            ("ROS_SRCS(PARTIAL)", "Expected library name after PARTIAL"),
            ("ROS_SRCS(invalid.txt)", "unsupported ROS file"),
            ("ROS_SRCS(ZERO_COPY Query.srv)", "ZERO_COPY for .srv file is not allowed"),
        ):
            with self.subTest(body=body):
                files = ros_files()
                files["app/ya.make"] = f"ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ONLY_TAGS(CPP_ROS)\n{body}\nEND()\n"
                result = lib.make_process(files, "app")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_invalid_macro_arguments(self):
        cases = (
            ("GEN_PCH_FILE()", "expects an input header"),
            ("DEB_VERSION()", "expects one changelog"),
            ("YA_TOOLS_CONF()", "expects one directory"),
            ("LLVM_COMPILE_CXX()", "requires input, output"),
            ("COMPILE_LUA()", "expects a source"),
            ("BUNDLE_OUTPUT()", "expects a target"),
            ("TS_PROTO_PACKAGE_NAME()", "expects one package name"),
            ("SET_CUSTOM_CLANG_TIDY()", "expects a resource module"),
            ("YQL_CURRENT_ABI_VERSION(extra)", "expects no arguments"),
            ("FBS_NAMESPACE()", "expects a namespace"),
            ("PROTO2FBS()", "expects one proto input"),
            ("LINKER_SCRIPT(link.ld)", "outside a PROGRAM"),
            ("TEST_SRCS(NAMESPACE)", "is not allowed"),
            ("SET_LIST_PRE_SUF()", "expects an output"),
            ("GENERATE_IMPLIB()", "requires a library name and path"),
            ("GENERATE_IMPLIB(lib path SONAME)", "requires a value"),
            ("GENERATE_IMPLIB(lib path unknown)", "unexpected argument"),
            ("NODE_BUILDER()", "keyword/value pairs"),
            ("NODE_BUILDER(builder unknown x)", "Unknown NODE_BUILDER"),
            ("NODE_BUILDER(builder NAME x NAME y)", "Repeated NODE_BUILDER"),
            ("NODE_BUILDER(builder)", "requires NAME"),
            ("GOLAUNCH_FUNCTION()", "expects one function"),
            ("GOLAUNCH_OVERRIDE()", "expects a target and function"),
            ("GENERATE_GOLAUNCH_CALLBACKS()", "expects one name"),
            ("STYLE_PYTHON(CONFIG_TYPE)", "requires a config type"),
            ("STYLE_PYTHON(CHECK_FORMAT)", "requires ruff"),
            ("STYLE_PYTHON(RUN_IN_SOURCE_ROOT)", "requires ruff"),
            ("SET(YAML_LINT_SRCS a.yaml)\nSTYLE_YAML_WITH_CONFIG()", "over YAML_LINT_SRCS"),
            ("REGISTER_YQL_PYTHON_UDF(NAME)", "requires a value"),
            ("REGISTER_YQL_PYTHON_UDF(unknown value)", "unknown option"),
            ("REGISTER_YQL_PYTHON_UDF(ADD_LIBRA_MODULES yes)", "not supported in opensource"),
            ("SDC_RESOURCE_INFO(category name)", "category, name and URL"),
            ("SDC_SB_RESOURCE_INFO(42)", "id and name"),
            ("SDC_ROSNODE_INFO()", "one node path"),
            ("SDC_SOLIB_INFO()", "one URL"),
            ("SDC_INSTALL(file)", "expected an installation kind"),
            ("SDC_INSTALL(COMMON_BIN TARGET)", "missing argument"),
            ("SDC_DIAGS_SPLIT_GENERATOR_V4()", "diagnostics file path is required"),
            ("SDC_DIAGS_SPLIT_GENERATOR_V4(outside.yaml)", "must be a .yaml inside"),
            ("SDC_DIAGS_SPLIT_GENERATOR_V3()", "exactly one diagnostics file"),
            ("SDC_DIAGS_SPLIT_GENERATOR_V3(outside.yaml)", "must be inside"),
            ("USRV_BUILD(FROM)", "missing FROM"),
            ("USRV_BUILD(a.cpp)", "FROM is required"),
            ("USRV_ALL_SRCS(SUFFIX)", "missing SUFFIX"),
            ("USRV_GEN_PROTO_STRUCTS(OPTS_FILE)", "requires a value"),
            ("USRV_GEN_PROTO_STRUCTS(PROTODESC one PROTODESC_NAME two)", "mutually exclusive"),
            ("USRV_GEN_PROTO_STRUCTS(OUTPUT_INCLUDES file.h)", "requires FILE"),
            ("USRV_GEN_PROTO_STRUCTS(unknown)", "unknown argument"),
            ("USRV_GEN_PROTO_STRUCTS()", "PROTODESC or PROTODESC_NAME is required"),
        )
        for body, message in cases:
            with self.subTest(body=body):
                files = {"app/ya.make": f"LIBRARY()\n{lib.BARE_MODULE}{body}\nEND()\n"}
                if body.startswith("NODE_BUILDER"):
                    files = mlaunch_files()
                    files["comp/ya.make"] = f"MLAUNCH_COMPONENT()\n{lib.BARE_MODULE}{body}\nEND()\n"
                    files["app/ya.make"] = f"GOLAUNCH_PROGRAM()\n{lib.BARE_MODULE}PEERDIR(comp)\nEND()\n"
                result = lib.make_process(files, "app")
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertIn(message, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_program_rejects_non_linker_script(self):
        result = lib.make_process({"app/ya.make": f"PROGRAM()\n{lib.BARE_MODULE}LINKER_SCRIPT(invalid.txt)\nEND()\n"}, "app")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("only .ld scripts", result.stderr)

    def test_invalid_docker_images(self):
        for image, message in (("docker://image", "separated by '='"),
                               ("tag=docker://image", "invalid Docker digest")):
            with self.subTest(image=image):
                result = lib.make_process({"app/ya.make": f"EXECTEST()\nRUN(check)\nDOCKER_IMAGE({image})\nEND()\n"}, "app", "-t")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
