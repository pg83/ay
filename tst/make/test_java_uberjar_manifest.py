import unittest

from tst import lib


GROUPS = [
    'UBERJAR_HIDING_PREFIX(shaded)',
    'UBERJAR_HIDE_EXCLUDE_PATTERN(public.** keep.**)',
    'UBERJAR_HIDE_INCLUDE_PATTERN(internal.**)',
    'UBERJAR_PATH_EXCLUDE_PREFIX(META-INF/old)',
    'UBERJAR_MANIFEST_TRANSFORMER_MAIN(example.Main)',
    'UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Title Example App)',
    'UBERJAR_APPENDING_TRANSFORMER(reference.conf extras.conf)',
    'UBERJAR_SERVICES_RESOURCE_TRANSFORMER()',
]
ORDERED = ['--shade-prefix', 'shaded', '--shade-exclude', 'public.**', '--shade-exclude', 'keep.**',
           '--shade-include', 'internal.**', '--uber-exclude', 'META-INF/old', '--manifest-main', 'example.Main',
           '--manifest-attribute', 'Title:Example App', '--append-transformer', 'reference.conf',
           '--append-transformer', 'extras.conf', '--service-transformer']
CASES = {
    'ordered': ('\n'.join(GROUPS), ORDERED),
    'reverse': ('\n'.join(reversed(GROUPS)), ORDERED),
    'repeat': ('''UBERJAR_HIDING_PREFIX(first)
UBERJAR_HIDING_PREFIX(last)
UBERJAR_MANIFEST_TRANSFORMER_MAIN(old.Main)
UBERJAR_MANIFEST_TRANSFORMER_MAIN(new.Main)
UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(One First Value)
UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Two Second Value)
UBERJAR_APPENDING_TRANSFORMER(first.conf)
UBERJAR_APPENDING_TRANSFORMER(last.conf)
UBERJAR_SERVICES_RESOURCE_TRANSFORMER()
UBERJAR_SERVICES_RESOURCE_TRANSFORMER()''',
               ['--shade-prefix', 'last', '--manifest-main', 'new.Main', '--manifest-attribute', 'One:First Value',
                '--manifest-attribute', 'Two:Second Value', '--append-transformer', 'first.conf',
                '--append-transformer', 'last.conf', '--service-transformer']),
    'raw-before': ('''SET(UBERJAR_MANIFEST_TRANSFORMER_MAIN_FLAG --manifest-main raw.Main)
SET(UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE_FLAGS --manifest-attribute Raw:before)
UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Macro value)
UBERJAR_MANIFEST_TRANSFORMER_MAIN(macro.Main)''',
                   ['--manifest-main', 'macro.Main', '--manifest-attribute', 'Raw:before', '--manifest-attribute', 'Macro:value']),
    'raw-after': ('''UBERJAR_MANIFEST_TRANSFORMER_MAIN(macro.Main)
UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Macro value)
SET(UBERJAR_MANIFEST_TRANSFORMER_MAIN_FLAG --manifest-main raw.Main)
SET(UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE_FLAGS --manifest-attribute Raw:after)
SET_APPEND(UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE_FLAGS --manifest-attribute Tail:last)''',
                  ['--manifest-main', 'raw.Main', '--manifest-attribute', 'Raw:after', '--manifest-attribute', 'Tail:last']),
    'variables': ('''SET(VALUE First Value)
UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Title ${VALUE})
SET(VALUE Later Value)
SET_APPEND(UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE_FLAGS --manifest-attribute Late:${VALUE})''',
                  ['--manifest-attribute', 'Title:First Value', '--manifest-attribute', 'Late:Later', 'Value']),
    'quoted': ('''UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Title "Example App")
UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Second "two words")''',
               ['--manifest-attribute', 'Title:Example App', '--manifest-attribute', 'Second:two words']),
    'empty': ('UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Blank)', ['--manifest-attribute', 'Blank:']),
    'plain': ('UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Title plain)', ['--manifest-attribute', 'Title:plain']),
    'raw-spaces': ('''SET(UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE_FLAGS --manifest-attribute "Title:Two Words")
SET_APPEND(UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE_FLAGS --manifest-attribute "Second:More Words")''',
                   ['--manifest-attribute', 'Title:Two', 'Words', '--manifest-attribute', 'Second:More', 'Words']),
    'no-uber': ('\n'.join(GROUPS), None),
    'sibling': ('UBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Owner Own Value)\nPEERDIR(meadow/plain)',
                ['--jar', '$(B)/meadow/plain/plain.jar', '--manifest-attribute', 'Owner:Own Value']),
}


def fixture(kind):
    body, _ = CASES[kind]
    return lib.java_tree(**{
        'meadow/gen/ya.make': 'JAVA_PROGRAM(bundle)\nNO_LINT()\n' + ('' if kind == 'no-uber' else 'UBERJAR()\n') + 'JAVA_SRCS(Main.java)\n' + body + '\nEND()\n',
        'meadow/gen/Main.java': 'public class Main {}\n',
        'meadow/plain/ya.make': 'JAVA_LIBRARY_NON_SPLIT(plain)\nNO_LINT()\nJAVA_SRCS(Plain.java)\nUBERJAR_MANIFEST_TRANSFORMER_ATTRIBUTE(Peer Peer Value)\nEND()\n',
        'meadow/plain/Plain.java': 'class Plain {}\n',
        'build/platform/java/scriptgen/ya.make': lib.resource_lib('SCRIPTGEN', 13),
        'build/platform/java/uberjar/ya.make': lib.resource_lib('UBERJAR', 12),
        'build/platform/java/uberjar/uberjar17/ya.make': lib.resource_lib('UBERJAR17', 12),
        'build/platform/java/uberjar/uberjar26/ya.make': lib.resource_lib('UBERJAR26', 12),
    })


def shader_options(node):
    commands = [c['cmd_args'] for c in node['cmds'] if 'ru.yandex.devtools.emigrante.Main' in c['cmd_args']]
    if not commands:
        return None
    args = commands[0]
    return args[args.index('$(B)/meadow/gen/bundle.lib.jar') + 1:]


class JavaUberjarManifestTest(unittest.TestCase):
    def test_template_groups_variables_and_command_quotes(self):
        for kind, (_, expected) in CASES.items():
            with self.subTest(kind=kind):
                graph = lib.make(fixture(kind), 'meadow/gen')
                jar = lib.node_by_output(graph, '$(B)/meadow/gen/bundle.jar')
                self.assertEqual(shader_options(jar), expected)
                self.assertIn('$(S)/meadow/gen/Main.java', jar['inputs'])
                self.assertIn('$(S)/build/scripts/prepare_jar_build.py', jar['inputs'])
                ids = {n['uid'] for n in graph['graph']}
                self.assertTrue(set(graph['result']) <= ids)
                self.assertTrue(all(set(n['deps']) <= ids for n in graph['graph']))
                nodes = {n['uid']: n for n in graph['graph']}
                pending = list(graph['result'])
                reachable = set()
                while pending:
                    uid = pending.pop()
                    if uid not in reachable:
                        reachable.add(uid)
                        pending.extend(nodes[uid]['deps'])
                self.assertIn(jar['uid'], reachable)
                if kind == 'sibling':
                    peer = lib.node_by_output(graph, '$(B)/meadow/plain/plain.jar')
                    self.assertIsNone(shader_options(peer))
                    self.assertIn(peer['uid'], jar['deps'])
                    self.assertIn(peer['outputs'][0], jar['inputs'])


if __name__ == '__main__':
    unittest.main()
