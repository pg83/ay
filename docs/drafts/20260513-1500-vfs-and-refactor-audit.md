# VFS-конвертации и общий рефакторинг — детальный аудит

Дата: 2026-05-13. После PR-M3-make-streaming.

Цель: убрать «лишние» конвертации между `string` и `VFS` в коде эмиттера,
а заодно зачистить накопленный технический долг, который пользователь
правомерно называет «забыли порефакторить».

## 0. Текущее состояние

| Метрика | Значение |
|---|---|
| `ParseVFSOrSource(...)` сайтов в проде | **94** |
| `VFS.String()` вызовов в проде | 32 |
| String-typed path-идентификаторов в сигнатурах | ~45 |
| `gen.go` строк | 7452 |
| `m3_misc.go` строк | 1899 |
| `cc.go` строк | 1186 |

Самые тяжёлые «пеперхиватели» (`ParseVFSOrSource` на файл):

- `m3_misc.go` — 42 сайта (R5, JV, JVSplit, CF, BI, PR, archive)
- `gen.go` — 19 сайтов (PY, walkClosure, codegen-key construction)
- `pb.go` — 13 сайтов
- `ev.go` — 7 сайтов
- `resource.go` — 6
- `cp.go` — 5
- `en.go` — 4, `ar.go` — 4, `as.go` — 4, `ld.go` — 3

## 1. Где `string` обоснован, а где — нет

**Обоснованно `string`** (домен — литеральные аргументы программ, не пути):

- `Cmd.CmdArgs []string` — это аргументы запускаемого процесса. `protoc` принимает строки, не VFS. `.String()` тут происходит **на границе VFS→cmd_args**, один раз, на этапе композиции.
- `ModuleCCInputs.AddIncl []string`, `.PeerAddInclGlobal []string` — каждый элемент уже несёт `"$(S)/..."` префикс и идёт прямо в cmd_args через `-I` обёртку. Эти слайсы — это «`-I`-готовые» строки, не пути.
- `Cmd.Stdout`, `Cmd.Env[K]` — тоже cmd-args level.

**Должно быть `VFS`**, но сейчас `string`:

- `Node.Inputs/Outputs []VFS` — уже VFS, всё ОК на принимающей стороне.
- `GeneratedFileInfo.OutputPath string` — **должно быть `VFS`**. Это идентификатор $(B)-rooted файла, используется как ключ в `byOutput map[string]*GeneratedFileInfo` (см. `codegen_registry.go:91`). Вызывающие пишут `OutputPath: outputPath` где `outputPath` — литералом построенная `"$(B)/..."` строка; читающие зовут `s.codegen.Lookup(vfsPath.String())` (`scanner.go:924`) — туда-обратно.
- `GeneratedFileInfo.EmitsIncludes []string` — каждый элемент это уже VFS-form string («`$(B)/...`», «`$(S)/...`» или sysincl-имя). Сейчас `forEachResolvedChild` в `scanner.go:929` парсит каждую запись через `ParseVFSOrSource`. Должно быть `[]VFS` плюс отдельная маркировка sysincl-чек (sysincl-имя — это не VFS, а голое имя для `s.sysinclLookup`).
- `codegenOutputKey.path` — уже `VFS` (`gen.go:354`). Здесь система устаканилась.

**Сигнатуры эмиттеров, принимающие `string` где должна быть `VFS`:**

