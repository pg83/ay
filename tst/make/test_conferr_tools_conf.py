import json

from tst.make.lib import MakeTest


class ToolsConfConfigureErrorsTest(MakeTest):
    """Configure errors of YA_TOOLS_CONF (build/plugins/res.py) and of Cython includes (build/plugins/pybuild.py)."""

    def library(self, *statements):
        self.write("ya.make", "LIBRARY()\n" + "".join(statement + "\n" for statement in statements) + "END()\n")

    def tools_conf(self, conf, **files):
        self.library("YA_TOOLS_CONF(" + self.project_name + "/conf)")
        self.write("conf/ya.conf.json", json.dumps(conf))
        for path, content in files.items():
            self.write("conf/" + path.replace("__tool__json", ".tool.json").replace("__toolchain__json", ".toolchain.json").replace("__", "/"), json.dumps(content))

    def test_tools_conf_from_a_missing_directory(self):
        self.library("YA_TOOLS_CONF(" + self.project_name + "/missing)")
        self.assertConfigureLikeYa()

    def test_tools_conf_without_ya_conf_json(self):
        self.library("YA_TOOLS_CONF(" + self.project_name + "/conf)")
        self.write("conf/readme.txt", "conf\n")
        self.assertConfigureLikeYa()

    def test_tools_conf_bottle_formula_outside_of_the_tree(self):
        self.tools_conf({"bottles": {"e2e": {"formula": "contrib/e2e/resources.json"}}})
        self.assertConfigureLikeYa()

    def test_tools_conf_bottle_formula_that_is_missing(self):
        self.tools_conf({"bottles": {"e2e": {"formula": "build/external_resources/e2e_missing/resources.json"}}})
        self.assertConfigureLikeYa()

    def test_tools_conf_simple_tool_without_a_formula(self):
        self.tools_conf({"simple_tools": {"e2e_missing": {}}, "bottles": {}})
        self.assertConfigureLikeYa()

    def test_tools_conf_tool_config_formula_that_is_missing(self):
        self.tools_conf({"bottles": {}}, tools__tools__e2e__tool__json={"tool": {"type": "simple", "definition": {"formula": "build/external_resources/e2e_missing/resources.json"}}})
        self.assertConfigureLikeYa()

    def test_tools_conf_toolchain_bottle_outside_of_the_tree(self):
        self.tools_conf({"bottles": {}}, tools__toolchains__e2e__toolchain__json={"bottles": {"e2e": {"formula": "contrib/e2e/resources.json"}}})
        self.assertConfigureLikeYa()

    def test_pyx_including_a_missing_file(self):
        self.write("ya.make", "PY3_LIBRARY()\nPY_SRCS(value.pyx)\nEND()\n")
        self.write("value.pyx", "include \"missing.pxi\"\n")
        self.assertConfigureLikeYa()
