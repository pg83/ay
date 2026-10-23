"""Sandbox configuration identity is distinct from its inherited Python language."""
import hashlib
import unittest

import lib
from test_python2 import python2_base
from test_python_srcs import python_base, objcopy_nodes, kvs


TASK = 'grove/jobs/CountAcorns'
BUILD = '$(B)/' + TASK
SOURCE = '$(S)/' + TASK


def fixture(macro='SANDBOX_PY3_TASK'):
    files = python_base()
    files.update(python2_base())
    lib.tool_program(files, 'tools/ink', 'ink')
    files['policy/python.inc'] = '''PY_SRCS(__init__.py leaves.py)
RUN_PROGRAM(tools/ink capture ${MODULE_KIND} ${MODULE_TAG} ${_PYTHON_VER} ${PYTHON2} ${PYTHON3}
    IN seed.in OUT_NOAUTO ${MODULE_TAG}.dat)
RESOURCE(${MODULE_TAG}.dat recorded/${MODULE_TAG})
'''
    files[TASK + '/ya.make'] = f'{macro}(acorns)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nINCLUDE(${{ARCADIA_ROOT}}/policy/python.inc)\nEND()\n'
    files[TASK + '/__init__.py'] = ''
    files[TASK + '/leaves.py'] = 'leaf = 8\n'
    files[TASK + '/seed.in'] = 'seed\n'
    for directory in ('sandbox/sdk2', 'sandbox/bin', 'sandbox/taskbox/worker'):
        files[directory + '/ya.make'] = f'PY23_LIBRARY()\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nPY_SRCS(side.py)\nEND()\n'
        files[directory + '/side.py'] = ''
    return files


def make(files, *roots):
    roots = roots or (TASK,)
    return lib.make(files, roots[-1], *roots[:-1], '--sandboxing', '--strip-packages-from-results')


def kv_only_hash(payload, tag):
    # Pinned resource_handler/packer.h: sorted tokens + UnitPath, then raw MODULE_TAG.
    return hashlib.md5((','.join(sorted(payload + ['$S/' + TASK])) + tag).encode()).hexdigest()[:26]


def resource_kvs(node):
    return kvs(node) if '--kvs' in node['cmds'][0]['cmd_args'] else []


def resources(graph, consumer):
    return [node for node in objcopy_nodes(graph, TASK) if node['uid'] in consumer['deps']]