- `EmitEN(headerRel string, ...)` — `en.go:54`. Из headerRel внутри строится `Source(instance.Path + "/" + headerRel)`, `instance.Path + "/" + headerRel` (как голая строка для `--include-path`), `Build(instance.Path + "/" + headerRel + "_serialized.cpp")`. Тройное конкатенирование одной и той же базы; пользователь в чате назвал именно этот сайт.
- `EmitCP(srcAbsPath, dstAbsPath string)` — `cp.go:108`. Параметры — VFS-form-строки, внутри `ParseVFSOrSource` × 2.
- `EmitJVCPG4(srcAbsPath, jvPrimaryOutput string, ...)` — `cp.go:28`. То же.
- `EmitR5(ragel5BinPath, rlgenCdBinPath string, ...)` — `m3_misc.go:31`. На входе VFS-form-строки бинарей, внутри `ParseVFSOrSource` × 3.
- `EmitPB(srcRel, cppStyleguideBinary, protocBinary, ...)` — `pb.go`. Бинари приходят строками от mining/codegen, `srcAbs := "$(S)/" + protoRelPath` строится локально, в Inputs/Outputs `ParseVFSOrSource` × 6.
- `EmitEV` — `ev.go:187`. Аналогично.
- `EmitCF`, `EmitBI`, `EmitPR`, `EmitJV`, `EmitJVSplit` — `m3_misc.go`. Все принимают `string` для tool paths.
- `EmitCC(srcRel string, ...)` — `cc.go:254`. `srcRel` корректен (это относительный path внутри модуля), но внутри `composeCCPaths` уже строится VFS. Сигнатура ОК, но дальнейшее использование `outVFS.String()` / `inVFS.String()` сразу после построения — лишний alloc, см. секцию 3.D.

## 2. Каталог проблем

### A. Двойное построение пути (en.go-стиль)

Один и тот же relative-rel конкатенируется заново при каждом упоминании:

- `en.go:68-70`:
  ```go
  headerSrcVFS    := Source(instance.Path + "/" + headerRel)
  includePath     := instance.Path + "/" + headerRel
  serializedCPPVFS := Build(instance.Path + "/" + headerRel + "_serialized.cpp")
  ```
  Три аллокации одной базы. И дальше:
  ```go
  cmdArgs := []string{enumParserBin, headerSrcVFS.String(), ...}
  ```
  `.String()` снова собирает `"$(S)/" + Rel`, хотя `instance.Path + "/" + headerRel` уже посчитан в `includePath`.

- `pb.go:348-355`:
  ```go
  protoRelPath := moduleDir + "/" + srcRel
  protoBase    := strings.TrimSuffix(protoRelPath, ".proto")
  pbH    := "$(B)/" + protoBase + ".pb.h"
  pbCC   := "$(B)/" + protoBase + ".pb.cc"
  srcAbs := "$(S)/" + protoRelPath
  ```
  Дальше `pbH`, `pbCC` идут в cmd_args (корректно как строки) И в Inputs/Outputs через `ParseVFSOrSource(pbH)`. Двойная работа на каждом proto-файле.

- `gen.go:6194-6196`:
  ```go
  evRelPath := srcInstance.Path + "/" + srcRel
  evH    := "$(B)/" + evRelPath + ".pb.h"
  evPbCC := "$(B)/" + evRelPath + ".pb.cc"
  ```
  Те же грабли для EV.

- `m3_misc.go:41-43`, `:188-203`, `:298-303`, etc.

### B. Строковые поля в типизированных структурах

`GeneratedFileInfo` (`codegen_registry.go:57`) хранит:
- `OutputPath string` — но это всегда `"$(B)/..."` путь
- `EmitsIncludes []string` — но каждая запись это VFS-form-строка или sysincl-имя

На write-сайте: `OutputPath: outputPath` где `outputPath` — литерал-строка.
На read-сайте: `r.byOutput[info.OutputPath]` (карта `map[string]...`).
На consumer-сайте: `s.codegen.Lookup(vfsPath.String())` — `.String()` на каждый
`forEachResolvedChild` вызов в скан-DFS. Скан DFS вызывается ~миллионы раз
(M3 closure 8750 нод × средняя глубина включений ~20).

### C. `ParseVFSOrSource` — слишком толерантный шим

Реализация:
```go
func ParseVFSOrSource(s string) VFS {
    if v, ok := ParseVFS(s); ok { return v }
    return Source(s)
}
```

Поведенческий смысл: «если префикс есть — VFS, нет — `Source(s)`». Используется
в 94 местах. В большинстве — потому что вызывающий имеет VFS-form-строку
(`pbH`, `srcAbs`, `r6Out`, …) и хочет восстановить VFS. Это симптом, а не
решение: правильный фикс — не создавать строку в первую очередь.

