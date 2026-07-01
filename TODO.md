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
