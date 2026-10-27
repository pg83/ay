from tst.make.lib import MakeTest, PROJECT


DESCRIPTIONS = "sdg/sdc/ros/monitorings/diag_updater/etc/Descriptions"


def args(node):
    return [arg.replace("$(SOURCE_ROOT)", "$(S)").replace("$(BUILD_ROOT)", "$(B)") for command in node["cmds"] for arg in command["cmd_args"]]


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

    def test_gen_pch_file_first_consumer_context(self):
        self.write("lib/ya.make", f"LIBRARY()\nADDINCL(GLOBAL ${{ARCADIA_BUILD_ROOT}}/{PROJECT} {PROJECT}/lib/private)\nCFLAGS(-DLIB_FLAG GLOBAL -DLIB_GLOBAL)\nGEN_PCH_FILE({PROJECT}/lib/b_impl.h ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/lib/b_impl.h.pch)\nSRCS(lib.cpp)\nEND()\n")
        self.write("lib/private/p.h", "#pragma once\n")
        self.write("lib/b_impl.h", "#pragma once\nint b();\n")
        self.write("lib/b.h", "#pragma once\n#include \"b_impl.h\"\n#if 0\n    #include \"lib/b_impl.h.pch\"\n#endif\n")
        self.write("lib/lib.cpp", "int b() { return 1; }\n")
        self.write("ga/ya.make", "LIBRARY()\nCFLAGS(GLOBAL -DGA_GLOBAL)\nSRCS(ga.cpp)\nEND()\n")
        self.write("ga/ga.cpp", "int ga() { return 1; }\n")
        for name, peers in (("la", f"{PROJECT}/lib {PROJECT}/ga"), ("lb", f"{PROJECT}/lib")):
            self.write(f"{name}/ya.make", f"LIBRARY()\nADDINCL({PROJECT}/{name}/inc)\nCFLAGS(-D{name.upper()}_FLAG)\nPEERDIR({peers})\nSRCS({name}.cpp)\nEND()\n")
            self.write(f"{name}/inc/x.h", "#pragma once\n")
            self.write(f"{name}/{name}.cpp", f"#include <{PROJECT}/lib/b.h>\nint {name}() {{ return b(); }}\n")
        self.write("prog/ya.make", f"PROGRAM()\nPEERDIR({PROJECT}/la {PROJECT}/lb)\nSRCS(main.cpp)\nEND()\n")
        self.write("prog/main.cpp", f"#include <{PROJECT}/lib/b.h>\nint main() {{ return b(); }}\n")
        self.write("ya.make", "RECURSE(prog)\n")
        graph = self.graph()
        pch = args(self.node(graph, "lib/b_impl.h.pch"))
        consumer = args(self.node(graph, "la/la.cpp.o"))
        self.assertEqual([arg for arg in pch if arg.startswith("-I")], [arg for arg in consumer if arg.startswith("-I")])
        self.assertEqual([arg for arg in pch if arg.endswith(("_FLAG", "_GLOBAL"))], ["-DLIB_FLAG", "-DLIB_GLOBAL", "-DGA_GLOBAL"])