Из реальных 94 сайтов:
- ~70 — «у меня была VFS-form строка от EmitX, теперь надо как VFS». Лечится
  изменением сигнатуры EmitX (возвращать VFS, а не строку).
- ~15 — «у меня tool-binary path от codegen registry / mineTools, нужно как
  VFS». Лечится типизацией tool-path как VFS на mining-стороне.
- ~9 — синтетические тестовые литералы (`"c.in"` без префикса). Tests-only
  использование — оставить `ParseVFSOrSource` как тестовый helper.

### D. Сразу-после-конструкции `.String()`

`cc.go:271-272`:
```go
outVFS, inVFS := composeCCPaths(...)
outputPath := outVFS.String()
inputPath  := inVFS.String()
```
Дальше `outputPath`, `inputPath` идут только в `composeTargetCC` /
`composeHostCC` (`cc.go:354-356`) как cmd_args. Композеры могли бы принять
VFS и сами .String'ить — но композеры тоже строят cmd_args, так что разумнее
один String() здесь и нести `string`. **Здесь оставить как есть** — это
правильная граница VFS→cmd_args.

Аналогично `as.go:156, 157, 255, 256`. ОК как есть.

### E. Слайс-конвертеры на границах

`vfsStringsSlice([]VFS) []string` (`vfs.go:128`) и `ToVFSSlice([]string) []VFS`
(`vfs.go:146`) — migration shims. Используются в нескольких узких местах, где
старый API всё ещё возвращает `[]string`. Эти места нужно идентифицировать и
прибить на источнике.

`grep -rn "ToVFSSlice\|vfsStringsSlice" /home/pg/monorepo/yatool/*.go` —
посчитать сайты, изучить, у каждого вынести VFS-генерацию на upstream.

## 3. Не-VFS-рефакторинги (которые «забыли»)

### G. gen.go — 7452 строк в одном файле

Семантические разделы (по `grep ^func`):
- modules / collectModule / collectStmts / applyXxxStmt — ~1500 LOC
- defaultPeerdirsFor / module-default rules — ~800 LOC
- emitOneSource / per-source dispatch — ~600 LOC
- codegen-output registries (PB/EV/EN/R5/CF/BI…) — ~1500 LOC
- умbrella / back-peer post-emit (`newPostEmitPrepare`) — ~400 LOC
- runGenInto / GenWithMode / Gen entry — ~150 LOC
- генштаб закона (ScanContext, walkClosure helpers) — ~500 LOC
- ALLOCATOR, sysincl-helper, прочее — остальное

Файл стоит разбить:
- `gen.go` — entry points (Gen, GenWithMode, runGenInto), genCtx, ModuleInstance.
- `modules.go` — collectModule, collectStmts, applyXxxStmt, applyXxxAddIncl.
- `emit_sources.go` — emitOneSource + источник-ориентированные ветки.
- `codegen.go` — emitEnumSrcs / emitPySrcs / emitPyRegister / EV/PB-driver.
- `post_emit.go` — newPostEmitPrepare + umbrella/back-peer state.
- `default_peers.go` — defaultPeerdirsFor и допуски модулей.

### H. cc.go composers — параметрические гирлянды

`composeTargetCC`/`composeHostCC` принимают по 12 параметров одного типа `[]string`:
```go
composeTargetCC(outputPath, inputPath string,
                ownAddIncl, peerAddIncl, ownCFlags, ownExtras,
                autoPeerCFlags, peerExtras, ownGlobalBucket, perSrcCFlags []string,
                isCxx, noCompilerWarnings bool) []string
```

Заменить на одну структуру `ccComposeArgs` (или развить уже существующий
`ModuleCCInputs` отдельным методом `(in *ModuleCCInputs) composeCmdArgs(...)`).
Сейчас 12 позиционных аргументов всех одного типа — лёгкий способ
поменять местами и не заметить (компилятор не поможет).

