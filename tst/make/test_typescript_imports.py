import json

from tst.make.lib import MakeTest, PROJECT


class TypescriptImportsTest(MakeTest):
    def setUp(self):
        super().setUp()
        self.write("ya.make", "TS_PACKAGE()\nEND()\nRECURSE_FOR_TESTS(tests)\n")
        self.write("package.json", json.dumps({"name": "@e2e/imports", "version": "1.0.0", "engines": {"node": "22.13.0"}}))
        self.write("pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters:\n  .: {}\n")
        self.write("tsconfig.json", '{"compilerOptions":{"target":"ES2022"},"include":["src/**/*.ts"]}\n')
        self.write("vitest.config.ts", "export default {};\n")
        self.write("tests/ya.make", f"TS_TEST_VITEST_FOR({PROJECT})\nTS_TEST_SRCS(../src)\nTS_TEST_CONFIG(../vitest.config.ts)\nEND()\n")

    def test_missing_relative_modules_are_configuration_errors(self):
        self.write("src/missing.spec.ts", 'import "./absent";\nimport "./absent.js";\n')
        result = self.make("-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("absent", result.stderr)

    def test_keyword_prefix_identifier_continuations_keep_dependencies(self):
        clauses = (
            "import {name} from {target};",
            "import {name}, * as {name} from {target};",
            "import {{ {name} as {name}, }} from {target};",
            "import type {{ {name} as {name} }} from {target};",
            "export type {{ {name} as {name} }} from {target};",
        )
        dependencies = []
        for prefix in ("a", "as", "f", "fr", "fro", "from", "t", "ty", "typ", "type"):
            for suffix in ("$", "0", "A", "_", "x", "z"):
                for clause in clauses:
                    dependency = f"continuation{len(dependencies)}"
                    self.write(f"src/{dependency}.spec.ts", clause.format(name=prefix + suffix, target=f'"./{dependency}"'))
                    self.write(f"src/{dependency}.ts", "export const value = 42;\n")
                    dependencies.append(dependency)
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for dependency in dependencies:
            with self.subTest(dependency=dependency):
                self.assertIn(f"$(S)/{PROJECT}/src/{dependency}.ts", scan["inputs"])

    def test_dynamic_import_whitespace_and_trailing_comments(self):
        dependencies = []
        for keyword in ("import", "require"):
            for whitespace in ("\t", "\n", "\f", "\r", "\t\f\r\n "):
                for comment in ("/**/", "/* text */", "/* **/", "/* a**/"):
                    dependency = f"spacing{len(dependencies)}"
                    self.write(f"src/{dependency}.spec.ts", f'{keyword}{whitespace}({whitespace}"./{dependency}"{whitespace}{comment}{whitespace})')
                    self.write(f"src/{dependency}.ts", "export const value = 42;\n")
                    dependencies.append(dependency)
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for dependency in dependencies:
            with self.subTest(dependency=dependency):
                self.assertIn(f"$(S)/{PROJECT}/src/{dependency}.ts", scan["inputs"])

    def test_compact_static_clauses_are_ignored_by_the_import_scanner(self):
        statements = (
            'import{value}from"./dependency";',
            'import*as names from"./dependency";',
            'export{value}from"./dependency";',
            'export*from"./dependency";',
        )
        for index, text in enumerate(statements):
            self.write(f"src/compact{index}.spec.ts", text)
        self.write("src/dependency.ts", "export const value = 42;\n")
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        self.assertNotIn(f"$(S)/{PROJECT}/src/dependency.ts", scan["inputs"])

    def test_type_named_default_imports_and_comment_boundaries(self):
        statements = (
            'import type{sep}from{sep}{target};',
            'import type{sep}from{sep}from{sep}{target};',
            'import type{sep}*{sep}as{sep}names{sep}from{sep}{target};',
            'import type{sep}{{name{sep}as{sep}alias}}{sep}from{sep}{target};',
        )
        dependencies = []
        for statement in statements:
            for separator in ("\t", "\f", " /**/ ", " /*body*/ "):
                dependency = f"typed{len(dependencies)}"
                self.write(f"src/{dependency}.spec.ts", statement.format(sep=separator, target=f'"./{dependency}"'))
                self.write(f"src/{dependency}.ts", "export const name = 42;\n")
                dependencies.append(dependency)
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for dependency in dependencies:
            with self.subTest(dependency=dependency):
                self.assertIn(f"$(S)/{PROJECT}/src/{dependency}.ts", scan["inputs"])

    def test_type_default_import_names_extend_from_keyword_prefixes(self):
        dependencies = []
        for prefix in ("f", "fr", "fro", "from"):
            for suffix in ("$", "0", "A", "_", "a", "z"):
                dependency = f"default{len(dependencies)}"
                self.write(f"src/{dependency}.spec.ts", f'import type\t{prefix}{suffix}\tfrom "./{dependency}";')
                self.write(f"src/{dependency}.ts", "export default 42;\n")
                dependencies.append(dependency)
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for dependency in dependencies:
            with self.subTest(dependency=dependency):
                self.assertIn(f"$(S)/{PROJECT}/src/{dependency}.ts", scan["inputs"])

    def test_partial_type_default_imports_do_not_establish_dependencies(self):
        for index, prefix in enumerate(("f", "fr", "fro", "from")):
            self.write(f"src/eof{index}.spec.ts", "import type " + prefix)
        for index, tail in enumerate(('"./ignored"', "'./ignored'", ",", " /", " /*", " /**", " /***", " /*a*b*")):
            self.write(f"src/tail{index}.spec.ts", "import type from" + tail)
        self.write("src/complete.spec.ts", 'import "./dependency";')
        self.write("src/dependency.ts", "export const value = 42;\n")
        self.write("src/ignored.ts", "export const value = 17;\n")
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        self.assertIn(f"$(S)/{PROJECT}/src/dependency.ts", scan["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/src/ignored.ts", scan["inputs"])

    def test_type_default_import_requires_a_module_string(self):
        self.write("src/invalid.spec.ts", "import type from ;")
        result = self.make("-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("src/invalid.spec.ts", result.stderr)

    def test_dynamic_call_repeated_whitespace_and_adjacent_comments(self):
        dependencies = []
        for keyword in ("import", "require"):
            for pattern in ('{keyword}\t\t\f\r\n(\t\t\f\r\n"./{dependency}"\t\t\f\r\n)',
                            '{keyword}("./{dependency}"/**/)',
                            '{keyword}("./{dependency}"/* body */)'):
                dependency = f"adjacent{len(dependencies)}"
                self.write(f"src/{dependency}.spec.ts", pattern.format(keyword=keyword, dependency=dependency))
                self.write(f"src/{dependency}.ts", "export const value = 42;\n")
                dependencies.append(dependency)
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for dependency in dependencies:
            with self.subTest(dependency=dependency):
                self.assertIn(f"$(S)/{PROJECT}/src/{dependency}.ts", scan["inputs"])

    def test_comments_touching_import_identifiers_and_aliases(self):
        statements = (
            'import(\t{target})', 'require(\f{target})',
            'import \t{target}',
            'import name/**/from {target}',
            'import * as/**/names from {target}',
            'import type * as names/**/from {target}',
            'import {{name as alias\t}} from {target}',
            'import {{name as alias\f}} from {target}',
            'import {{name as alias/**/}} from {target}',
            'import type {{name as alias\t}} from {target}',
            'import type {{name as alias\f}} from {target}',
            'import type {{name as alias/**/}} from {target}',
        )
        for index, statement in enumerate(statements):
            self.write(f"src/edge{index}.spec.ts", statement.format(target=f'"./dependency{index}"'))
            self.write(f"src/dependency{index}.ts", "export const name = 42;\n")
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for index in range(len(statements)):
            with self.subTest(statement=index):
                self.assertIn(f"$(S)/{PROJECT}/src/dependency{index}.ts", scan["inputs"])

    def test_slash_after_import_keywords_is_not_an_import_clause(self):
        for index, keyword in enumerate(("import", "export", "require")):
            self.write(f"src/slash{index}.spec.ts", keyword + "/ignored")
        self.write("src/complete.spec.ts", 'import "./dependency";')
        self.write("src/dependency.ts", "export const value = 42;\n")
        self.write("src/ignored.ts", "export const value = 17;\n")
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        self.assertIn(f"$(S)/{PROJECT}/src/dependency.ts", scan["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/src/ignored.ts", scan["inputs"])

    def test_comments_cannot_replace_a_static_module_specifier(self):
        for keyword in ("import", "export"):
            with self.subTest(keyword=keyword):
                self.write("src/invalid.spec.ts", keyword + "/**/;")
                result = self.make("-t")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("src/invalid.spec.ts", result.stderr)

    def test_dynamic_module_specifiers_survive_missing_call_closer(self):
        dependencies = []
        for keyword in ("import", "require"):
            for index, tail in enumerate(("", " ", " /*after*/", " + other", ", options")):
                dependency = f"{keyword}{index}"
                self.write(f"src/{dependency}.spec.ts", f'{keyword}("./{dependency}"{tail}')
                self.write(f"src/{dependency}.ts", "export const value = 42;\n")
                dependencies.append(dependency)
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for dependency in dependencies:
            with self.subTest(dependency=dependency):
                self.assertIn(f"$(S)/{PROJECT}/src/{dependency}.ts", scan["inputs"])

    def test_invalid_clause_tokens_do_not_establish_dependencies(self):
        clauses = (
            ("import", "defaultName", ",", "{", "first", "as", "alias", ",", "second", ",", "}", "from"),
            ("import", "defaultName", ",", "*", "as", "names", "from"),
            ("export", "type", "{", "first", "as", "alias", "}", "from"),
            ("require", "("),
        )
        for clause_index, tokens in enumerate(clauses):
            # Do not enter the module-specifier position, where invalid
            # tokens are configuration errors in upstream's scanner.
            for end in range(1 if tokens[0] == "require" else 2, len(tokens)):
                for invalid_index, invalid in enumerate(("@", ";", "=", "/x", "0", "asx")):
                    text = " ".join(tokens[:end]) + " " + invalid + ' "./ignored";'
                    self.write(f"src/invalid{clause_index}_{end}_{invalid_index}.spec.ts", text)
        self.write("src/complete.spec.ts", 'import "./dependency";\n')
        self.write("src/dependency.ts", "export const first = 42;\n")
        self.write("src/ignored.ts", "export const first = 17;\n")
        scan = next(node for node in self.graph("-t")["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        self.assertIn(f"$(S)/{PROJECT}/src/dependency.ts", scan["inputs"])
        self.assertNotIn(f"$(S)/{PROJECT}/src/ignored.ts", scan["inputs"])

    def test_static_import_export_clauses_and_comments(self):
        clauses = (
            "import {target};",
            "import name from {target};",
            "import * as names from {target};",
            "import {{name}} from {target};",
            "import {{ name as other, second, }} from {target};",
            "import name, {{ first as other, second }} from {target};",
            "import name, * as names from {target};",
            "import type {{ Name as Other, }} from {target};",
            "export {{ name as other, second, }} from {target};",
            "export * from {target};",
            "export * as names from {target};",
            "import /*one*/ name /*two*/, /*three*/ {{ /*four*/ value /*five*/ as /*six*/ other, }} /*seven*/ from /*eight*/ {target};",
        )
        names = []
        for index, clause in enumerate(clauses):
            name = f"dependency{index}"
            target = f'"./{name}"' if index % 2 else f"'./{name}'"
            self.write(f"src/import{index}.spec.ts", clause.format(target=target) + "\n")
            self.write(f"src/{name}.ts", "export const name = 42;\n")
            names.append(name)
        for index, whitespace in enumerate(("\t", "\n", "\r", "\f")):
            name = f"whitespace{index}"
            self.write(f"src/space{index}.spec.ts", f'import{whitespace}{{{whitespace}name{whitespace}as{whitespace}other,{whitespace}}}{whitespace}from{whitespace}"./{name}";\n')
            self.write(f"src/{name}.ts", "export const name = 42;\n")
            names.append(name)
        graph = self.graph("-t")
        scan = next(node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for name in names:
            with self.subTest(dependency=name):
                self.assertIn(f"$(S)/{PROJECT}/src/{name}.ts", scan["inputs"])

    def test_dynamic_calls_ignore_directive_and_property_access(self):
        self.write("src/imports.spec.ts", (
            'const first = import /*before*/ ( /*argument*/ "./dynamic" /*after*/ );\n'
            "const second = require /*before*/ ( /*argument*/ './required' /*after*/ );\n"
            '// nots-ignore-import\nimport {value} from "./ignored";\n'
            'object.require("./property"); object.import("./property");\n'
            '/* import "./commented"; */\n'
            'const literal = `import "./literal"`;\n'
            'export { value } from "./exported";\n'
        ))
        for name in ("dynamic", "required", "exported", "ignored", "property", "commented", "literal"):
            self.write(f"src/{name}.ts", "export const value = 42;\n")
        graph = self.graph("-t")
        scan = next(node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for name in ("dynamic", "required", "exported"):
            self.assertIn(f"$(S)/{PROJECT}/src/{name}.ts", scan["inputs"])
        for name in ("ignored", "property", "commented", "literal"):
            self.assertNotIn(f"$(S)/{PROJECT}/src/{name}.ts", scan["inputs"])

    def test_keyword_prefix_identifiers_and_empty_named_imports(self):
        clauses = (
            "import {name} from {target};",
            "import {name}, {{}} from {target};",
            "import {name}, * as {name} from {target};",
            "import {{ {name} as {name}, }} from {target};",
            "import type {name} from {target};",
            "export type {{ {name} as {name} }} from {target};",
        )
        dependencies = []
        for name in ("a", "as", "f", "fr", "fro", "from", "t", "ty", "typ", "type", "$", "_"):
            for index, clause in enumerate(clauses):
                dependency = f"dependency{len(dependencies)}"
                text = clause.format(name=name, target=f"'./{dependency}'")
                self.write(f"src/import{len(dependencies)}.spec.ts", text)
                self.write(f"src/{dependency}.ts", "export const value = 42;\n")
                dependencies.append(dependency)
        self.write("src/empty.spec.ts", 'import {} from "./empty"; export {} from "./empty";')
        self.write("src/empty.ts", "export {};\n")
        graph = self.graph("-t")
        scan = next(node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for dependency in (*dependencies, "empty"):
            with self.subTest(dependency=dependency):
                self.assertIn(f"$(S)/{PROJECT}/src/{dependency}.ts", scan["inputs"])

    def test_comments_at_each_import_clause_boundary(self):
        statements = (
            "import {sep} './dependency'",
            "import {sep} ( {sep} './dependency' {sep} )",
            "require {sep} ( {sep} './dependency' {sep} )",
            "import {sep} defaultName {sep} , {sep} * {sep} as {sep} all {sep} from {sep} './dependency'",
            "import {sep} defaultName {sep} , {sep} {{ {sep} first {sep} as {sep} alias {sep} , {sep} second {sep} , {sep} }} {sep} from {sep} './dependency'",
            "export {sep} type {sep} {{ {sep} first {sep} as {sep} alias {sep} }} {sep} from {sep} './dependency'",
        )
        dependencies = []
        separators = (("/**/", True), ("/* * text */", False), ("/*a*b*/", False), ("/*a*/\t/*b*/", True), ("\r\n\f", True))
        for index, (separator, included) in enumerate(separators):
            for statement_index, statement in enumerate(statements):
                dependency = f"dependency{index}_{statement_index}"
                text = statement.format(sep=separator).replace("./dependency", "./" + dependency)
                self.write(f"src/comment{index}_{statement_index}.spec.ts", text)
                self.write(f"src/{dependency}.ts", "export const first = 42, second = 0;\n")
                dependencies.append((dependency, included))
        graph = self.graph("-t")
        scan = next(node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for dependency, included in dependencies:
            with self.subTest(dependency=dependency):
                self.assertEqual(f"$(S)/{PROJECT}/src/{dependency}.ts" in scan["inputs"], included)

    def test_truncated_clauses_are_scanned_without_losing_complete_imports(self):
        clauses = (
            "import defaultName, { first as alias, second, } from './dependency'",
            'import defaultName, * as names from "./dependency"',
            "import type { first as alias, } from './dependency'",
            "export { first as alias, second } from './dependency'",
            "require /* before */ ( /* argument */ './dependency' /* after */ )",
        )
        fragments = []
        for index, clause in enumerate(clauses):
            for end in range(clause.index(" ") + 2, len(clause) + 1):
                fragment = clause[:end]
                # Upstream diagnoses an absent module after `from` or an
                # opening call argument; other incomplete clauses are ignored.
                if fragment.endswith("from ") or fragment in (
                    "require /* before */ (", "require /* before */ ( ",
                    "require /* before */ ( /* argument */", "require /* before */ ( /* argument */ ",
                ):
                    continue
                name = f"src/partial{index}_{end}.spec.ts"
                self.write(name, fragment)
                fragments.append(name)
        self.write("src/complete.spec.ts", 'import "./dependency"; // no final newline')
        self.write("src/dependency.ts", "export const first = 42, second = 0;\n")
        graph = self.graph("-t")
        scan = next(node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        self.assertIn(f"$(S)/{PROJECT}/src/dependency.ts", scan["inputs"])
        for name in fragments:
            self.assertIn(f"$(S)/{PROJECT}/{name}", scan["inputs"])

    def test_module_specifiers_missing_after_comments_are_errors(self):
        fragments = (
            "import defaultName, { first as alias, second, } from ",
            'import defaultName, * as names from ',
            "import type { first as alias, } from ",
            "export { first as alias, second } from ",
            "require /* before */ (", "require /* before */ ( ",
            "require /* before */ ( /* argument */", "require /* before */ ( /* argument */ ",
        )
        for index, fragment in enumerate(fragments):
            self.write(f"src/missing{index}.spec.ts", fragment)
        result = self.make("-t")
        self.assertNotEqual(result.returncode, 0)
        for index in range(len(fragments)):
            self.assertIn(f"src/missing{index}.spec.ts", result.stderr)

    def test_unterminated_block_comments_inside_import_clauses(self):
        tokens = (
            ("import", "defaultName", ",", "{", "first", "as", "alias", ",", "second", ",", "}", "from"),
            ("import", "defaultName", ",", "*", "as", "names", "from"),
            ("import", "type", "{", "first", "as", "alias", "}", "from"),
            ("require", "("),
        )
        fragments = []
        for index, clause in enumerate(tokens):
            for end in range(1, len(clause) + 1):
                for comment_index, comment in enumerate(("/", "/*", "/**", "/* a", "/* a*", "/* a**", "/* a*b", "/* * a *")):
                    name = f"src/comment{index}_{end}_{comment_index}.spec.ts"
                    self.write(name, " ".join(clause[:end]) + " " + comment)
                    fragments.append(name)
        self.write("src/complete.spec.ts", 'import "./dependency";')
        self.write("src/dependency.ts", "export const first = 42;\n")
        graph = self.graph("-t")
        scan = next(node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        self.assertIn(f"$(S)/{PROJECT}/src/dependency.ts", scan["inputs"])
        for name in fragments:
            self.assertIn(f"$(S)/{PROJECT}/{name}", scan["inputs"])

    def test_star_runs_in_comments_do_not_establish_imports(self):
        # Upstream's import scanner skips this clause even though the source
        # contains a valid TypeScript block comment. Keep its graph semantics.
        whitespace = "/* ** comment **/"
        self.write("src/imports.spec.ts", f'import{whitespace}{{{whitespace}name{whitespace}as{whitespace}other,{whitespace}}}{whitespace}from{whitespace}"./dependency";\n')
        self.write("src/dependency.ts", "export const name = 42;\n")
        graph = self.graph("-t")
        scan = next(node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        self.assertNotIn(f"$(S)/{PROJECT}/src/dependency.ts", scan["inputs"])

    def test_partial_non_module_clauses_do_not_hide_a_complete_import(self):
        samples = (
            "import", "import('", 'import("value',
            "import /*unfinished", "import(/*unfinished", "import {", "import {name",
            "import {name as", "import {name,", "import value,", "import * as", "import type ",
            'import name from "', 'import name from "./dependency"',
            "require /*unfinished", "export {name as", "export * as ",
        )
        for index, text in enumerate(samples):
            self.write(f"src/partial{index}.spec.ts", text)
        self.write("src/dependency.ts", "export const name = 42;\n")
        graph = self.graph("-t")
        scan = next(node for node in graph["graph"] if any(path.endswith(".ts_test.fake") for path in node["outputs"]))
        for index in range(len(samples)):
            self.assertIn(f"$(S)/{PROJECT}/src/partial{index}.spec.ts", scan["inputs"])
        # The last closed string is an import even without a semicolon.
        self.assertIn(f"$(S)/{PROJECT}/src/dependency.ts", scan["inputs"])

    def test_empty_module_specifiers_are_configuration_errors(self):
        for index, text in enumerate(("import ", "import(", "import( ", "import name from ", "require(")):
            self.write(f"src/invalid{index}.spec.ts", text)
        result = self.make("-t")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("import", result.stderr.lower())
