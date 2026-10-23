"""Sandbox Python task declarations retain both upstream module contexts."""
import unittest

import lib
from test_python_srcs import python_base, objcopy_nodes, kvs


def fixture():
    files = python_base()
    files.update({
        'work/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\nRECURSE(jobs/InspectDelta)\n',
        'work/jobs/InspectDelta/ya.make': (
            f'SANDBOX_PY3_TASK()\n{lib.BARE_MODULE}PY_SRCS(__init__.py)\n'
            'PEERDIR(packages/calendar)\nEND()\n'
        ),
        'work/jobs/InspectDelta/__init__.py': '',
        'packages/calendar/ya.make': f'PY3_LIBRARY()\n{lib.BARE_MODULE}PY_SRCS(days.py)\nEND()\n',
        'packages/calendar/days.py': '',
        'sandbox/sdk2/ya.make': f'PY3_LIBRARY()\n{lib.BARE_MODULE}PY_SRCS(api.py)\nEND()\n',
        'sandbox/sdk2/api.py': '',
        'sandbox/bin/ya.make': f'PY3_LIBRARY()\n{lib.BARE_MODULE}PY_SRCS(driver.py)\nEND()\n',
        'sandbox/bin/driver.py': '',
        'sandbox/taskbox/worker/ya.make': f'PY3_LIBRARY()\n{lib.BARE_MODULE}PY_SRCS(worker.py)\nEND()\n',
        'sandbox/taskbox/worker/worker.py': '',
    })
    return files