### I. ar.go — 7 перегрузок EmitAR*

`EmitAR / EmitARGlobal / EmitARNamed / EmitARNamedTagged / EmitARGlobalNamedTagged / EmitARGlobalNamed` — все 6 публичных + internal `emitARNode`. Большая часть — тонкие шимы.

Свернуть в один `EmitAR(opts AROptions)` где `AROptions` имеет необязательные
поля `BinaryName`, `Tag`, `Global`, `PluginVFS` и т.п.

### J. resource.go — повторяющийся objcopy-hashing

В `resource.go` три ветки строят одно и то же:
```go
hash := objcopyHash(...)
outputObj := Build(instance.Path + "/objcopy_" + hash + ".o")
```
Вынести в helper.

### K. ModuleCCInputs.SourceRoot / SrcDir — контекст, а не инпуты

`cc.go:79-145`: поля `SourceRoot string`, `SrcDir string` сидят в **CC-input
структуре**, но это per-walker контекст. Они дублируются от вызывающего к
вызывающему. Можно вынести в `genCtx` и тред'ить туда, не через ModuleCCInputs.
Текущая постановка — реликт миграции PR-30.

## 4. Дорожная карта рефакторинга

Порядок по риску ↑ и охвату ↓:

### PR-VFS-1: CodegenRegistry → VFS (низкий риск, малая площадь)

- `GeneratedFileInfo.OutputPath` → `VFS`
- `GeneratedFileInfo.EmitsIncludes` → разделить на `EmitsVFS []VFS` (нормальные пути) и `EmitsSysIncl []string` (sysincl-имена, не VFS); ИЛИ ввести `type EmitsInclude struct { V VFS; SysInclName string }` и оставить плоский слайс.
- `CodegenRegistry.byOutput map[string]*GeneratedFileInfo` → `VFSMap[*GeneratedFileInfo]`
- `Lookup(string)` → `Lookup(VFS)`
- В `scanner.go:924, 688` убрать `vfsPath.String()`.

Acceptance: L4 byte-exact M2+M3. Тесты PASS. Перф (gen wall) не растёт.

### PR-VFS-2: en.go EmitEN — устранить тройную конкатенацию

- Сигнатура: `EmitEN(instance ModuleInstance, header VFS, ...)` вместо `headerRel string`.
- Внутри: `serializedCPPVFS := Build(header.Rel + "_serialized.cpp")`, `serializedHVFS := Build(header.Rel + "_serialized.h")` (header.Rel = `instance.Path + "/" + headerRel` уже).
- `cmdArgs := []string{enumParserBin, header.String(), "--include-path", header.Rel, "--output", serializedCPPVFS.String()}`
- `Inputs: ...append(inputs, header)` без re-parse.

Этот PR — буквально пример из чата.

### PR-VFS-3: pb.go / ev.go — VFS-first проектирование

- В `EmitPB`: `pbH := Build(protoBase + ".pb.h")`, `pbCC := Build(protoBase + ".pb.cc")`, `srcAbs := Source(protoRelPath)` (VFS, не строка).
- В cmd_args: `pbH.String()`, `pbCC.String()`, `srcAbs.String()` — один раз каждый.
- `Inputs: []VFS{...}` — без `ParseVFSOrSource`.
- Аналогично `EmitEV`.
- Также бинари `protocBinary`, `cppStyleguideBinary`, `pbWrapperPath` — на mining-стороне сделать VFS-typed (`commonFlags` в mine.go возвращает map[string]string → миграция через `map[string]VFS` для tool paths).

### PR-VFS-4: cp.go — VFS параметры

- `EmitCP(srcAbs, dstAbs VFS)` вместо `string`.
- `EmitJVCPG4(srcAbs VFS, jvPrimaryOutput VFS, ...)`
- Поток данных: caller (gen.go) уже имеет VFS в большинстве вызовов — там сейчас просто `.String()` либо тянет string из codegen registry. После PR-VFS-1 codegen registry отдаёт VFS, и каскадно очищается.

