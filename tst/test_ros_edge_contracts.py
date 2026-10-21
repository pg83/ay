import unittest

import lib
import test_python_proto
import test_ros


class RosEdgeContractsTest(unittest.TestCase):
    def test_python_messages_and_services_keep_generated_imports_and_inputs(self):
        files = test_python_proto.python_base()
        files.update(test_ros.ros_files())
        files.update({
            'app/ya.make': f'PY3_PROGRAM()\n{lib.BARE_MODULE}PEERDIR(msgs)\nEND()\n',
            'msgs/ya.make': f'ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ONLY_TAGS(PY3_ROS)\nROS_DIR(msgs)\nSET(ROS_PURE_PY yes)\nROS_SRCS(msg/Child.msg msg/Parent.msg srv/Query.srv)\nEND()\n',
            'msgs/msg/Child.msg': 'int32 value\n',
            'msgs/msg/Parent.msg': 'Child child\n',
            'msgs/srv/Query.srv': 'Child request\n---\nParent response\n',
            'sdg/sdc/third_party/ros/genpy/yamakes/pylib/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}END()\n',
        })
        lib.tool_program(files, 'sdg/tier0/tools/generate_proxy_import_file', 'generate_proxy_import_file')
        graph = lib.make(files, 'app')
        parent = lib.node_by_output(graph, '$(B)/msgs/msg/_Parent.py')
        self.assertIn('$(S)/msgs/msg/Child.msg', parent['inputs'])
        service = lib.node_by_output(graph, '$(B)/msgs/srv/_Query.py')
        self.assertIn('$(S)/msgs/msg/Child.msg', service['inputs'])
        self.assertIn('$(S)/msgs/msg/Parent.msg', service['inputs'])
        for kind, names in (('msg', ('Child', 'Parent')), ('srv', ('Query',))):
            init = lib.node_by_output(graph, f'$(B)/msgs/{kind}/__init__.py')
            args = init['cmds'][0]['cmd_args']
            for name in names:
                self.assertIn('--from-import=._' + name, args)
            self.assertNotIn('--from-import=..cpp_msg', args)

    def test_unqualified_ros_names_and_header_alias_resolve_from_peers(self):
        files = test_ros.ros_files()
        files.update({
            'headers/ya.make': f'ROS_LIBRARY(std_msgs)\n{lib.BARE_MODULE}ONLY_TAGS(CPP_ROS)\nROS_DIR(headers)\nROS_SRCS(msg/Header.msg)\nEND()\n',
            'headers/msg/Header.msg': 'uint64 stamp\n',
            'msgs/ya.make': f'ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ONLY_TAGS(CPP_ROS)\nROS_DIR(msgs)\nPEERDIR(headers)\nROS_SRCS(msg/Child.msg msg/Parent.msg)\nEND()\n',
            'msgs/msg/Child.msg': 'int32 value\n',
            'msgs/msg/Parent.msg': 'Header header\nChild child\n',
        })
        graph = lib.make(files, 'msgs')
        parent = lib.node_by_output(graph, '$(B)/msgs/Parent.h')
        self.assertIn('$(S)/headers/msg/Header.msg', parent['inputs'])
        self.assertIn('$(S)/msgs/msg/Child.msg', parent['inputs'])

    def test_unresolved_ros_names_do_not_create_phantom_inputs(self):
        for name in ('Absent', 'other/Absent'):
            with self.subTest(name=name):
                files = test_ros.ros_files()
                files.update({'msgs/ya.make': f'ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ONLY_TAGS(CPP_ROS)\nROS_DIR(msgs)\nROS_SRCS(msg/Parent.msg)\nEND()\n',
                              'msgs/msg/Parent.msg': name + ' value\n'})
                graph = lib.make(files, 'msgs')
                parent = lib.node_by_output(graph, '$(B)/msgs/Parent.h')
                self.assertEqual(parent['inputs'], ['$(B)/' + test_ros.GENERATOR + '/generator', '$(S)/msgs/msg/Parent.msg'])

    def test_ros_macros_and_copied_messages_in_native_library(self):
        files = {'native/ya.make': f'LIBRARY()\n{lib.BARE_MODULE}ROS_DIR(ignored)\nROS_SRCS(ignored.msg)\nCOPY_FILE(AUTO input.msg generated.msg)\nSRCS(main.cpp input.msg)\nEND()\n',
                 'native/input.msg': 'int32 value\n', 'native/main.cpp': ''}
        graph = lib.make(files, 'native')
        lib.node_by_output(graph, '$(B)/native/libnative.a')
        self.assertFalse(any(n['kv']['p'] == 'RS' for n in graph['graph']))

    def test_extracted_ros_message_is_generated_and_compiled(self):
        files = test_ros.ros_files()
        files.update({'msgs/ya.make': f'ROS_LIBRARY(robot)\n{lib.BARE_MODULE}ONLY_TAGS(CPP_ROS)\nROS_DIR(msgs)\nFROM_ARCHIVE(input.tar OUT Generated.msg)\nEND()\n',
                      'msgs/input.tar': ''})
        graph = lib.make(files, 'msgs')
        copied = lib.node_by_output(graph, '$(B)/msgs/Generated.msg')
        generated = lib.node_by_output(graph, '$(B)/msgs/Generated.h')
        self.assertIn(copied['uid'], generated['deps'])
        lib.node_by_output(graph, '$(B)/msgs/Generated.cc.o')


if __name__ == '__main__':
    unittest.main(verbosity=2)
