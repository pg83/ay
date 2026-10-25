import json
import unittest

from tst.lib import make, make_process, node_by_output


S = "$(S)"
B = "$(B)"


class DocsTests(unittest.TestCase):
    def test_unicode_variables_and_recursive_exclusions(self):
        variables = {"ascii": "<tag>&value", "title": "Привет", "emoji": "🚀"}
        graph = make({
            "guide/ya.make": "DOCS()\nDOCS_VARS(" + " ".join(f'"{k}={v}"' for k, v in variables.items()) + ")\n"
                "SRCS(**/*.md EXCLUDE **/private/** **/draft.md)\n"
                "DOCS_INCLUDE_SOURCES(${ARCADIA_ROOT}/code/example.cpp)\nEND()\n",
            "guide/index.md": "# Index\n",
            "guide/deep/page.md": "# Page\n",
            "guide/private/secret.md": "# Secret\n",
            "guide/deep/private/secret.md": "# Secret\n",
            "guide/deep/draft.md": "# Draft\n",
            "code/example.cpp": "",
        }, "guide")
        pre = node_by_output(graph, "$(B)/guide/_preprocessed.tar.gz")
        args = pre["cmds"][1]["cmd_args"]
        self.assertEqual(args[args.index("--vars") + 1], json.dumps(variables, sort_keys=True))
        library = node_by_output(graph, "$(B)/guide/__docs_internal.docslib.fake")
        self.assertIn("$(S)/guide/index.md", library["inputs"])
        self.assertIn("$(S)/guide/deep/page.md", library["inputs"])
        self.assertIn("$(S)/code/example.cpp", library["inputs"])
        self.assertFalse(any("private/" in p or p.endswith("draft.md") for p in library["inputs"]))

    def test_invalid_copy_sources_and_variables_are_diagnosed(self):
        for body, error in (
            ("DOCS_COPY_FILES(FROM)", "missing value for FROM"),
            ("DOCS_COPY_FILES(NAMESPACE)", "missing value for NAMESPACE"),
            ("SRCS(SRCDIR)", "missing SRCDIR"),
            ("DOCS_VARS(title)", "expected name=value"),
            ("DOCS_VARS(title=a=b)", "expected name=value"),
        ):
            with self.subTest(body=body):
                result = make_process({"guide/ya.make": f"DOCS()\n{body}\nEND()\n"}, "guide")
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(error, result.stderr)
                self.assertNotIn("panic:", result.stderr)

    def test_book_generated_inputs_and_plantuml(self):
        graph = make({
            "guide/ya.make": """DOCS(handbook)
USE_PLANTUML()
DOCS_VARS(a=default z=last)
SET(a override)
DOCS_COPY_FILES(FROM ${ARCADIA_ROOT}/assets/ NAMESPACE images pic.png)
RUN_PYTHON3(gen.py --archive bundle.tar OUT_NOAUTO bundle.tar)
END()
""",
            "guide/.yfm": "title: Handbook\n",
            "guide/index.md": "# Start\n",
            "guide/gen.py": "print('generated')\n",
            "assets/pic.png": "picture",
        }, "guide")
        library = node_by_output(graph, f"{B}/guide/__docs_internal.docslib.fake")
        copy = node_by_output(graph, f"{B}/guide/images/pic.png")
        run = node_by_output(graph, f"{B}/guide/bundle.tar")
        self.assertIn(copy['uid'], library['deps'])
        self.assertIn(run['uid'], library['deps'])
        self.assertIn(f"{S}/guide/.yfm", library['inputs'])
        self.assertIn(f"{S}/guide/ya.make", library['inputs'])
        self.assertIn(f"{B}/guide/bundle.tar", library['cmds'][0]['cmd_args'])
        self.assertEqual(copy['cmds'][0]['cmd_args'][-5:], [
            '--src-dir', f'{S}/assets/', '--dst-dir', f'{B}/guide/images', 'pic.png'])
        pre = node_by_output(graph, f"{B}/guide/_preprocessed.tar.gz")
        book = node_by_output(graph, f"{B}/guide/handbook.tar.gz")
        self.assertEqual(pre['requirements']['cpu'], 4)
        args = pre['cmds'][1]['cmd_args']
        self.assertEqual(args[args.index('--vars') + 1], '{"a": "override", "z": "last"}')
        self.assertEqual(pre['cmds'][1]['env']['LANG'], 'en_US.UTF-8')
        self.assertNotIn('LANG', book['cmds'][1]['env'])
        self.assertIn(f'{B}/guide/__docs_internal.docslib.fake', pre['inputs'])
        self.assertIn(f'{B}/guide/_preprocessed.tar.gz', book['inputs'])
        self.assertIn(library['uid'], pre['deps'])
        self.assertIn(pre['uid'], book['deps'])
        self.assertEqual(book['outputs'], [f'{B}/guide/handbook.tar.gz',
            f'{B}/guide/preprocessed.tar.gz', f'{B}/guide/docs.manifest'])
        self.assertIn(book['uid'], graph['result'])

    def test_plantuml_builds_java_tool_and_font_union(self):
        resources = {
            'build/platform/python/ymake_python3': 'YMAKE_PYTHON3',
            'build/platform/java/jdk': 'JDK_DEFAULT',
            'build/platform/java/jdk/jdk17': 'JDK17',
            'build/platform/java/uberjar': 'UBERJAR',
            'build/platform/java/uberjar/uberjar17': 'UBERJAR17',
            'build/platform/java/scriptgen': 'SCRIPTGEN',
        }
        files = {
            f'{path}/ya.make': f'RESOURCES_LIBRARY()\nDECLARE_EXTERNAL_RESOURCE({name} sbr:{i})\nEND()\n'
            for i, (path, name) in enumerate(resources.items(), 1)
        }
        graph = make({
            **files,
            'guide/ya.make': 'DOCS()\nUSE_PLANTUML()\nEND()\n',
            'contrib/tools/plantuml/ya.make': 'JAVA_PROGRAM(diagrams)\nJDK_VERSION(17)\nUBERJAR()\nPEERDIR(vendor/drawing/2.0 contrib/java/openjdk-fontconfig)\nEND()\n',
            'vendor/drawing/2.0/ya.make': 'JAVA_CONTRIB()\nJAR_RESOURCE(123)\nSRC_RESOURCE(456)\nEND()\n',
            'contrib/java/openjdk-fontconfig/ya.make': 'UNION(fonts)\nFROM_SANDBOX(789 OUT face.ttf config.properties)\nEND()\n',
        }, 'guide', opensource=False)
        fetched = node_by_output(graph, f'{B}/vendor/drawing/2.0/drawing-2.0.jar')
        jar = node_by_output(graph, f'{B}/contrib/tools/plantuml/diagrams.jar')
        tool = node_by_output(graph, f'{B}/contrib/tools/plantuml/diagrams.run.cp.jar')
        fonts = node_by_output(graph, f'{B}/contrib/java/openjdk-fontconfig/fonts.pkg.fake')
        pre = node_by_output(graph, f'{B}/guide/_preprocessed.tar.gz')
        self.assertIn(fetched['uid'], jar['deps'])
        self.assertIn(jar['uid'], tool['deps'])
        host_fonts = next(n for n in graph['graph'] if fonts['outputs'][0] in n['outputs'] and n['uid'] in tool['deps'])
        self.assertNotEqual(fonts['platform'], host_fonts['platform'])
        self.assertIn(tool['uid'], pre['deps'])
        self.assertIn(fonts['uid'], pre['deps'])
        self.assertEqual(fetched['cmds'][0]['cmd_args'][-3:], [
            '123', '--copy-to', f'{B}/vendor/drawing/2.0/drawing-2.0.jar'])
        self.assertIn('$(RESOURCE_ROOT)/sbr/123/resource', fetched['cmds'][0]['cmd_args'])
        self.assertIn('$(B)/vcs.json', jar['cmds'][4]['cmd_args'])
        self.assertIn(f'{B}/vendor/drawing/2.0/drawing-2.0.jar', jar['cmds'][7]['cmd_args'])
        self.assertEqual(fonts['outputs'], [f'{B}/contrib/java/openjdk-fontconfig/fonts.pkg.fake',
            f'{B}/contrib/java/openjdk-fontconfig/face.ttf', f'{B}/contrib/java/openjdk-fontconfig/config.properties'])

    def test_copy_consumes_a_generated_file(self):
        graph = make({
            'guide/ya.make': """DOCS()
DOCS_COPY_FILES(FROM ${BINDIR}/incoming NAMESPACE assets chart.png)
RUN_PYTHON3(gen.py OUT_NOAUTO incoming/chart.png)
END()
""",
            'guide/gen.py': 'print("chart")\n',
        }, 'guide')
        producer = node_by_output(graph, f'{B}/guide/incoming/chart.png')
        copy = node_by_output(graph, f'{B}/guide/assets/chart.png')
        self.assertIn(producer['uid'], copy['deps'])
        self.assertIn(f'{B}/guide/incoming/chart.png', copy['inputs'])

    def test_standalone_library_and_reused_docs_peer(self):
        files = {
            'common/ya.make': 'DOCS_LIBRARY(contents)\nDOCS_DIR(text)\nEND()\n',
            'text/page.md': '# Page\n',
            'site/ya.make': 'DOCS()\nPEERDIR(part common)\nEND()\n',
            'part/ya.make': 'DOCS()\nRUN_PYTHON3(gen.py OUT_NOAUTO page.md)\nEND()\n',
            'part/gen.py': "print('# Generated')\n",
        }
        library_graph = make(files, 'common')
        library = node_by_output(library_graph, f'{B}/common/contents.docslib')
        self.assertIn(library['uid'], library_graph['result'])
        self.assertIn(f'{S}/text/page.md', library['inputs'])
        for target, extra in [('site', 'part'), ('part', 'site')]:
            graph = make(files, target, extra)
            generated = [n for n in graph['graph'] if f'{B}/part/page.md' in n['outputs']]
            self.assertEqual(len(generated), 1)
            for archive in ['__docs_external.docslib', '__docs_internal.docslib.fake']:
                node = node_by_output(graph, f'{B}/part/{archive}')
                self.assertIn(generated[0]['uid'], node['deps'])
            for module in ['site', 'part']:
                self.assertIn(node_by_output(graph, f'{B}/{module}/{module}.tar.gz')['uid'], graph['result'])

    def test_add_modules_to_results_includes_variants(self):
        graph = make({'guide/ya.make': 'DOCS()\nEND()\n'}, 'guide', '--add-modules-to-results')
        results = set(graph['result'])
        for name in ('__docs_internal.docslib.fake', '_preprocessed.tar.gz', 'guide.tar.gz'):
            self.assertIn(node_by_output(graph, f'{B}/guide/{name}')['uid'], results)
        self.assertEqual(len(results), 3)

    def test_docs_peers_and_library_sources(self):
        graph = make({
            'site/ya.make': 'DOCS()\nPEERDIR(part shared)\nDOCS_CONFIG(config/site.yml)\nEND()\n',
            'site/index.md': '# Site\n',
            'part/ya.make': 'DOCS()\nPEERDIR(shared)\nDOCS_DIR(content)\nEND()\n',
            'content/page.md': '# Part\n',
            'shared/ya.make': 'DOCS_LIBRARY(common)\nSRCS(SRCDIR src **/*.md EXCLUDE src/private/*)\nDOCS_INCLUDE_SOURCES(code/sample.cpp)\nEND()\n',
            'shared/src/top.md': '# Top\n',
            'shared/src/deep/page.md': '# Deep\n',
            'shared/src/private/secret.md': '# Hidden\n',
            'code/sample.cpp': 'int sample;\n',
            'config/site.yml': 'title: Site\n',
        }, 'site')
        shared = node_by_output(graph, f'{B}/shared/common.docslib')
        self.assertIn(f'{S}/shared/src/top.md', shared['inputs'])
        self.assertIn(f'{S}/shared/src/deep/page.md', shared['inputs'])
        self.assertNotIn(f'{S}/shared/src/private/secret.md', shared['inputs'])
        self.assertNotIn(f'{S}/shared/ya.make', shared['inputs'])
        self.assertIn(f'{S}/code/sample.cpp', shared['inputs'])
        external = node_by_output(graph, f'{B}/part/__docs_external.docslib')
        self.assertEqual(external['cmds'][0]['cmd_args'][2:5], ['--docs-dir', 'content', 'content'])
        pre = node_by_output(graph, f'{B}/site/_preprocessed.tar.gz')
        self.assertEqual(pre['cmds'][0]['cmd_args'][4:], [
            'shared/common.docslib', 'part/__docs_external.docslib', 'site/__docs_internal.docslib.fake'])
        self.assertIn(external['uid'], pre['deps'])
        self.assertIn(shared['uid'], pre['deps'])
        self.assertEqual(pre['cmds'][1]['cmd_args'][-1], f'{S}/config/site.yml')
        outputs = [o for n in graph['graph'] for o in n['outputs']]
        self.assertNotIn(f'{B}/part/_preprocessed.tar.gz', outputs)


if __name__ == '__main__':
    unittest.main()