### PR-VFS-5: m3_misc.go (R5/CF/BI/PR/JV) — единым проходом

42 сайта `ParseVFSOrSource` в одном файле — массовый, но шаблонный перевод
на VFS-параметры. Идёт после PR-VFS-1, потому что зависит от типа `OutputPath`.

### PR-VFS-6: gen.go consumer-сайты

- `walkClosure(ctx, ..., vfsPath VFS, ...)` уже принимает VFS — вызывающие должны передавать VFS, а не `ParseVFSOrSource(r6Out)`. Это автоматически фиксится по мере того, как `r6Out` становится VFS вверх по стеку.
- `emitPySrcs`, `emitPyRegister` — переписать инпуты на VFS.

После PR-VFS-1..6: ожидаемое число `ParseVFSOrSource` сайтов в проде < 5
(только тестовые-литеральные шимы и edge cases).

### PR-REFACTOR-1: gen.go split (отдельная ветка, независимо от VFS-серии)

Разбить 7452-строчный gen.go на 6 семантических файлов (см. секцию 3.G).
Чистый mechanical-move без изменения поведения. Стоит делать **после** VFS-серии,
чтобы не было merge-конфликтов с VFS-патчами.

### PR-REFACTOR-2: cc.go composer struct

Заменить 12-параметровые `composeTargetCC`/`composeHostCC` на структуру.
Малый риск, средняя польза для читабельности.

### PR-REFACTOR-3: ar.go — единый EmitAR(opts)

6 публичных EmitAR* → 1 + AROptions. Mechanical.

### PR-REFACTOR-4: ModuleCCInputs context-split

Вынести `SourceRoot`, `SrcDir` из `ModuleCCInputs` в `genCtx`. Меняет
сигнатуру composeCCPaths и нескольких composer'ов. Средний риск.

## 5. Acceptance per PR

Каждый PR:

1. `go build ./...` clean
2. `go vet ./...` clean
3. `go test ./... -count=1` PASS (~5s)
4. `./yatool gen --target tools/archiver --out .out/m2.json` →
   normalize → `our_sha256 == ref_sha256 == b4d440096b9c…`
5. `./yatool gen --target devtools/ymake/bin --out .out/m3.json` →
   normalize → `our_sha256 == ref_sha256 == c735f57761bf…`
6. Gen wall-time на M3 не растёт (<5s gate, текущее 4.33s).
7. `./yatool make -j 0 tools/archiver` стримит без ошибок.
8. `./yatool make -G tools/archiver` даёт `c3078d56…` (= `yatool gen --out -`).

## 6. Альтернатива — что не делать

Соблазн «убрать `Cmd.CmdArgs []string` и сделать всё типизированным» — **нет**.
cmd_args это литералы команды процессу, не пути. Введение типа `CmdArg` или
`PathOrFlag` поверх string добавит boxing, не убавит ошибок (компилятор и
так не отличит флаг от пути). Граница string-typed остаётся ровно по
`Cmd.CmdArgs` и `Cmd.Env`.

То же — `ModuleCCInputs.AddIncl []string`, `PeerAddInclGlobal []string`:
это `-I`-готовые префиксы, не VFS-пути. Конвертация туда-обратно стоит дороже
улучшения типобезопасности.

## 7. Метрики, которые ожидаются после VFS-серии

- `ParseVFSOrSource` сайтов: 94 → ≤5 (только тестовые шимы)
- `VFS.String()` сайтов: 32 → ~32 (число не меняется; меняется где они стоят
  — теперь только на VFS→cmd_args границе, один раз на путь)
- Аллокации в `./yatool gen --target devtools/ymake/bin` (heap profile):
  ожидание −10..20% (текущий профиль доминирует path-конкатенациями и
  ParseVFS allocations; сегодня 18 MB heap, ~5.7M allocs).
- Wall-time: без значимого изменения (path операции не доминируют после
  PR-M3-perf серии); может слегка улучшиться от меньшего allocation pressure.
