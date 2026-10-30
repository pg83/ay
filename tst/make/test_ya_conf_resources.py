import json

from tst.make.lib import MakeTest, PROJECT


class YaConfResourcesTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", f"PY3_LIBRARY()\nYA_CONF_JSON({PROJECT}/ya.conf.json)\nPY_SRCS(answer.py)\nEND()\n")
        self.write("answer.py", "VALUE = 42\n")
        for name in ("formula.json", "formula.jsonx.json", "formula.j.json", "formula.js.json", "formula.jso.json", "formula.json.json"):
            self.write(name, "{}\n")
        self.write_tree("build/external_resources/ay_e2e_scanner/resources.json", "{}\n")

    def assert_macro_rejected(self, text):
        self.write("ya.conf.json", text)
        result = self.make()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("YA_CONF_JSON", result.stderr)

    def test_removed_macro_rejects_well_formed_resource_descriptions(self):
        formulas = {str(index): {"formula": f"{PROJECT}/{name}"} for index, name in enumerate((
            "formula.json", "formula.jsonx.json", "formula.j.json", "formula.js.json", "formula.jso.json", "formula.json.json",
        ))}
        descriptions = {"bottles": formulas, "simple_tools": {"tool": {"resource": "ay_e2e_scanner"}}}
        text = json.dumps(descriptions, indent=2)
        text = text.replace('"formula":', '"formula"\t \r\n:').replace('"resource":', '"resource"\r\n \t:')
        self.assert_macro_rejected(text)

    def test_removed_macro_rejects_malformed_resource_candidates(self):
        whitespace = "\t\n\f\r  "
        candidates = []
        for key in ("formula", "resource"):
            for suffix in ('!', whitespace + '!', whitespace + ':' + whitespace + 'false', whitespace + ':' + whitespace + '""'):
                candidates.append('"' + key + '"' + suffix)
        for name in ("a.", "a..", "a.j", "a.j.", "a.jx", "a.js", "a.js.", "a.jsx", "a.jso", "a.jso.", "a.jsox", "a.jsonx"):
            candidates.append('"formula"' + whitespace + ':' + whitespace + json.dumps(name))
        candidates.extend(('"formula"' + whitespace + ':' + whitespace + f'"{PROJECT}/formula.json"',
                           '"resource"' + whitespace + ':' + whitespace + '"ay_e2e_scanner"'))
        self.assert_macro_rejected("\n".join(candidates))

    def test_removed_macro_rejects_truncated_resource_descriptions(self):
        fragments = ('"formula"\t', '"formula"\f', '"formula" \t\f ', '"formula":\t\f ',
                     '"formula":"unfinished.', '"formula":"unfinished.j', '"formula":"unfinished.js',
                     '"formula":"unfinished.jso', '"formula":"unfinished.json',
                     '"resource"\t', '"resource"\f', '"resource" \t\f ', '"resource":\t\f ',
                     '"resource": "unfinished')
        for fragment in fragments:
            with self.subTest(fragment=fragment):
                self.assert_macro_rejected(fragment)

    def test_removed_macro_rejects_single_space_and_unterminated_values(self):
        descriptions = ('{"formula" : "' + PROJECT + '/formula.json", "resource" : "ay_e2e_scanner"}',
                        '"formula":"unfinished')
        for text in descriptions:
            with self.subTest(text=text):
                self.assert_macro_rejected(text)

    def test_removed_macro_rejects_compact_json_descriptions(self):
        self.assert_macro_rejected('{"resource":"ay_e2e_scanner"}')
