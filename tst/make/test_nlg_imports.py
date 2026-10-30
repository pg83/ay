from tst.make.lib import MakeTest, PROJECT


class NlgImportsTest(MakeTest):
    def test_multiple_imports_with_root_names_and_alias_spacing(self):
        roots = ("A_ay_e2e_nlg", "0_ay_e2e_nlg")
        paths = tuple(root + "/Value.nlg" for root in roots)
        for root in roots:
            self.assertFalse((self.root / root).exists())
            self.write_tree(root + "/ya.make", "LIBRARY()\nCOMPILE_NLG(Value.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
            self.write_tree(root + "/Value.nlg", "{% macro value() %}42{% endmacro %}\n")
            self.write_tree(root + "/config.json", "{}\n")
            self.write_tree(root + "/translations.json", "{}\n")
        first, second = paths
        statements = (
            f"import '{first}','{second}'",
            f"import '{first}'as Alias \t, '{second}'  as Other",
            f"import '{first}' \t, '{second}' with context \t, '{first}'",
            f'import "{first}", "{second}"',
        )
        names = []
        for index, statement in enumerate(statements):
            name = f"answer{index}.nlg"
            names.append(name)
            self.write(name, "{% " + statement + " %}\n")
        self.write("ya.make", "LIBRARY()\nPEERDIR(" + " ".join(roots) + ")\nCOMPILE_NLG(" + " ".join(names) + " NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("config.json", "{}\n")
        self.write("translations.json", "{}\n")
        graph = self.graph()
        for name in names:
            inputs = self.node(graph, name + ".cpp.o")["inputs"]
            for path in paths:
                self.assertIn(f"$(B)/{path}.h", inputs)

    def test_invalid_single_quoted_imports_and_context_tails_are_ignored(self):
        path = f"{PROJECT}/deps/ignored.nlg"
        commands = (
            f"nXgimport '{path}'", f"import '{path}'  unexpected",
            f"from '{path}' import value with context unexpected",
            f"from '{path}' import value with context\t unexpected",
        )
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/deps)\nCOMPILE_NLG(answer.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("answer.nlg", "".join("{% " + command + " %}\n" for command in commands))
        self.write("deps/ya.make", "LIBRARY()\nCOMPILE_NLG(ignored.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("deps/ignored.nlg", "{% macro value() %}42{% endmacro %}\n")
        for prefix in ("", "deps/"):
            self.write(prefix + "config.json", "{}\n")
            self.write(prefix + "translations.json", "{}\n")
        self.assertNotIn(f"$(B)/{PROJECT}/deps/ignored.nlg.h", self.node(self.graph(), "answer.nlg.cpp.o")["inputs"])

    def test_alias_character_classes_and_internal_whitespace(self):
        commands = []
        names = []
        for keyword in ("import", "ext_nlgimport", "from", "ext_from"):
            for whitespace in (" ", "\t\v\f\r\n "):
                for alias in ("0Alias", "Alias0_", "_Alias0", "a0A_"):
                    name = f"A0_{len(names)}.nlg"
                    names.append(name)
                    target = f"{PROJECT}/deps/{name}"
                    quoted = f'"{target}"' if len(names) % 2 else f"'{target}'"
                    if keyword.endswith("from"):
                        command = f"{keyword}{whitespace}{quoted}{whitespace}import{whitespace}{alias}{whitespace},{whitespace}{alias}Other{whitespace}with{whitespace}context"
                    else:
                        command = f"{keyword}{whitespace}{quoted}{whitespace}as{whitespace}{alias}{whitespace}with{whitespace}context"
                    commands.append("{% " + command + " %}\n")
                    self.write("deps/" + name, "{% macro value() %}42{% endmacro %}\n")
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/deps)\nCOMPILE_NLG(answer.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("answer.nlg", "".join(commands))
        self.write("deps/ya.make", "LIBRARY()\nCOMPILE_NLG(" + " ".join(names) + " NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        for prefix in ("", "deps/"):
            self.write(prefix + "config.json", "{}\n")
            self.write(prefix + "translations.json", "{}\n")
        inputs = self.node(self.graph(), "answer.nlg.cpp.o")["inputs"]
        for name in names:
            with self.subTest(template=name):
                self.assertIn(f"$(B)/{PROJECT}/deps/{name}.h", inputs)

    def test_invalid_keyword_and_suffix_tokens_do_not_add_dependencies(self):
        path = f"{PROJECT}/deps/ignored.nlg"
        commands = [
            f'extX_import "{path}"', f'ext_fXom "{path}" import value',
            f'import "{path}" asWrong', f'import "{path}" as Alias unexpected',
            f'import "{path}" with unrelated', f'import "{path}" with context unexpected',
            f'from "{path}" import value unexpected', f'from "{path}" import value with unrelated',
        ]
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/deps)\nCOMPILE_NLG(answer.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("answer.nlg", "".join("{% " + command + " %}\n" for command in commands))
        self.write("deps/ya.make", "LIBRARY()\nCOMPILE_NLG(ignored.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("deps/ignored.nlg", "{% macro value() %}42{% endmacro %}\n")
        for prefix in ("", "deps/"):
            self.write(prefix + "config.json", "{}\n")
            self.write(prefix + "translations.json", "{}\n")
        self.assertNotIn(f"$(B)/{PROJECT}/deps/ignored.nlg.h", self.node(self.graph(), "answer.nlg.cpp.o")["inputs"])

    def test_non_import_quotes_and_invalid_paths_are_not_dependencies(self):
        ignored = f"{PROJECT}/deps/ignored.nlg"
        commands = (
            f'import "{ignored}\'',
            f"import '{ignored}\"",
            'import ""',
            f'import "{ignored}" as "alias"',
            f'from "{ignored}" import "value"',
            f'set value = "{ignored}"',
            f'import "{PROJECT}/deps/ignored.txt"',
            f'from "{ignored}" import first with unrelated',
        )
        text = "".join("{% " + command + " %}\n" for command in commands)
        text += "{% \t\v\f\r\n" + f'import "{PROJECT}/deps/base.nlg"' + "\n\r\f\v\t %}\n"
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/deps)\nCOMPILE_NLG(answer.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("answer.nlg", text)
        self.write("config.json", "{}\n")
        self.write("translations.json", "{}\n")
        self.write("deps/ya.make", "LIBRARY()\nCOMPILE_NLG(base.nlg ignored.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        for name in ("base.nlg", "ignored.nlg", "ignored.txt"):
            self.write("deps/" + name, "{% macro value() %}42{% endmacro %}\n")
        self.write("deps/config.json", "{}\n")
        self.write("deps/translations.json", "{}\n")
        inputs = self.node(self.graph(), "answer.nlg.cpp.o")["inputs"]
        self.assertIn(f"$(B)/{PROJECT}/deps/base.nlg.h", inputs)
        self.assertNotIn(f"$(B)/{PROJECT}/deps/ignored.nlg.h", inputs)

    def test_truncated_statements_and_comments_do_not_abort_generation(self):
        path = f"{PROJECT}/deps/base.nlg"
        imports = (
            f'from "{path}" import first, second with context',
            f"ext_from '{path}' import first, second with context",
            f'import "{path}" as first with context, "{path}" as second',
            f"ext_nlgimport '{path}' as alias with context",
        )
        fragments = ["{% " + statement[:end] + " %}\n" for statement in imports for end in range(len(statement))]
        fragments.extend(("{{ variable }}\n", "{text\n", "# skipped comment\n", "{% {%%}\n"))
        fragments.append('{% from "' + path + '" import first %}\n')
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/deps)\nCOMPILE_NLG(answer.nlg truncated.nlg comment.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("answer.nlg", "".join(fragments))
        self.write("deps/base.nlg", "{% macro first() %}42{% endmacro %}\n")
        self.write("deps/ya.make", "LIBRARY()\nCOMPILE_NLG(base.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("deps/config.json", "{}\n")
        self.write("deps/translations.json", "{}\n")
        self.write("truncated.nlg", '{% import "unterminated')
        self.write("comment.nlg", "# no final newline")
        self.write("config.json", "{}\n")
        self.write("translations.json", "{}\n")
        graph = self.graph()
        self.node(graph, "answer.nlg.cpp")
        self.node(graph, "truncated.nlg.cpp")
        self.assertIn(f"$(B)/{PROJECT}/deps/base.nlg.h", self.node(graph, "answer.nlg.cpp.o")["inputs"])

    def test_import_aliases_external_modules_and_context(self):
        statements = (
            'from "{path}" import value',
            "from '{path}' import first, second with context",
            'import "{path}"',
            "import '{path}' as alias",
            'nlgimport "{path}" as alias with context',
            'ext_from "{path}" import first, second with context',
            "ext_import '{path}' as alias with context",
            'ext_nlgimport "{path}"',
        )
        commands = []
        names = []
        for index, statement in enumerate(statements):
            name = f"template{index}.nlg"
            names.append(name)
            path = f"{PROJECT}/deps/{name}"
            commands.append("{% " + statement.format(path=path) + " %}\n")
            self.write("deps/" + name, "{% macro value() %}42{% endmacro %}\n")
        commands.append(f'{{% import "{PROJECT}/deps/template0.nlg" as first, \'{PROJECT}/deps/template1.nlg\' as second with context %}}\n')
        commands.append(f'{{% ext_import "{PROJECT}/deps/template2.nlg" with context, \'{PROJECT}/deps/template3.nlg\' as alias %}}\n')
        for whitespace in ("\t", "\n", "\v", "\f", "\r"):
            commands.append("{% " + f'ext_from{whitespace}"{PROJECT}/deps/template4.nlg"{whitespace}import{whitespace}first{whitespace},{whitespace}second{whitespace}with{whitespace}context' + " %}\n")
        self.write("ya.make", f"LIBRARY()\nPEERDIR({PROJECT}/deps)\nCOMPILE_NLG(answer.nlg NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("answer.nlg", "".join(commands))
        self.write("config.json", "{}\n")
        self.write("translations.json", "{}\n")
        self.write("deps/ya.make", "LIBRARY()\nCOMPILE_NLG(" + " ".join(names) + " NLG_CONFIG config.json TRANSLATIONS_JSON translations.json)\nEND()\n")
        self.write("deps/config.json", "{}\n")
        self.write("deps/translations.json", "{}\n")
        graph = self.graph()
        compiled = self.node(graph, "answer.nlg.cpp.o")
        generated = self.node(graph, "answer.nlg.cpp")
        for name in names:
            with self.subTest(template=name):
                self.assertIn(f"$(B)/{PROJECT}/deps/{name}.h", compiled["inputs"])
        for index in (2, 3, 4, 5, 6, 7):
            self.assertIn(f"$(B)/{PROJECT}/deps/template{index}.nlg.pb.txt", generated["inputs"])
