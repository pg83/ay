# План переноса эмиттеров

## Текущее состояние

Первый этап уже по сути завершен:

- все `emit*`-функции вынесены в `emit_*.go`;
- старые большие carrier-файлы (`codegen.go`, `m3_misc.go`, `pb.go`, `resource.go`, часть `sources.go`) в основном очищены от верхнеуровневой `emit*`-логики;
- `go test ./...` проходит на текущем состоянии.

Новый remaining scope:

- в кодовой базе все еще много `Emit*`-функций вне `emit_*.go`;
- это уже не orchestration-слой, а low-level node emitters и их непосредственные helper-ы;
- нужен второй план: не про `emit*`, а про перенос `Emit*` и вычищение старых тематических файлов.

## Что осталось

Сейчас вне `emit_*.go` остаются такие `Emit*`:

- `ar.go`
  - `EmitAR`
  - `EmitARNamed`
  - `EmitARNamedTagged`
  - `EmitARGlobalNamedTagged`
- `as.go`
  - `EmitAS`
- `cc.go`
  - `EmitCC`
- `cp.go`
  - `EmitJVCPG4`
  - `EmitCP`
- `en.go`
  - `EmitEN`
- `ev.go`
  - `EmitEV`
- `js.go`
  - `EmitJS`
- `ld.go`
  - `EmitLD`
- `m3_misc.go`
  - `EmitR5`
  - `EmitJV`
  - `EmitJVSplit`
  - `EmitCF`
  - `EmitBI`
  - `EmitPR`
- `pb.go`
  - `EmitPB`
- `r6.go`
  - `EmitR6`

## Новая цель

Разнести не только `emit*`, но и `Emit*` primitives по `emit_*.go`, сохранив при этом нормальные смысловые family, а не делая "одна функция = один файл" без причины.

Нужно прийти к состоянию, где:

1. orchestration-слой (`emit*`) уже разложен;
2. primitive emitter-слой (`Emit*`) тоже собран в `emit_*.go`;
3. старые carrier-файлы либо исчезают, либо становятся helper-only;
4. в каждом `emit_*.go` лежит один coherent pipeline/family.

## Принципы

1. Не смешивать orchestration `emit*` и primitive `Emit*` в одном файле без причины.
2. Primitive `Emit*` группировать по реальному pipeline, а не по случайной исторической близости.
3. Helper-ы оставлять рядом с соответствующим primitive emitter family.
4. Не делать абстрактный `emit_helpers.go`.
5. После каждого meaningful family прогонять `gofmt` и `go test ./...`.

## Целевая раскладка для `Emit*`

### 1. Archive family

- `emit_ar.go`
  - `EmitAR`
  - `EmitARNamed`
  - `EmitARNamedTagged`
  - `EmitARGlobalNamedTagged`
  - `emitARNode`

Примечание:
- helper-ы из `ar.go`, связанные с именованием архивов и фильтрацией member inputs, должны жить рядом;
- после этого `ar.go` либо исчезает, либо становится чисто helper-file для archive naming, если это окажется чище.

### 2. Assembly family

- `emit_as.go`
  - `EmitAS`
  - `emitASYasm`

Примечание:
- `composeASPaths`, `composeASCmdArgs`, yasm-константы и связанные helper-ы должны остаться рядом;
- цель — чтобы `as.go` исчез или стал helper-only.

### 3. C/C++ compile family

- `emit_cc.go`
  - `EmitCC`

Примечание:
- это центральный primitive emitter, вокруг него likely останется отдельный большой family-файл;
- helper-ы из `cc.go` не дробить механически.

### 4. Copy / CP family

- `emit_cp.go`
  - `EmitCP`
  - `EmitJVCPG4`

Примечание:
- это один family про copy-style nodes и JV downstream rename step.

### 5. Enum / proto / event generator primitives

- `emit_en_primitive.go`
  - `EmitEN`

- `emit_pb_primitive.go`
  - `EmitPB`

- `emit_ev_primitive.go`
  - `EmitEV`

Примечание:
- если окажется, что `EmitPB` и `EmitEV` share too much runtime plumbing, можно оставить их в одном файле:
  - `emit_pb_ev.go`

### 6. LD / link family

- `emit_ld.go`
  - `EmitLD`

- `emit_dynamic_library.go`
  - `emitDynamicLibrary`

Примечание:
- `EmitLD` и DYNAMIC_LIBRARY — соседние, но не одинаковые уровни.
- Сводить их в один файл только если helper-слой реально общий.

### 7. Ragel / parser generators family

- `emit_r5.go`
  - `EmitR5`

- `emit_r6.go`
  - `EmitR6`

- `emit_bison_y.go`
  - `emitBisonY`

Примечание:
- это family generator-based source producers;
- orchestration `emitBisonY` уже вынесен, next step — primitive `EmitR5` / `EmitR6`.

### 8. Misc small-node primitives from old `m3_misc.go`

- `emit_cf_primitive.go`
  - `EmitCF`

- `emit_bi.go`
  - `EmitBI`

- `emit_pr_primitive.go`
  - `EmitPR`

- `emit_jv_primitive.go`
  - `EmitJV`
  - `EmitJVSplit`

Примечание:
- после этого `m3_misc.go` должен стать helper-only или исчезнуть.

### 9. JS family

- `emit_js.go`
  - `EmitJS`

### 10. Source dispatch family

- `emit_sources.go`
  - `emitOneSource`
  - `emitLibraryProtoSource`

Это уже сделано по сути; дальше трогать только если понадобится подтянуть к ним какие-то helper-ы из `sources.go`.

## Порядок работ

### Фаза A. Закрыть old carrier-файлы с `Emit*`

Порядок:
1. `m3_misc.go` primitives
2. `ar.go`
3. `as.go`
4. `dynlib/ld` соседние family

Причина:
- именно эти файлы сейчас сильнее всего остаются "старыми носителями".

### Фаза B. Primitive families вокруг core toolchain

Порядок:
1. `cc.go`
2. `cp.go`
3. `pb.go` / `ev.go` / `en.go`
4. `r5.go` / `r6.go` / `js.go`

Причина:
- это более чувствительные low-level emitters;
- их надо переносить уже после вычищения более простых и локальных family.

### Фаза C. Финальная зачистка helper-файлов

Задачи:
1. пересмотреть заголовки файлов;
2. убрать устаревшие комментарии, которые еще говорят про старую раскладку;
3. проверить, не осталось ли `Emit*`/`emit*` вне `emit_*.go`;
4. проверить, не осталось ли пустых/полупустых старых файлов.

## Что не делать

- Не смешивать `EmitCC` с unrelated `EmitPB`/`EmitEV` только ради уменьшения числа файлов.
- Не дробить один coherent primitive pipeline на множество микро-файлов.
- Не трогать working behavior ради косметики.
- Не переименовывать helper-ы массово без необходимости.

## Критерий завершения

Работа считается законченной, когда:

1. `grep 'func emit[A-Z]'` показывает только `emit_*.go`;
2. `grep 'func Emit[A-Z]'` тоже показывает только `emit_*.go`;
3. старые carrier-файлы либо исчезли, либо стали helper-only;
4. `go test ./...` проходит на финальном состоянии.
