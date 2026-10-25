from tst.make.lib import MakeTest, PROJECT


DESCRIPTIONS = "sdg/sdc/ros/monitorings/diag_updater/etc/Descriptions"


class SdcGeneratorsTest(MakeTest):
    def test_diagnostics_v4(self):
        for tag in ("CPP_DIAG", "GO_DIAG"):
            with self.subTest(tag=tag):
                self.write("ya.make", f"SDC_DIAGS_LIBRARY()\nONLY_TAGS({tag})\nSDC_DIAGS_SPLIT_GENERATOR_V4({DESCRIPTIONS}/Diagnostics/LivenessCheck.yaml)\nEND()\n")
                graph = self.graph()
                self.assertTrue(any(path.endswith(("LivenessCheck.cpp", "LivenessCheck.go")) for node in graph["graph"] for path in node["outputs"]))

    def test_diagnostics_v3_restricted_peer(self):
        output = "${ARCADIA_BUILD_ROOT}/sdg/sdc/ros/monitorings/diag_updater/gen_diags/diagnostics/v3/Diagnostics/LivenessCheck.h"
        self.write("ya.make", f"LIBRARY()\nADDINCL(sdg/sdc/ros/monitorings/diag_updater/include)\nPEERDIR(contrib/restricted/boost/hana sdg/sdc/ros/misc/protocol/msg/diagnostic)\nSDC_DIAGS_SPLIT_GENERATOR_V3({DESCRIPTIONS}/Diagnostics)\nSDC_DIAGS_SPLIT_GENERATOR_V3({DESCRIPTIONS}/Diagnostics/LivenessCheck.yaml)\nRESOURCE({output} /diagnostic)\nEND()\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("peerdir policy", result.stderr)
        self.assertIn("contrib/restricted/boost/hana", result.stderr)

    def test_tasklet_registration(self):
        for macro, args in (("TASKLET_REG", "answer cpp e2e::Answer"), ("TASKLET_REG_EXT", "answer cpp e2e::Answer e2e::Wrapper")):
            with self.subTest(macro=macro):
                self.write("ya.make", f"LIBRARY()\n{macro}({args} {PROJECT}/answer.h)\nEND()\n")
                self.write("answer.h", "#pragma once\nnamespace e2e { class Answer {}; class Wrapper {}; }\n")
                self.node(self.graph(), "answer.task.cpp")
