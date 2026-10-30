import json

from tst.make.lib import MakeTest, PROJECT


class ToolsConfigurationTest(MakeTest):
    def test_tools_configuration_requires_its_json_manifest(self):
        self.write("ya.make", f"PY3_LIBRARY()\nYA_TOOLS_CONF({PROJECT}/missing)\nEND()\n")
        self.assertNotEqual(self.make().returncode, 0)

    def test_tool_definitions_toolchains_and_resource_aliases(self):
        self.write("ya.make", f"PY3_LIBRARY()\nYA_TOOLS_CONF({PROJECT}/conf/)\nEND()\n")
        formula = f"{PROJECT}/conf/formula.json"
        chain_formula = f"{PROJECT}/conf/chain.json"
        self.write("conf/formula.json", "{}\n")
        self.write("conf/chain.json", "{}\n")
        self.write("conf/ya.conf.json", json.dumps({
            "bottles": {"explicit": {"formula": formula}},
            "simple_tools": {"alias": {"resource": "ay_e2e_alias"}},
        }))
        definitions = {
            "tools/internal/tiers.json": {},
            "tools/tools/explicit.tool.json": {"tool": {"type": "simple", "definition": {"formula": formula}}},
            "tools/tools/ay_e2e_default.tool.json": {"tool": {"type": "simple", "definition": {}}},
            "tools/tools/nested/inline.tool.json": {"tool": {"type": "simple", "definition": {"formula": {}}}},
            "tools/tools/ignored.tool.json": {"tool": {"type": "bottle"}},
            "tools/toolchains/explicit.toolchain.json": {"bottles": {"explicit": {"formula": chain_formula}}},
            "tools/toolchains/nested/ignored.toolchain.json": {"bottles": {"ignored": {"formula": f"{PROJECT}/conf/missing.json"}}},
        }
        for name, value in definitions.items():
            self.write("conf/" + name, json.dumps(value))
        resources = [f"build/external_resources/{name}/resources.json" for name in ("ay_e2e_alias", "ay_e2e_default")]
        for name in resources:
            self.write_tree(name, "{}\n")
        inputs = {path for node in self.graph()["graph"] for path in node["inputs"]}
        nested = "tools/toolchains/nested/ignored.toolchain.json"
        expected = [f"{PROJECT}/conf/ya.conf.json", formula, chain_formula, *resources,
                    *(f"{PROJECT}/conf/{name}" for name in definitions if name != nested)]
        for name in expected:
            with self.subTest(input=name):
                self.assertIn("$(S)/" + name, inputs)
        self.assertNotIn(f"$(S)/{PROJECT}/conf/{nested}", inputs)
