"""Original native source callback ordering; T117 python-callbacks/native.json."""

CASES = [{'name': 'PY2_LIBRARY-before',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_SRCS(extra.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_5f0afff36777d396f5887bba74.o',
              '$(B)/grove/ledger/objcopy_3af7645cb1f8f8663fa7a28da1.o',
              '$(B)/grove/ledger/leaf.extension.reg.cpp.o']},
 {'name': 'PY2_LIBRARY-between',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(extra.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_5f0afff36777d396f5887bba74.o',
              '$(B)/grove/ledger/objcopy_3af7645cb1f8f8663fa7a28da1.o',
              '$(B)/grove/ledger/leaf.extension.reg.cpp.o']},
 {'name': 'PY2_LIBRARY-after',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_SRCS(extra.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_5f0afff36777d396f5887bba74.o',
              '$(B)/grove/ledger/objcopy_3af7645cb1f8f8663fa7a28da1.o',
              '$(B)/grove/ledger/leaf.extension.reg.cpp.o']},
 {'name': 'PY3_LIBRARY-before',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_SRCS(extra.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
              '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
              '$(B)/grove/ledger/objcopy_1f8c35ba3d8c9886686b2e6543.o',
              '$(B)/grove/ledger/objcopy_03e3e4e7cdb92c11fb47902789.o',
              '$(B)/grove/ledger/leaf.extension.reg3.cpp.o']},
 {'name': 'PY3_LIBRARY-between',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(extra.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
              '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
              '$(B)/grove/ledger/objcopy_1f8c35ba3d8c9886686b2e6543.o',
              '$(B)/grove/ledger/objcopy_03e3e4e7cdb92c11fb47902789.o',
              '$(B)/grove/ledger/leaf.extension.reg3.cpp.o']},
 {'name': 'PY3_LIBRARY-after',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_SRCS(extra.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
              '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
              '$(B)/grove/ledger/objcopy_1f8c35ba3d8c9886686b2e6543.o',
              '$(B)/grove/ledger/objcopy_03e3e4e7cdb92c11fb47902789.o',
              '$(B)/grove/ledger/leaf.extension.reg3.cpp.o']},
 {'name': 'PY2_LIBRARY-generated',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'COPY_FILE(note.py renamed.py)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(renamed.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_5f0afff36777d396f5887bba74.o',
              '$(B)/grove/ledger/objcopy_bf673b65e52cc45b10e87cc39c.o',
              '$(B)/grove/ledger/leaf.extension.reg.cpp.o']},
 {'name': 'PY2_LIBRARY-copy-later',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(renamed.py)\n'
                                    'COPY_FILE(note.py renamed.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_5f0afff36777d396f5887bba74.o',
              '$(B)/grove/ledger/objcopy_bf673b65e52cc45b10e87cc39c.o',
              '$(B)/grove/ledger/leaf.extension.reg.cpp.o']},
 {'name': 'PY2_LIBRARY-copy-chain',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'COPY_FILE(mid.py renamed.py)\n'
                                    'COPY_FILE(note.py mid.py)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(renamed.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_5f0afff36777d396f5887bba74.o',
              '$(B)/grove/ledger/leaf.extension.reg.cpp.o',
              '$(B)/grove/ledger/objcopy_bf673b65e52cc45b10e87cc39c.o']},
 {'name': 'PY2_LIBRARY-copy-context',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'COPY_FILE_WITH_CONTEXT(note.py renamed.py)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(renamed.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_5f0afff36777d396f5887bba74.o',
              '$(B)/grove/ledger/objcopy_bf673b65e52cc45b10e87cc39c.o',
              '$(B)/grove/ledger/leaf.extension.reg.cpp.o']},
 {'name': 'PY2_LIBRARY-nested-pyi',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(TOP_LEVEL pkg/note.py pkg/shape.pyi)\n'
                                    'PY_REGISTER(pkg.extension)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': '',
            'grove/ledger/pkg/note.py': '',
            'grove/ledger/pkg/shape.pyi': ''},
  'members': ['$(B)/grove/ledger/objcopy_4dca4a178172efee289728cd23.o',
              '$(B)/grove/ledger/objcopy_f1bba13f7652a87adc76f8e13f.o',
              '$(B)/grove/ledger/pkg.extension.reg.cpp.o']},
 {'name': 'PY2_LIBRARY-pyi',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(note.py extra.py helper.pyi)\n'
                                    'RESOURCE_FILES(DONT_COMPRESS data.dat)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': '',
            'grove/ledger/helper.pyi': '',
            'grove/ledger/data.dat': ''},
  'members': ['$(B)/grove/ledger/objcopy_c7b2ffbcf352995a8613c4534a.o',
              '$(B)/grove/ledger/objcopy_efdbcdeafdee2d313e6bc433d2.o',
              '$(B)/grove/ledger/objcopy_7476336555517b63d5a663981d.o',
              '$(B)/grove/ledger/leaf.extension.reg.cpp.o']},
 {'name': 'PY2_LIBRARY-pyi-between',
  'module': 'PY2_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY2_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(helper.pyi)\n'
                                    'RESOURCE(DONT_COMPRESS before.dat /before)\n'
                                    'PY_SRCS(note.py a.pyi)\n'
                                    'RESOURCE_FILES(DONT_COMPRESS between.dat)\n'
                                    'PY_SRCS(extra.py b.pyi)\n'
                                    'RESOURCE(DONT_COMPRESS after.dat /after)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': '',
            'grove/ledger/helper.pyi': '',
            'grove/ledger/a.pyi': '',
            'grove/ledger/b.pyi': '',
            'grove/ledger/before.dat': '',
            'grove/ledger/between.dat': '',
            'grove/ledger/after.dat': ''},
  'members': ['$(B)/grove/ledger/objcopy_2bdc7df791d3741dd68e188cf2.o',
              '$(B)/grove/ledger/objcopy_aacf9098ea7721b3abb2204e95.o',
              '$(B)/grove/ledger/objcopy_048fb736b2f7b1accd54b03fa8.o',
              '$(B)/grove/ledger/objcopy_7476336555517b63d5a663981d.o',
              '$(B)/grove/ledger/objcopy_5f0afff36777d396f5887bba74.o',
              '$(B)/grove/ledger/objcopy_af05b260cb5d575496c7a0f351.o',
              '$(B)/grove/ledger/objcopy_3af7645cb1f8f8663fa7a28da1.o',
              '$(B)/grove/ledger/objcopy_4bb4fcbdd69dae526eecde2465.o']},
 {'name': 'PY3_LIBRARY-generated',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'COPY_FILE(note.py renamed.py)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(renamed.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
              '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
              '$(B)/grove/ledger/objcopy_91cfa92181cfcddb867f07c4e5.o',
              '$(B)/grove/ledger/leaf.extension.reg3.cpp.o']},
 {'name': 'PY3_LIBRARY-copy-later',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(renamed.py)\n'
                                    'COPY_FILE(note.py renamed.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
              '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
              '$(B)/grove/ledger/objcopy_91cfa92181cfcddb867f07c4e5.o',
              '$(B)/grove/ledger/leaf.extension.reg3.cpp.o']},
 {'name': 'PY3_LIBRARY-copy-chain',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'COPY_FILE(mid.py renamed.py)\n'
                                    'COPY_FILE(note.py mid.py)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(renamed.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
              '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
              '$(B)/grove/ledger/leaf.extension.reg3.cpp.o',
              '$(B)/grove/ledger/objcopy_91cfa92181cfcddb867f07c4e5.o']},
 {'name': 'PY3_LIBRARY-copy-context',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'COPY_FILE_WITH_CONTEXT(note.py renamed.py)\n'
                                    'PY_SRCS(note.py)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(renamed.py)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': ''},
  'members': ['$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
              '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
              '$(B)/grove/ledger/objcopy_91cfa92181cfcddb867f07c4e5.o',
              '$(B)/grove/ledger/leaf.extension.reg3.cpp.o']},
 {'name': 'PY3_LIBRARY-nested-pyi',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(TOP_LEVEL pkg/note.py pkg/shape.pyi)\n'
                                    'PY_REGISTER(pkg.extension)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': '',
            'grove/ledger/pkg/note.py': '',
            'grove/ledger/pkg/shape.pyi': ''},
  'members': ['$(B)/grove/ledger/objcopy_5cbad432aefa03bf65caf92ba6.o',
              '$(B)/grove/ledger/objcopy_44e16ccc72acf2a6eda555cd63.o',
              '$(B)/grove/ledger/objcopy_28c0daeebed19ba7e889e979a0.o',
              '$(B)/grove/ledger/pkg.extension.reg3.cpp.o']},
 {'name': 'PY3_LIBRARY-pyi',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_REGISTER(leaf.extension)\n'
                                    'PY_SRCS(note.py extra.py helper.pyi)\n'
                                    'RESOURCE_FILES(DONT_COMPRESS data.dat)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': '',
            'grove/ledger/helper.pyi': '',
            'grove/ledger/data.dat': ''},
  'members': ['$(B)/grove/ledger/objcopy_84bd72945cfd8519917c92f10e.o',
              '$(B)/grove/ledger/objcopy_38f76e535f615e3bb54f6c7f05.o',
              '$(B)/grove/ledger/objcopy_fcf951c1f7375b6f255afd78f0.o',
              '$(B)/grove/ledger/objcopy_ca8e2854ae7fcb788c66ed9bd3.o',
              '$(B)/grove/ledger/leaf.extension.reg3.cpp.o']},
 {'name': 'PY3_LIBRARY-pyi-between',
  'module': 'PY3_LIBRARY',
  'files': {'grove/ledger/ya.make': 'PY3_LIBRARY(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'SRCS(extension.c)\n'
                                    'PY_SRCS(helper.pyi)\n'
                                    'RESOURCE(DONT_COMPRESS before.dat /before)\n'
                                    'PY_SRCS(note.py a.pyi)\n'
                                    'RESOURCE_FILES(DONT_COMPRESS between.dat)\n'
                                    'PY_SRCS(extra.py b.pyi)\n'
                                    'RESOURCE(DONT_COMPRESS after.dat /after)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': '',
            'grove/ledger/helper.pyi': '',
            'grove/ledger/a.pyi': '',
            'grove/ledger/b.pyi': '',
            'grove/ledger/before.dat': '',
            'grove/ledger/between.dat': '',
            'grove/ledger/after.dat': ''},
  'members': ['$(B)/grove/ledger/objcopy_83a152162aa65dcddd72ddc8b6.o',
              '$(B)/grove/ledger/objcopy_ee5312c3635af3ae5223da6985.o',
              '$(B)/grove/ledger/objcopy_dcc494e956029eaede43ace512.o',
              '$(B)/grove/ledger/objcopy_ca8e2854ae7fcb788c66ed9bd3.o',
              '$(B)/grove/ledger/objcopy_bf8f69a5a81483b9e2addcc2da.o',
              '$(B)/grove/ledger/objcopy_22e677d3ecdc3392a0b2add509.o',
              '$(B)/grove/ledger/objcopy_48e16a6c612ca006e057678aab.o',
              '$(B)/grove/ledger/objcopy_1f8c35ba3d8c9886686b2e6543.o',
              '$(B)/grove/ledger/objcopy_03e3e4e7cdb92c11fb47902789.o',
              '$(B)/grove/ledger/objcopy_75bbc3140d641c3800dc3fe78b.o']},
 {'name': 'PY2TEST-source-callbacks',
  'module': 'PY2TEST',
  'files': {'grove/ledger/ya.make': 'PY2TEST(ledger)\n'
                                    'NO_PYTHON_INCLUDES()\n'
                                    'NO_CHECK_IMPORTS()\n'
                                    'PY_SRCS(helper.pyi)\n'
                                    'RESOURCE(DONT_COMPRESS before.dat /before)\n'
                                    'TEST_SRCS(a.py a.pyi)\n'
                                    'RESOURCE_FILES(DONT_COMPRESS between.dat)\n'
                                    'TEST_SRCS(b.py b.pyi)\n'
                                    'RESOURCE(DONT_COMPRESS after.dat /after)\n'
                                    'END()\n',
            'grove/ledger/stone.cpp': '',
            'grove/ledger/local.cpp': '',
            'grove/ledger/note.py': 'x=1\n',
            'grove/ledger/gen.py': '',
            'grove/ledger/conftest.py': 'x=1\n',
            'grove/ledger/extra.py': 'x=2\n',
            'grove/ledger/extension.c': '',
            'grove/ledger/helper.pyi': '',
            'grove/ledger/a.py': '',
            'grove/ledger/b.py': '',
            'grove/ledger/a.pyi': '',
            'grove/ledger/b.pyi': '',
            'grove/ledger/before.dat': '',
            'grove/ledger/between.dat': '',
            'grove/ledger/after.dat': ''},
  'members': ['$(B)/grove/ledger/objcopy_2bdc7df791d3741dd68e188cf2.o',
              '$(B)/grove/ledger/objcopy_161e7d2df3259744d9488a2735.o',
              '$(B)/grove/ledger/objcopy_cbdc8a0c9ac46876027318f3d7.o',
              '$(B)/grove/ledger/objcopy_aacf9098ea7721b3abb2204e95.o',
              '$(B)/grove/ledger/objcopy_ea5f6ba82bc1f0b794cbc6d820.o',
              '$(B)/grove/ledger/objcopy_ab183a1b580a417e0f39695f85.o',
              '$(B)/grove/ledger/objcopy_048fb736b2f7b1accd54b03fa8.o',
              '$(B)/grove/ledger/objcopy_7476336555517b63d5a663981d.o']}]