class SandboxModuleIdentityTest(unittest.TestCase):
    def assert_context(self, graph, macro, tag):
        binary = tag == 'SB_TASK_BIN'
        py2 = tag == 'PY2' or binary and macro == 'SANDBOX_PY23_TASK'
        name = 'acorns' if binary else ('libpy' if py2 else 'libpy3') + 'acorns.global.a'
        consumer = lib.node_by_output(graph, BUILD + '/' + name)
        generated = lib.node_by_output(graph, BUILD + '/' + tag + '.dat')
        tool = lib.node_by_output(graph, '$(B)/tools/ink/ink')
        self.assertEqual(generated['cmds'][0]['cmd_args'], [
            tool['outputs'][0], 'capture', macro, tag, 'py2' if py2 else 'py3',
            'yes' if py2 else 'no', 'no' if py2 else 'yes'])
        self.assertIn(tool['uid'], generated['deps'])
        self.assertIn(SOURCE + '/seed.in', generated['inputs'])
        owned = resources(graph, consumer)
        registry_key = 'SANDBOX_TASK_REGISTRY.' + TASK.replace('/', '.') + '=' + TASK.replace('/', '.')
        registry = next(n for n in owned if registry_key in resource_kvs(n))
        self.assertEqual(registry['outputs'], [BUILD + '/objcopy_' + kv_only_hash([registry_key], tag) + '.o'])
        bytecode = lib.node_by_output(graph, BUILD + '/__init__.py.yapyc' + ('' if py2 else '3'))
        packaged = next(n for n in owned if SOURCE + '/__init__.py' in n['inputs'])
        self.assertIn(bytecode['outputs'][0], packaged['inputs'])
        self.assertIn(bytecode['uid'], packaged['deps'])
        self.assertIn(SOURCE + '/__init__.py', bytecode['inputs'])
        compiler = lib.node_by_output(graph, '$(B)/tools/' + ('py2cc/py2cc' if py2 else 'py3cc/py3cc'))
        self.assertIn(compiler['uid'], bytecode['deps'])
        payload = next(n for n in owned if generated['outputs'][0] in n['inputs'])
        self.assertIn(generated['uid'], payload['deps'])
        for node in owned:
            for tool_path in ('tools/rescompiler/rescompiler', 'tools/rescompressor/rescompressor'):
                resource_tool = lib.node_by_output(graph, '$(B)/' + tool_path)
                self.assertIn(resource_tool['uid'], node['deps'])
                self.assertIn(resource_tool['outputs'][0], node['inputs'])
        namespaces = [n for n in owned if any(k.startswith('py/namespace/') for k in resource_kvs(n))]
        self.assertEqual(bool(namespaces), not py2)
        main = [n for n in owned if 'PY_MAIN=sandbox.taskbox.binary:main' in resource_kvs(n)]
        self.assertEqual(len(main), int(binary))
        if binary:
            self.assertEqual(main[0]['outputs'], [BUILD + '/objcopy_' + kv_only_hash(['PY_MAIN=sandbox.taskbox.binary:main'], tag) + '.o'])
        return consumer, registry, packaged

    def test_python3_binary_and_library_have_distinct_resources_and_shared_source_producer(self):
        graph = make(fixture())
        binary, br, bp = self.assert_context(graph, 'SANDBOX_PY3_TASK', 'SB_TASK_BIN')
        library, lr, lp = self.assert_context(graph, 'SANDBOX_PY3_TASK', 'PY3')
        self.assertNotEqual(br['outputs'], lr['outputs'])
        self.assertNotEqual(bp['outputs'], lp['outputs'])
        self.assertNotIn(br['uid'], library['deps'])
        self.assertNotIn(lr['uid'], binary['deps'])
        self.assertEqual(len([n for n in graph['graph'] if BUILD + '/__init__.py.yapyc3' in n['outputs']]), 1)
        for path in ('sandbox/bin', 'sandbox/sdk2', 'sandbox/taskbox/worker'):
            peer = lib.node_by_output(graph, '$(B)/' + path + '/libpy3' + path.replace('/', '-') + '.global.a')
            self.assertIn(peer['uid'], binary['deps'])

    def test_python23_binary_keeps_python2_language_and_all_contexts(self):
        graph = make(fixture('SANDBOX_PY23_TASK'))
        contexts = [self.assert_context(graph, 'SANDBOX_PY23_TASK', tag) for tag in ('SB_TASK_BIN', 'PY2', 'PY3')]
        self.assertEqual(len({row[1]['outputs'][0] for row in contexts}), 3)
        self.assertEqual(len({row[2]['outputs'][0] for row in contexts}), 3)
        for consumer, _, _ in contexts:
            self.assertIn(consumer['uid'], graph['result'])

    def test_peer_selection_and_both_root_orders_preserve_resource_ownership(self):
        for macro in ('SANDBOX_PY3_TASK', 'SANDBOX_PY23_TASK'):
            for py2 in ((False, True) if macro == 'SANDBOX_PY23_TASK' else (False,)):
                files = fixture(macro)
                files['reader/ya.make'] = ('PY2_PROGRAM' if py2 else 'PY3_PROGRAM_BIN') + f'(reader)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nPEERDIR({TASK})\nEND()\n'
                tag = 'PY2' if py2 else 'PY3'
                for roots in (('reader',), ('reader', TASK), (TASK, 'reader')):
                    with self.subTest(macro=macro, py2=py2, roots=roots):
                        graph = make(files, *roots)
                        reader = lib.node_by_output(graph, '$(B)/reader/reader')
                        library, registry, _ = self.assert_context(graph, macro, tag)
                        self.assertIn(library['uid'], reader['deps'])
                        self.assertNotIn(registry['uid'], reader['deps'])
                        sdk = lib.node_by_output(graph, '$(B)/sandbox/sdk2/libpy' + ('' if py2 else '3') + 'sandbox-sdk2.global.a')
                        self.assertIn(sdk['uid'], reader['deps'])
                        if TASK in roots:
                            binary, _, _ = self.assert_context(graph, macro, 'SB_TASK_BIN')
                            self.assertNotIn(binary['uid'], reader['deps'])
                        else:
                            self.assertFalse(any(BUILD + '/acorns' in n['outputs'] for n in graph['graph']))

    def test_module_tag_condition_selects_python_sources_in_each_lexical_context(self):
        files = fixture()
        files['policy/python.inc'] += 'IF(MODULE_TAG == "SB_TASK_BIN")\nPY_SRCS(binary_only.py)\nELSE()\nPY_SRCS(library_only.py)\nENDIF()\n'
        files[TASK + '/binary_only.py'] = ''
        files[TASK + '/library_only.py'] = ''
        graph = make(files)
        for tag, source in (('SB_TASK_BIN', 'binary_only.py'), ('PY3', 'library_only.py')):
            consumer, _, _ = self.assert_context(graph, 'SANDBOX_PY3_TASK', tag)
            included = [path for n in resources(graph, consumer) for path in n['inputs'] if path.endswith(('_only.py', '_only.py.yapyc3'))]
            self.assertIn(SOURCE + '/' + source, included)
            self.assertNotIn(SOURCE + '/' + ('library_only.py' if tag == 'SB_TASK_BIN' else 'binary_only.py'), included)

    def test_final_source_tag_controls_resource_identity_without_changing_language(self):
        files = fixture()
        files[TASK + '/ya.make'] = files[TASK + '/ya.make'].replace('END()', 'ONLY_TAGS(SB_TASK_BIN)\nSET(MODULE_TAG CEDAR_ALIAS)\nEND()')
        graph = make(files)
        binary = lib.node_by_output(graph, BUILD + '/acorns')
        registry_key = 'SANDBOX_TASK_REGISTRY.' + TASK.replace('/', '.') + '=' + TASK.replace('/', '.')
        registry = next(n for n in resources(graph, binary) if registry_key in resource_kvs(n))
        self.assertEqual(registry['outputs'], [BUILD + '/objcopy_' + kv_only_hash([registry_key], 'CEDAR_ALIAS') + '.o'])
        # RUN_PROGRAM arguments bind before this later SET, while RESOURCE
        # reads the final module tag. Neither call is replayed.
        generated = lib.node_by_output(graph, BUILD + '/SB_TASK_BIN.dat')
        self.assertEqual(generated['cmds'][0]['cmd_args'][-3:], ['py3', 'no', 'yes'])
        lib.node_by_output(graph, BUILD + '/__init__.py.yapyc3')

    def test_generated_python_retains_actual_build_producer_and_main_output(self):
        files = fixture()
        files['policy/python.inc'] += ('RUN_PROGRAM(tools/ink --save anchor.py --secondary born.py IN seed.in OUT_NOAUTO born.py anchor.py)\n'
                                      'PY_SRCS(${ARCADIA_BUILD_ROOT}/' + TASK + '/born.py)\n')
        graph = make(files)
        producer = lib.node_by_output(graph, BUILD + '/anchor.py')
        self.assertEqual(producer['outputs'], [BUILD + '/anchor.py', BUILD + '/born.py'])
        bytecode = next(n for n in graph['graph'] if BUILD + '/born.py' in n['inputs'] and '--slow-py3cc' in n['cmds'][0]['cmd_args'])
        self.assertIn(producer['uid'], bytecode['deps'])
        self.assertIn(BUILD + '/anchor.py', bytecode['inputs'])
        self.assertNotIn(SOURCE + '/born.py', bytecode['inputs'])
        for tag in ('SB_TASK_BIN', 'PY3'):
            consumer, _, _ = self.assert_context(graph, 'SANDBOX_PY3_TASK', tag)
            owned = resources(graph, consumer)
            packed = [n for n in owned if BUILD + '/born.py' in n['inputs']]
            self.assertTrue(packed)
            for node in packed:
                self.assertIn(producer['uid'], node['deps'])
            # BUILD-token resources use the native raw-C++ packer, followed
            # by a real object producer, rather than llvm-objcopy directly.
            compiled = next(n for n in graph['graph'] if n['uid'] in consumer['deps']
                            and n['outputs'][0].endswith('_raw.auxcpp.o'))
            raw = lib.node_by_output(graph, compiled['outputs'][0][:-2])
            self.assertIn(raw['uid'], compiled['deps'])
            self.assertIn(bytecode['uid'], raw['deps'])
            self.assertIn(bytecode['outputs'][0], raw['inputs'])

    def test_ordinary_python3_program_bin_keeps_its_own_source_resources(self):
        files = fixture()
        files[TASK + '/ya.make'] = f'PY3_PROGRAM_BIN(acorns)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nPY_MAIN(grove.launch)\nPY_SRCS(__init__.py leaves.py)\nEND()\n'
        graph = make(files)
        binary = lib.node_by_output(graph, BUILD + '/acorns')
        owned = resources(graph, binary)
        main = next(n for n in owned if 'PY_MAIN=grove.launch:main' in resource_kvs(n))
        self.assertEqual(main['outputs'], [BUILD + '/objcopy_' + kv_only_hash(['PY_MAIN=grove.launch:main'], 'PY3') + '.o'])
        self.assertTrue(any(SOURCE + '/__init__.py' in n['inputs'] for n in owned))
        self.assertFalse(any(k.startswith('SANDBOX_TASK_REGISTRY.') for n in owned for k in resource_kvs(n)))

    def test_native_library_effective_tags_survive_the_shared_binding_stage(self):
        files = fixture()
        files[TASK + '/ya.make'] = (f'PY23_NATIVE_LIBRARY(acorns)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\n'
                                   'RUN_PROGRAM(tools/ink ${MODULE_TAG} IN seed.in OUT_NOAUTO ${MODULE_TAG}.dat)\n'
                                   'RESOURCE(${MODULE_TAG}.dat recorded/${MODULE_TAG})\n'
                                   'RESOURCE(- NATIVE_HINT=kept)\nEND()\n')
        files['reader/ya.make'] = f'PY2_PROGRAM(reader)\n{lib.BARE_MODULE}NO_PYTHON_INCLUDES()\nPEERDIR({TASK})\nEND()\n'
        graph = make(files, TASK, 'reader')
        for tag in ('PY2_NATIVE', 'PY3_NATIVE'):
            generated = lib.node_by_output(graph, BUILD + '/' + tag + '.dat')
            self.assertEqual(generated['cmds'][0]['cmd_args'][-1], tag)
            self.assertIn(SOURCE + '/seed.in', generated['inputs'])
            resource = lib.node_by_output(graph, BUILD + '/objcopy_' + kv_only_hash(['NATIVE_HINT=kept'], tag) + '.o')
            self.assertEqual(resource_kvs(resource), ['NATIVE_HINT=kept'])
            consumers = [n for n in graph['graph'] if resource['uid'] in n['deps'] and n['outputs'][0].endswith('.global.a')]
            self.assertEqual(len(consumers), 1)


if __name__ == '__main__':
    unittest.main(verbosity=2)
