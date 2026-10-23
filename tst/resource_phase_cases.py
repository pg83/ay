"""Exact native call/readiness controls from T117 remaining-source-phases/native.json."""

CASES = [{'name': 'copy-py3-late',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'ONLY_TAGS(PY3)\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'COPY_FILE(seed.dat data.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/seed.dat': 'seed'},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o',
              '$(B)/mod/objcopy_90759244cca0f899f325587f8a.o']},
 {'name': 'copy-py3-early',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'ONLY_TAGS(PY3)\n'
                           'PY_SRCS(a.py)\n'
                           'COPY_FILE(seed.dat data.dat)\n'
                           'RESOURCE(data.dat payload)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/seed.dat': 'seed'},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'copy-both-late',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'COPY_FILE(seed.dat data.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/seed.dat': 'seed'},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'copy-both-early',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'COPY_FILE(seed.dat data.dat)\n'
                           'RESOURCE(data.dat payload)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/seed.dat': 'seed'},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'program-py3-late',
  'files': {'tools/gen/ya.make': 'PROGRAM(gen)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n',
            'tools/gen/main.cpp': 'int main(){return 0;}\n',
            'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'ONLY_TAGS(PY3)\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'RUN_PROGRAM(tools/gen OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o',
              '$(B)/mod/objcopy_90759244cca0f899f325587f8a.o']},
 {'name': 'program-py3-early',
  'files': {'tools/gen/ya.make': 'PROGRAM(gen)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n',
            'tools/gen/main.cpp': 'int main(){return 0;}\n',
            'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'ONLY_TAGS(PY3)\n'
                           'PY_SRCS(a.py)\n'
                           'RUN_PROGRAM(tools/gen OUT_NOAUTO data.dat)\n'
                           'RESOURCE(data.dat payload)\n'
                           'END()\n',
            'mod/a.py': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'program-both-late',
  'files': {'tools/gen/ya.make': 'PROGRAM(gen)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n',
            'tools/gen/main.cpp': 'int main(){return 0;}\n',
            'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'RUN_PROGRAM(tools/gen OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'program-both-early',
  'files': {'tools/gen/ya.make': 'PROGRAM(gen)\nNO_LIBC()\nNO_RUNTIME()\nNO_UTIL()\nSRCS(main.cpp)\nEND()\n',
            'tools/gen/main.cpp': 'int main(){return 0;}\n',
            'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RUN_PROGRAM(tools/gen OUT_NOAUTO data.dat)\n'
                           'RESOURCE(data.dat payload)\n'
                           'END()\n',
            'mod/a.py': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'python-py3-late',
  'files': {'mod/gen.py': '',
            'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'ONLY_TAGS(PY3)\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'RUN_PYTHON3(gen.py OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o',
              '$(B)/mod/objcopy_90759244cca0f899f325587f8a.o']},
 {'name': 'python-py3-early',
  'files': {'mod/gen.py': '',
            'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'ONLY_TAGS(PY3)\n'
                           'PY_SRCS(a.py)\n'
                           'RUN_PYTHON3(gen.py OUT_NOAUTO data.dat)\n'
                           'RESOURCE(data.dat payload)\n'
                           'END()\n',
            'mod/a.py': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'python-both-late',
  'files': {'mod/gen.py': '',
            'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'RUN_PYTHON3(gen.py OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'python-both-early',
  'files': {'mod/gen.py': '',
            'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RUN_PYTHON3(gen.py OUT_NOAUTO data.dat)\n'
                           'RESOURCE(data.dat payload)\n'
                           'END()\n',
            'mod/a.py': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'copy-chain-py3',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'ONLY_TAGS(PY3)\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'COPY_FILE(mid.dat data.dat)\n'
                           'COPY_FILE(seed.dat mid.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/seed.dat': 'seed'},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o',
              '$(B)/mod/objcopy_90759244cca0f899f325587f8a.o']},
 {'name': 'copy-chain-both',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_LIBC()\n'
                           'NO_RUNTIME()\n'
                           'NO_UTIL()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'COPY_FILE(mid.dat data.dat)\n'
                           'COPY_FILE(seed.dat mid.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/seed.dat': 'seed'},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'library-ext-before',
  'files': {'mod/ya.make': 'LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'SRCS(GLOBAL stone.cpp)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libmod.global.a',
  'members': ['$(B)/mod/objcopy_9641e8508761fe983b78999b7d.o',
              '$(B)/mod/objcopy_6cafab3a27f16ce5230680c6d3.o',
              '$(B)/mod/stone.cpp.o']},
 {'name': 'library-ext-after',
  'files': {'mod/ya.make': 'LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'SRCS(GLOBAL stone.cpp)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libmod.global.a',
  'members': ['$(B)/mod/objcopy_6cafab3a27f16ce5230680c6d3.o',
              '$(B)/mod/objcopy_9641e8508761fe983b78999b7d.o',
              '$(B)/mod/stone.cpp.o']},
 {'name': 'library-ext-pending',
  'files': {'mod/ya.make': 'LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'SRCS(GLOBAL stone.cpp)\n'
                           'RESOURCE(data.dat payload)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libmod.global.a',
  'members': ['$(B)/mod/objcopy_9641e8508761fe983b78999b7d.o',
              '$(B)/mod/stone.cpp.o',
              '$(B)/mod/objcopy_d264abe12bf6e257ae05b6de8e.o']},
 {'name': 'library-dirs-before',
  'files': {'mod/ya.make': 'LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'SRCS(GLOBAL stone.cpp)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libmod.global.a',
  'members': ['$(B)/mod/objcopy_b3e4629c558055ec569bb9402f.o',
              '$(B)/mod/objcopy_6cafab3a27f16ce5230680c6d3.o',
              '$(B)/mod/stone.cpp.o']},
 {'name': 'library-dirs-after',
  'files': {'mod/ya.make': 'LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'SRCS(GLOBAL stone.cpp)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libmod.global.a',
  'members': ['$(B)/mod/objcopy_6cafab3a27f16ce5230680c6d3.o',
              '$(B)/mod/objcopy_b3e4629c558055ec569bb9402f.o',
              '$(B)/mod/stone.cpp.o']},
 {'name': 'library-dirs-pending',
  'files': {'mod/ya.make': 'LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'SRCS(GLOBAL stone.cpp)\n'
                           'RESOURCE(data.dat payload)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libmod.global.a',
  'members': ['$(B)/mod/objcopy_b3e4629c558055ec569bb9402f.o',
              '$(B)/mod/stone.cpp.o',
              '$(B)/mod/objcopy_d264abe12bf6e257ae05b6de8e.o']},
 {'name': 'py3_library-ext-before',
  'files': {'mod/ya.make': 'PY3_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_b9b624274d4aee47f38a84e814.o',
              '$(B)/mod/objcopy_a714dca11e9d28e308c7a07c7a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py3_library-ext-after',
  'files': {'mod/ya.make': 'PY3_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_a714dca11e9d28e308c7a07c7a.o',
              '$(B)/mod/objcopy_b9b624274d4aee47f38a84e814.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py3_library-ext-pending',
  'files': {'mod/ya.make': 'PY3_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_b9b624274d4aee47f38a84e814.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o',
              '$(B)/mod/objcopy_90759244cca0f899f325587f8a.o']},
 {'name': 'py3_library-dirs-before',
  'files': {'mod/ya.make': 'PY3_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_5af2ec18a425d37e7639c946f1.o',
              '$(B)/mod/objcopy_a714dca11e9d28e308c7a07c7a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py3_library-dirs-after',
  'files': {'mod/ya.make': 'PY3_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_a714dca11e9d28e308c7a07c7a.o',
              '$(B)/mod/objcopy_5af2ec18a425d37e7639c946f1.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py3_library-dirs-pending',
  'files': {'mod/ya.make': 'PY3_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_5af2ec18a425d37e7639c946f1.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o',
              '$(B)/mod/objcopy_90759244cca0f899f325587f8a.o']},
 {'name': 'py23_library-ext-before',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_b9b624274d4aee47f38a84e814.o',
              '$(B)/mod/objcopy_a714dca11e9d28e308c7a07c7a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py23_library-ext-after',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_a714dca11e9d28e308c7a07c7a.o',
              '$(B)/mod/objcopy_b9b624274d4aee47f38a84e814.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py23_library-ext-pending',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'ALL_RESOURCE_FILES(yaml configs)\n'
                           'FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_b9b624274d4aee47f38a84e814.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py23_library-dirs-before',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_5af2ec18a425d37e7639c946f1.o',
              '$(B)/mod/objcopy_a714dca11e9d28e308c7a07c7a.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py23_library-dirs-after',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(fixed.yaml fixed)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_a714dca11e9d28e308c7a07c7a.o',
              '$(B)/mod/objcopy_5af2ec18a425d37e7639c946f1.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']},
 {'name': 'py23_library-dirs-pending',
  'files': {'mod/ya.make': 'PY23_LIBRARY()\n'
                           'NO_PYTHON_INCLUDES()\n'
                           'PY_SRCS(a.py)\n'
                           'RESOURCE(data.dat payload)\n'
                           'ALL_RESOURCE_FILES_FROM_DIRS(STRIP configs/ configs configs/**)\n'
                           'FROM_SANDBOX(FILE 42 OUT_NOAUTO data.dat)\n'
                           'END()\n',
            'mod/a.py': '',
            'mod/fixed.yaml': '',
            'mod/configs/one.yaml': '',
            'mod/configs/nested/two.yaml': '',
            'mod/stone.cpp': ''},
  'output': '$(B)/mod/libpy3mod.global.a',
  'members': ['$(B)/mod/objcopy_90759244cca0f899f325587f8a.o',
              '$(B)/mod/objcopy_5af2ec18a425d37e7639c946f1.o',
              '$(B)/mod/objcopy_0fcb3b09c61198189e7ef9c360.o',
              '$(B)/mod/objcopy_8085b178cf94df8e6d7ab74903.o']}]
