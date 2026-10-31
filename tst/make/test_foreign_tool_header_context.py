from tst.make.lib import MakeTest, PROJECT


class ForeignToolHeaderContextTest(MakeTest):
    def test_missing_target_header_dependency_is_rejected_during_cross_platform_generation(self):
        self.write("ya.make", f"LIBRARY(answer)\nRUN_PROGRAM({PROJECT}/tool STDOUT generated.cpp)\nEND()\n")
        self.write("tool/ya.make", f"PROGRAM(generator)\nIF(ARCH_AARCH64)\nRUN_PROGRAM({PROJECT}/header_tool STDOUT generated.h OUTPUT_INCLUDES ${{ARCADIA_BUILD_ROOT}}/{PROJECT}/missing.h)\nELSE()\nRUN_PROGRAM({PROJECT}/header_tool STDOUT generated.h)\nENDIF()\nSRCS(main.cpp public.h)\nEND()\n")
        self.write("tool/main.cpp", f'#include <{PROJECT}/tool/generated.h>\n' '#include <cstdio>\nint main() { std::puts("int answer() { return 42; }"); }\n')
        self.write("tool/public.h", f'#include <{PROJECT}/tool/generated.h>\n')
        self.write("header_tool/ya.make", "PROGRAM(header_writer)\nSRCS(main.cpp)\nEND()\n")
        self.write("header_tool/main.cpp", '#include <cstdio>\nint main() { std::puts("#pragma once"); }\n')
        result = self.make("--target-platform=default-linux-aarch64", "--host-platform=default-linux-x86_64")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("missing.h", result.stderr)
