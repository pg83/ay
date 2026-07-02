# TODO

## Подозрительные спец-перестановки в genModule — вероятно, не нужны

Гипотеза: перечисленные ниже ручные переупорядочивания не воспроизводят явный
upstream-механизм, а подгоняют порядок под конкретные кейсы; правильный порядок
должен выпадать из более общего механизма (порядок обхода managed peers /
PEERDIR-пропагации в ymake). Следующий рефактор: найти upstream-механизм,
заменить им перестановки или удалить их, если гейт не заметит разницы.

- py2/py3-program: перенос contrib/libs/python и library/python/runtime_py3 в
  хвост archiveOrder (gen.go, switch по tokPy2Program/tokPy3ProgramBin/tokPy3Program)
- py3-program: cflagsAggOrder = archiveOrder вместо resolved (gen.go)
- sbomOrder: перестановка contrib/libs/cxxsupp после contrib/libs/cxxsupp/libcxx (gen.go)
- sbomOrder: перенос build/platform/lld перед аллокаторными peers +
  ownSbomInsertIdx для вставки собственного sbom после lld (gen.go)
- library/python/runtime_py3: splice build-root пути после abseil в
  effectiveAddInclGlobal (gen.go)
- ALLOCATOR(FAKE): фильтрация library/cpp/malloc/api/ из peer-архивов LD (gen.go)
- py3-program + jemalloc: moveArchivePathsAfter/movePathsAfter вокруг
  bldBuildCowOnLibbuildCowOnA (gen.go)
- objcopy lead: moveSubrangeToFront глобальных ресурсов при resources+globalSrcs (gen.go)

## Слой R: незамоделированные ветки upstream resource_handler

- yasm-паковщик (TYASMResourcePacker → ro_<hash>.rodata) не смоделирован: все наши
  потоки эквивалентны DONT_COMPRESS/не-x86-веткам, где upstream берёт raw. Нужен,
  если в срезах появится RESOURCE без DONT_COMPRESS на x86-64 вне py-стека.
- raw-фолбэк для не-CanHandle записей обычного RESOURCE (пути с ARCADIA_BUILD_ROOT):
  сейчас packResources кидает throw при отсутствии RawClosure; upstream пакует
  через raw. Реализовать при первом реальном кейсе.

## Мультимодуль: несколько upstream-юнитов сплющены в один ModuleData

В upstream PROTO_LIBRARY / PY3_PROGRAM — мультимодули: каждый вариант это
отдельный юнит со своим MODULE_TAG и своим конфигом (CPP_PROTO, PY3_PROTO,
PY3_BIN, PY3_BIN_LIB, ...). У нас частично это выражено осью
ModuleInstance.Language (LangCPP/LangPy/LangDescProto для proto — отдельные
инстансы с memo), а частично — сплющено в один ModuleData и разруливается
ad-hoc данными по месту:

- resourceBinTagForData vs resourceLibTagForData: bin- и lib-стороны
  PY3_PROGRAM — это два upstream-юнита (PY3_BIN / PY3_BIN_LIB), у нас — выбор
  тега per-call внутри одного модуля;
- d.programPairedLib, py3BinProgramSide: флаги-костыли той же пары;
- "PY3_PROTO"-тег py-proto пака и CPP_PROTO-override в emitResourceObjcopy:
  MODULE_TAG вариантов, передаваемый параметром вместо юнитного свойства;
- hOnly-ветвление результата specialized-модулей: результат одного варианта,
  собранный внутри чужого прохода.

Направление: довести ось вариантов до конца — вариант = собственный
ModuleInstance (как уже сделано для Language) со своим MODULE_TAG/env/результатом;
тогда per-call теги, парные флаги и hOnly-сборка схлопываются, а packResources
получает Tag из юнита, как в upstream.
