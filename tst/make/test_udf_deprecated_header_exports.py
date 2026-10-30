from tst.make.lib import MakeTest


class UdfDeprecatedHeaderExportsTest(MakeTest):
    def test_udf_base_rejects_deprecated_exports_in_the_module_header(self):
        self.write("ya.make", "UDF_BASE(answer EXPORTS exports.txt)\nSRCS(answer.cpp)\nEND()\n")
        self.write("answer.cpp", 'extern "C" int answer() { return 42; }\n')
        self.write("exports.txt", "answer\n")
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("EXPORTS", result.stderr)
