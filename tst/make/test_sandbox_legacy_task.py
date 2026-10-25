"""Legacy Sandbox tasks retain independent Python 2 binary and library contexts."""
import unittest

from tst import lib
from tst.make.test_python_srcs import python_base
from tst.make.test_python2 import python2_base, keys as py_resource_keys, section, objcopy_nodes

TASK='orchard/jobs/MeasureCedar'
def fixture(selection='',name='cedar'):
 files=python_base();files.update(python2_base())
 files[TASK+'/ya.make']=(f'SANDBOX_TASK({name})\n'+lib.BARE_MODULE+'NO_PYTHON_INCLUDES()\n'+selection+'\n'
 'IF(MODULE_TAG == "SB_TASK_BIN")\nCFLAGS(-DSANDBOX_BINARY_CONTEXT)\nELSE()\nCFLAGS(-DSANDBOX_LIBRARY_CONTEXT)\nENDIF()\n'
 'PY_SRCS(__init__.py)\nSRCS(part.cpp)\n'
 'RUN_PROGRAM(tools/marker ${MODULE_KIND} ${MODULE_TAG} ${_PYTHON_VER} ${PYTHON_TYPE_FOR_CYTHON} OUT_NOAUTO ${MODULE_TAG}.stamp)\n'
 'RESOURCE(${MODULE_TAG}.stamp stamp/${MODULE_TAG} templates/report.tpl reports/template)\nEND()\n')
 files[TASK+'/__init__.py']='value = 1\n';files[TASK+'/part.cpp']='int part;\n';files[TASK+'/templates/report.tpl']='report fixture\n'
 for peer in ['sandbox/sdk2','sandbox/bin','sandbox/taskbox/worker']:
  files[peer+'/ya.make']='PY23_LIBRARY()\n'+lib.BARE_MODULE+'NO_PYTHON_INCLUDES()\nPY_SRCS(side.py)\nEND()\n';files[peer+'/side.py']='value = 2\n'
 lib.tool_program(files,'tools/marker','marker')
 files['runner/ya.make']='PY2_PROGRAM(runner)\n'+lib.BARE_MODULE+'NO_PYTHON_INCLUDES()\nNO_CHECK_IMPORTS()\nPEERDIR('+TASK+')\nEND()\n'
 files['checks/ya.make']='EXECTEST()\nRUN(true)\nDEPENDS('+TASK+')\nEND()\n'
 files['orchard/ya.make']='LIBRARY()\n'+lib.BARE_MODULE+'END()\nRECURSE(jobs/MeasureCedar)\n'
 return files


def resource_keys(node):
    return py_resource_keys(node) if "--keys" in node["cmds"][0]["cmd_args"] else []


def task_node(graph, tag, name='cedar'):
    output = name if tag == 'SB_TASK_BIN' else 'libpy' + name + '.global.a'
    return lib.node_by_output(graph, '$(B)/' + TASK + '/' + output)


