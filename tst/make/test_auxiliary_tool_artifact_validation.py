from tst.make.lib import MakeTest


class AuxiliaryToolArtifactValidationTest(MakeTest):
    def test_resource_only_modules_cannot_be_used_as_auxiliary_tools(self):
        self.write("ya.make", f"LIBRARY()\nRUN_PYTHON3(generate.py TOOL {self.project_name}/tool STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("tool/ya.make", "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(E2E_TOOL sbr:123456789)\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(f"{self.project_name}/tool", result.stderr)
