from tst.make.lib import MakeTest, PROJECT


class ResourceRecollectionPeersTest(MakeTest):
    def test_resource_binding_keeps_the_original_conditional_module_and_its_peers(self):
        self.write("ya.make", f"LIBRARY(answer)\nPEERDIR({PROJECT}/resources)\nRUN_PYTHON3(generate.py ${{E2E_SELF_RESOURCE_GLOBAL}} ${{E2E_LEAF_RESOURCE_GLOBAL}} STDOUT generated.cpp)\nEND()\n")
        self.write("generate.py", 'print("int answer() { return 42; }")\n')
        self.write("resources/ya.make", f"IF(NOT E2E_SELF_RESOURCE_GLOBAL)\nRESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(E2E_SELF sbr:123456789)\nPEERDIR({PROJECT}/leaf)\nEND()\nELSE()\nPEERDIR({PROJECT}/leaf)\nENDIF()\n")
        self.write("leaf/ya.make", "RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE(E2E_LEAF sbr:123456790)\nEND()\n")
        generated = self.node(self.graph(), "generated.cpp")
        args = [arg for command in generated["cmds"] for arg in command["cmd_args"]]
        for name, resource in (("E2E_SELF", 123456789), ("E2E_LEAF", 123456790)):
            self.assertIn(f"$({name}-sbr:{resource})", args)
