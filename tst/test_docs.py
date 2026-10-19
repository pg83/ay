import json
import tempfile
from pathlib import Path
import unittest

import lib

from lib import make, node_by_output


S = "$(S)"
B = "$(B)"


class DocsTests(unittest.TestCase):
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
        self.assertIn(library['uid'], pre['deps'])
        self.assertIn(pre['uid'], book['deps'])
        self.assertEqual(book['outputs'], [f'{B}/guide/handbook.tar.gz',
            f'{B}/guide/preprocessed.tar.gz', f'{B}/guide/docs.manifest'])
        self.assertIn(book['uid'], graph['result'])

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

    def test_explicit_inputs_without_sandboxing(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            (root / "ya.conf").write_text('[flags]\nOPENSOURCE = "yes"\n')
            (root / "guide").mkdir()
            (root / "guide/ya.make").write_text(
                "DOCS()\nRUN_PYTHON3(gen.py OUT_NOAUTO bundle.tar)\nEND()\n")
            (root / "guide/gen.py").write_text("print('hi')\n")
            (root / "guide/index.md").write_text("# Start\n")
            graph = json.loads(lib.run("make", "-j0", "-G", "--source-root", root, "guide").stdout)
        source = node_by_output(graph, f"{B}/guide/__docs_internal.docslib.fake")
        self.assertIn(f"{S}/guide/index.md", source['inputs'])
        run = node_by_output(graph, f"{B}/guide/bundle.tar")
        self.assertEqual(run['inputs'], [f"{S}/guide/gen.py"])

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