class SandboxPythonTaskTest(unittest.TestCase):
    def test_recursed_task_is_final_without_test_mode(self):
        graph = lib.make(fixture(), 'work', '--strip-packages-from-results')
        binary = lib.node_by_output(graph, '$(B)/work/jobs/InspectDelta/InspectDelta')
        library = lib.node_by_output(graph, '$(B)/work/jobs/InspectDelta/libpy3work-jobs-InspectDelta.global.a')
        self.assertIn(binary['uid'], graph['result'])
        self.assertIn(library['uid'], graph['result'])

    def test_recursed_task_materializes_python_peer_closure(self):
        graph = lib.make(fixture(), 'work', '-t', '--strip-packages-from-results',
                         '--add-modules-to-results')
        task = lib.node_by_output(graph, '$(B)/work/jobs/InspectDelta/InspectDelta')
        self.assertIn(task['uid'], graph['result'])
        for directory, name in (('packages/calendar', 'libpy3packages-calendar.global.a'),
                                ('sandbox/sdk2', 'libpy3sandbox-sdk2.global.a'),
                                ('sandbox/bin', 'libpy3sandbox-bin.global.a'),
                                ('sandbox/taskbox/worker', 'libpy3sandbox-taskbox-worker.global.a')):
            archive = lib.node_by_output(graph, '$(B)/' + directory + '/' + name)
            self.assertIn(archive['uid'], task['deps'])
        lib.node_by_output(graph, '$(B)/packages/calendar/days.py.yapyc3')
        library = lib.node_by_output(graph, '$(B)/work/jobs/InspectDelta/libpy3work-jobs-InspectDelta.global.a')
        self.assertIn(library['uid'], graph['result'])
        check = lib.node_by_output(graph, '$(B)/work/jobs/InspectDelta/test-results/import_test/meta.json')
        self.assertIn(task['uid'], check['deps'])
        resources = objcopy_nodes(graph, 'work/jobs/InspectDelta')
        registries = [n for n in resources if
                      'SANDBOX_TASK_REGISTRY.work.jobs.InspectDelta=work.jobs.InspectDelta' in kvs(n)]
        self.assertEqual(len(registries), 2)
        registry = next(n for n in registries if n['uid'] in task['deps'])
        library_registry = next(n for n in registries if n['uid'] in library['deps'])
        self.assertNotEqual(registry['uid'], library_registry['uid'])
        main = next(n for n in resources if 'PY_MAIN=sandbox.taskbox.binary:main' in kvs(n))
        exclusions = next(n for n in resources if any(
            value.startswith('py/no_check_imports/') and value.endswith('=Noneapi.*kernel.*library.*sky*api.*kernel.*library.*sky* api.* kernel.* library.* sky*')
            for value in kvs(n)))
        self.assertNotIn(registry['uid'], library['deps'])
        self.assertNotIn(library_registry['uid'], task['deps'])
        for node in (registry, main, exclusions):
            self.assertIn(node['uid'], task['deps'])
        self.assertNotIn(main['uid'], library['deps'])
        self.assertNotIn(exclusions['uid'], library['deps'])

    def test_binary_appends_masks_to_inherited_value_after_sanitizer_reset(self):
        for flags, prefix in ((['-DNO_CHECK_IMPORTS_FOR_VALUE=custom.*'], 'custom.*'),
                              (['-DSANITIZER_TYPE=address'], 'None'),
                              (['-DNO_CHECK_IMPORTS_FOR_VALUE='], '')):
            with self.subTest(flags=flags):
                graph = lib.make(fixture(), 'work/jobs/InspectDelta', *flags)
                binary = lib.node_by_output(graph, '$(B)/work/jobs/InspectDelta/InspectDelta')
                masks = [value.split('=', 1)[1]
                         for node in objcopy_nodes(graph, 'work/jobs/InspectDelta') if node['uid'] in binary['deps'] for value in kvs(node)
                         if value.startswith('py/no_check_imports/')]
                self.assertEqual(masks, [prefix + 'api.*kernel.*library.*sky*api.*kernel.*library.*sky* api.* kernel.* library.* sky*'])
                library = lib.node_by_output(graph, '$(B)/work/jobs/InspectDelta/libpy3work-jobs-InspectDelta.global.a')
                own = [value.split('=', 1)[1] for node in objcopy_nodes(graph, 'work/jobs/InspectDelta')
                       if node['uid'] in library['deps'] for value in kvs(node) if value.startswith('py/no_check_imports/')]
                self.assertEqual(own, ['custom.*'] if prefix == 'custom.*' else [])

    def test_python_peer_selects_library_and_sdk_without_task_binary(self):
        files = fixture()
        files['runner/ya.make'] = (
            f'PY3_PROGRAM_BIN(runner)\n{lib.BARE_MODULE}PEERDIR(work/jobs/InspectDelta)\nEND()\n'
        )
        graph = lib.make(files, 'runner', '--strip-packages-from-results')
        runner = lib.node_by_output(graph, '$(B)/runner/runner')
        for output in ('$(B)/work/jobs/InspectDelta/libpy3work-jobs-InspectDelta.global.a',
                       '$(B)/sandbox/sdk2/libpy3sandbox-sdk2.global.a'):
            self.assertIn(lib.node_by_output(graph, output)['uid'], runner['deps'])
        self.assertFalse(any(output == '$(B)/work/jobs/InspectDelta/InspectDelta'
                             or output.startswith(('$(B)/sandbox/bin/', '$(B)/sandbox/taskbox/worker/'))
                             for n in graph['graph'] for output in n['outputs']))
        self.assertFalse(any(value.startswith(('PY_MAIN=', 'py/no_check_imports/'))
                             for n in objcopy_nodes(graph, 'work/jobs/InspectDelta') for value in kvs(n)))

    def test_depends_preserves_binary_and_library_results(self):
        for selection, binary, library in (('', True, True), ('ONLY_TAGS(SB_TASK_BIN)', True, False),
                                            ('ONLY_TAGS(PY3)', False, True)):
            with self.subTest(selection=selection):
                files = fixture()
                files['checks/ya.make'] = 'EXECTEST()\nRUN(true)\nDEPENDS(work/jobs/InspectDelta)\nEND()\n'
                files['work/jobs/InspectDelta/ya.make'] = files['work/jobs/InspectDelta/ya.make'].replace(
                    'END()', selection + '\nEND()')
                graph = lib.make(files, 'checks', '-t', '--strip-packages-from-results')
                suite = lib.node_by_output(graph, '$(B)/checks/test-results/exectest/meta.json')
                for output, present in (('InspectDelta', binary),
                                        ('libpy3work-jobs-InspectDelta.global.a', library)):
                    selected = [n for n in graph['graph']
                                if '$(B)/work/jobs/InspectDelta/' + output in n['outputs']]
                    self.assertEqual(len(selected), int(present))
                    if present:
                        self.assertIn(selected[0]['uid'], graph['result'])
                        self.assertEqual(selected[0]['uid'] in suite['deps'], output == 'InspectDelta')

    def test_direct_contexts_keep_configuration_tag_and_python_language_separate(self):
        for selection, binary, library in (('', True, True), ('ONLY_TAGS(SB_TASK_BIN)', True, False),
                                            ('EXCLUDE_TAGS(SB_TASK_BIN)', False, True)):
            with self.subTest(selection=selection):
                files = fixture()
                files['work/jobs/InspectDelta/ya.make'] = (
                    f'SANDBOX_PY3_TASK(named)\n{lib.BARE_MODULE}{selection}\n'
                    'IF(PYTHON3)\n'
                    'IF(MODULE_TAG == "SB_TASK_BIN")\nPY_SRCS(binary.py)\n'
                    'ELSE()\nPY_SRCS(library.py)\nENDIF()\nENDIF()\nEND()\n'
                )
                files['work/jobs/InspectDelta/binary.py'] = ''
                files['work/jobs/InspectDelta/library.py'] = ''
                graph = lib.make(files, 'work/jobs/InspectDelta', '--strip-packages-from-results')
                outputs = {o for n in graph['graph'] for o in n['outputs']}
                prefix = '$(B)/work/jobs/InspectDelta/'
                self.assertEqual(prefix + 'named' in outputs, binary)
                self.assertEqual(prefix + 'libpy3named.global.a' in outputs, library)
                self.assertEqual(prefix + 'binary.py.yapyc3' in outputs, binary)
                self.assertEqual(prefix + 'library.py.yapyc3' in outputs, library)


if __name__ == '__main__':
    unittest.main(verbosity=2)