class SandboxLegacyTaskTest(unittest.TestCase):
    def make(self, files, target=TASK, *args, **kwargs):
        graph = lib.make(files, target, '--strip-packages-from-results', *args, **kwargs)
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(set(graph['result']) <= ids)
        self.assertTrue(all(set(node['deps']) <= ids for node in graph['graph']))
        return graph

    def assert_context(self, graph, tag):
        output = task_node(graph, tag)
        resources = [n for n in objcopy_nodes(graph, TASK) if n['uid'] in output['deps']]
        registry = 'SANDBOX_TASK_REGISTRY.' + TASK.replace('/', '.') + '=' + TASK.replace('/', '.')
        self.assertTrue(any(registry in section(n, '--kvs') for n in resources if '--kvs' in n['cmds'][0]['cmd_args']))
        generated = lib.node_by_output(graph, '$(B)/' + TASK + '/' + tag + '.stamp')
        self.assertEqual(generated['cmds'][0]['cmd_args'], [
            '$(B)/tools/marker/marker', 'SANDBOX_TASK', tag, 'py2', 'PY2'])
        payload = next(n for n in resources if 'stamp/' + tag in resource_keys(n))
        self.assertIn(generated['uid'], payload['deps'])
        self.assertIn(generated['outputs'][0], payload['inputs'])
        template = next(n for n in resources if 'reports/template' in resource_keys(n))
        self.assertIn('$(S)/' + TASK + '/templates/report.tpl', template['inputs'])
        bytecodes = [n for n in graph['graph'] if '$(B)/' + TASK + '/__init__.py.yapyc' in n['outputs']
                     and any(n['uid'] in r['deps'] for r in resources)]
        self.assertEqual(len(bytecodes), 1)
        bytecode = bytecodes[0]
        self.assertIn('$(S)/' + TASK + '/__init__.py', bytecode['inputs'])
        self.assertTrue(any(bytecode['uid'] in n['deps'] for n in resources))
        main = [value for n in resources if '--kvs' in n['cmds'][0]['cmd_args']
                for value in section(n, '--kvs') if value.startswith('PY_MAIN=')]
        self.assertEqual(main, ['PY_MAIN=sandbox.taskbox.binary:main'] if tag == 'SB_TASK_BIN' else [])
        consumer = output if tag == 'SB_TASK_BIN' else lib.node_by_output(graph, '$(B)/' + TASK + '/libpycedar.a')
        compiled = [n for n in graph['graph'] if n['uid'] in consumer['deps']
                    and '$(B)/' + TASK + '/part.cpp.o' in n['outputs']]
        self.assertEqual(len(compiled), 1)
        self.assertIn('-DSANDBOX_' + ('BINARY' if tag == 'SB_TASK_BIN' else 'LIBRARY') + '_CONTEXT',
                      compiled[0]['cmds'][0]['cmd_args'])
        return output

    def test_direct_task_keeps_two_python2_contexts_and_all_producers(self):
        graph = self.make(fixture())
        for tag in ('SB_TASK_BIN', 'PY2'):
            self.assertIn(self.assert_context(graph, tag)['uid'], graph['result'])
        self.assertFalse(any(out.endswith(('.yapyc3', '.py2.o', '.py3.o'))
                             for n in graph['graph'] if any(out.startswith('$(B)/' + TASK + '/') for out in n['outputs'])
                             for out in n['outputs']))
        binary = task_node(graph, 'SB_TASK_BIN')
        for peer in ('sandbox/bin', 'sandbox/sdk2', 'sandbox/taskbox/worker'):
            archive = lib.node_by_output(graph, '$(B)/' + peer + '/libpy' + peer.replace('/', '-') + '.global.a')
            self.assertIn(archive['uid'], binary['deps'])

    def test_python_peer_keeps_only_library_context_and_sdk(self):
        graph = self.make(fixture(), 'runner')
        runner = lib.node_by_output(graph, '$(B)/runner/runner')
        library = self.assert_context(graph, 'PY2')
        self.assertIn(library['uid'], runner['deps'])
        outputs = {out for n in graph['graph'] for out in n['outputs']}
        self.assertNotIn('$(B)/' + TASK + '/cedar', outputs)
        self.assertFalse(any(out.startswith(('$(B)/sandbox/bin/', '$(B)/sandbox/taskbox/worker/')) for out in outputs))

    def test_configuration_selectors_and_runtime_depends_keep_distinct_roles(self):
        for selection, tags in (('', {'SB_TASK_BIN', 'PY2'}), ('ONLY_TAGS(PY2)', {'PY2'}),
                                ('ONLY_TAGS(SB_TASK_BIN)', {'SB_TASK_BIN'}), ('EXCLUDE_TAGS(SB_TASK_BIN)', {'PY2'})):
            for target in (TASK, 'checks'):
                with self.subTest(selection=selection, target=target):
                    graph = self.make(fixture(selection), target, '-t')
                    outputs = {out for n in graph['graph'] for out in n['outputs']}
                    for tag, name in (('SB_TASK_BIN', 'cedar'), ('PY2', 'libpycedar.global.a')):
                        if target == TASK or tag == 'SB_TASK_BIN' or tag not in tags:
                            self.assertEqual('$(B)/' + TASK + '/' + name in outputs, tag in tags)
                        if tag in tags and (target == TASK or tag == 'SB_TASK_BIN'):
                            node = task_node(graph, tag)
                            if target == TASK:
                                self.assertIn(node['uid'], graph['result'])
                            else:
                                suite = lib.node_by_output(graph, '$(B)/checks/test-results/exectest/meta.json')
                                self.assertIn(node['uid'], suite['deps'])

    def test_default_names_and_recursion_preserve_binary_and_library(self):
        graph = self.make(fixture(name=''), 'orchard')
        for name in ('MeasureCedar', 'libpyorchard-jobs-MeasureCedar.global.a'):
            self.assertIn(lib.node_by_output(graph, '$(B)/' + TASK + '/' + name)['uid'], graph['result'])

    def test_deprecation_is_only_in_binary_and_fail_py2_is_an_error(self):
        for opensource, present in ((True, False), (False, True)):
            with self.subTest(opensource=opensource):
                graph = self.make(fixture('ONLY_TAGS(SB_TASK_BIN)'), opensource=opensource)
                binary = task_node(graph, 'SB_TASK_BIN')
                path = '$(B)/build/rules/py2_deprecation/libbuild-rules-py2_deprecation.a'
                nodes = [n for n in graph['graph'] if path in n['outputs']]
                self.assertEqual(len(nodes), int(present))
                if present:
                    self.assertIn(nodes[0]['uid'], binary['deps'])
        graph = self.make(fixture('ONLY_TAGS(PY2)'), TASK, '-DFAIL_PY2=yes')
        self.assert_context(graph, 'PY2')
        for opensource in (True, False):
            with self.subTest(source_clears_fail=True, opensource=opensource):
                graph = self.make(fixture('SET(FAIL_PY2 no)'), TASK, '-DFAIL_PY2=yes', opensource=opensource)
                binary = self.assert_context(graph, 'SB_TASK_BIN')
                path = '$(B)/build/rules/py2_deprecation/libbuild-rules-py2_deprecation.a'
                nodes = [n for n in graph['graph'] if path in n['outputs']]
                self.assertEqual(len(nodes), int(not opensource))
                if nodes:
                    self.assertIn(nodes[0]['uid'], binary['deps'])
        result = lib.make_process(fixture('ONLY_TAGS(SB_TASK_BIN)'), TASK, '-DFAIL_PY2=yes')
        self.assertEqual(result.returncode, 1)
        self.assertIn('You are using deprecated Python2-only code (SANDBOX_TASK)', result.stderr)
        result = lib.make_process(fixture('SET(FAIL_PY2 yes)'), TASK)
        self.assertEqual(result.returncode, 1)
        self.assertIn('You are using deprecated Python2-only code (SANDBOX_TASK)', result.stderr)

    def test_non_python2_peers_are_rejected_and_explicit_tags_are_respected(self):
        for kind in ('PY3_PROGRAM_BIN(client)', 'LIBRARY()'):
            with self.subTest(kind=kind):
                files = fixture()
                files['consumer/ya.make'] = kind + '\n' + lib.BARE_MODULE + 'NO_PYTHON_INCLUDES()\nSRCS(client.cpp)\nPEERDIR(' + TASK + ')\nEND()\n'
                files['consumer/client.cpp'] = 'int client;\n'
                result = lib.make_process(files, 'consumer')
                self.assertEqual(result.returncode, 1)
                self.assertIn('prohibited by incompatible tags', result.stderr)
        for tags in ('PY3', '""'):
            with self.subTest(tags=tags):
                files['consumer/ya.make'] = 'PROGRAM(client)\n' + lib.BARE_MODULE + 'SET(PEERDIR_TAGS ' + tags + ')\nSRCS(client.cpp)\nPEERDIR(' + TASK + ')\nEND()\n'
                result = lib.make_process(files, 'consumer')
                self.assertEqual(result.returncode, 1)
                self.assertIn('prohibited by incompatible tags', result.stderr)
        files['consumer/ya.make'] = 'PROGRAM(client)\n' + lib.BARE_MODULE + 'SET(PEERDIR_TAGS PY2)\nSRCS(client.cpp)\nPEERDIR(' + TASK + ')\nEND()\n'
        graph = self.make(files, 'consumer')
        binary = lib.node_by_output(graph, '$(B)/consumer/client')
        self.assertIn(task_node(graph, 'PY2')['uid'], binary['deps'])

    def test_package_accepts_library_tag_but_union_and_empty_override_reject_it(self):
        files = fixture()
        files['bundle/ya.make'] = 'PACKAGE(cedar_bundle)\nPEERDIR(' + TASK + ')\nEND()\n'
        graph = lib.make(files, 'bundle')
        ids = {node['uid'] for node in graph['graph']}
        self.assertTrue(graph['result'])
        self.assertTrue(set(graph['result']) <= ids)
        self.assertTrue(all(set(node['deps']) <= ids for node in graph['graph']))
        package = lib.node_by_output(graph, '$(B)/bundle/cedar_bundle.final.pkg.fake')
        library = self.assert_context(graph, 'PY2')
        self.assertIn(library['uid'], package['deps'])
        self.assertIn('$(B)/bundle/' + TASK + '/libpycedar.global.a', package['outputs'])
        self.assertFalse(any('$(B)/' + TASK + '/cedar' in node['outputs'] for node in graph['graph']))
        for declaration in ('UNION()', 'PACKAGE(cedar_bundle)\nSET(PEERDIR_TAGS "")'):
            with self.subTest(declaration=declaration):
                files['bundle/ya.make'] = declaration + '\nPEERDIR(' + TASK + ')\nEND()\n'
                result = lib.make_process(files, 'bundle')
                self.assertEqual(result.returncode, 1)
                self.assertIn('prohibited by incompatible tags', result.stderr)


if __name__ == '__main__':
    unittest.main(verbosity=2)
