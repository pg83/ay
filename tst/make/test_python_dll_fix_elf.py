import unittest

from tst import lib
from tst.make.test_python_any_module import fixture


class PythonDllFixElfTest(unittest.TestCase):
    def test_flags_follow_selected_module_and_effective_opt_out(self):
        for kind in ('PY2MODULE', 'PY_ANY_MODULE', 'DLL'):
            for version, setting in (('', ''), (' 3 2 PREFIX lib', ''), ('', 'SET(_NO_FIX_ELF yes)\n'), ('', 'SET(_NO_FIX_ELF yes)\nSET(_NO_FIX_ELF no)\n')):
                with self.subTest(kind=kind, version=version, setting=setting):
                    files, _ = fixture({})
                    files.update({
                        'grove/ya.make': kind + '(cedar' + version + ')\n' + lib.BARE_MODULE + 'SRCS(main.cpp)\n' + setting + 'END()\n',
                        'grove/main.cpp': 'int value;\n',
                    })
                    graph = lib.make(files, 'grove', '--sandboxing', '--target-platform', 'default-linux-x86_64')
                    output = 'libcedar.so' if kind == 'DLL' or version else 'cedar.so'
                    node = lib.node_by_output(graph, '$(B)/grove/' + output)
                    command = next(c['cmd_args'] for c in node['cmds'] if any(a.endswith('/link_dyn_lib.py') for a in c['cmd_args']))
                    expected = not (version and kind != 'DLL') and setting != 'SET(_NO_FIX_ELF yes)\n'
                    tool = '$(B)/tools/fix_elf/fix_elf'
                    self.assertEqual('--fix-elf' in command, expected)
                    self.assertEqual(tool in node['inputs'], expected)
                    if expected:
                        producer = lib.node_by_output(graph, tool)
                        self.assertIn(producer['uid'], node['deps'])
                        self.assertEqual(command[command.index('--fix-elf') + 1], tool)
                    else:
                        self.assertFalse(any(tool in n['outputs'] for n in graph['graph']))
                    self.assertIn('$(B)/grove/main.cpp.pic.o', node['inputs'])
                    compiled = lib.node_by_output(graph, '$(B)/grove/main.cpp.pic.o')
                    self.assertIn(compiled['uid'], node['deps'])
                    self.assertIn('-Wl,-soname,' + output + ('.3' if version else ''), command)
                    self.assertEqual(node['outputs'], ['$(B)/grove/' + output] + (['$(B)/grove/' + output + '.3'] if version else []))
                    self.assertIn(node['uid'], graph['result'])


if __name__ == '__main__':
    unittest.main()
