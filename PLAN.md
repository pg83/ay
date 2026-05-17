# План переноса эмиттеров

## Цель

Разнести `emit*`-функции по файлам `emit_*.go` не по принципу "одна функция = один файл", а по смысловым семействам:

- по `KV` узла (`PY`, `PR`, `AR`, `EN`, `PB`, `JV`, `CP`, `CF`);
- по общему pipeline;
- по общим helper-ам и общим типам результатов.

Идея: один `emit_*.go` должен соответствовать одному семейству эмиссии, а не случайной исторической куче из большого файла.

## Правила раскладки

1. В одном `emit_*.go` держать только тесно связанные эмиттеры.
2. Локальные `result`-типы переносить рядом с их семейством.
3. Helper-ы без `emit` оставлять рядом с тем семейством, которое они обслуживают.
4. Не размазывать один pipeline по 5-6 файлам без причины.
5. После каждого смыслового семейства прогонять `gofmt` и `go test ./...`.

## Целевая раскладка

### 1. Базовые одиночные emit-файлы

Эти функции уже сами по себе являются отдельными сущностями и не требуют дополнительной группировки:

- `emit_ar_node.go`
  - `emitARNode`
- `emit_as_yasm.go`
  - `emitASYasm`
- `emit_bison_y.go`
  - `emitBisonY`
- `emit_check_config_h.go`
  - `emitCheckConfigH`
- `emit_cython_cpp.go`
  - `emitCythonCpp`
- `emit_dynamic_library.go`
  - `emitDynamicLibrary`
- `emit_swig_c.go`
  - `emitSwigC`

### 2. Python codegen family (`PY`)

Сюда входят эмиттеры, которые производят Python-related узлы или Python-generated inputs.

- `emit_py_codegen.go`
  - `emitPySrcs`
  - `emitPyRegister`

- `emit_py_aux.go`
  - `emitGeneratedPyAuxChunks`
  - `emitRawAuxResourceChunks`

### 3. Python resource / objcopy family (`PY` + objcopy pipeline)

Это один pipeline упаковки ресурсов, и его лучше держать вместе.

- `emit_py_objcopy.go`
  - `emitResourceObjcopy`
  - `emitKvOnlyObjcopyNode`
  - `emitYaConfJSONObjcopy`
  - `emitPyNamespaceObjcopy`
  - `emitPyMainObjcopy`
  - `emitNoCheckImportsObjcopy`
  - `emitPySrcObjcopy`

Примечание:
- helper-ы `objcopyHash`, `expandRootrel`, `prResourceExtraInputs`, `buildPySrcEntries*`, `chunkPySrcEntries` и связанные типы должны жить рядом с этим файлом или в соседнем `emit_py_objcopy_helpers.go`, если файл станет слишком длинным.

### 4. Enum / protobuf / event codegen family (`EN`, `PB`, `EV`)

Это единый блок generator-driven codegen, сейчас размазанный между `codegen.go` и `pb.go`.

- `emit_en.go`
  - `emitEnumSrcs`

- `emit_proto.go`
  - `emitProtoSrcs`
  - `emitCPPProtoSrcs`

- `emit_py_proto.go`
  - `emitPyProtoSrcs`
  - `emitPyProtoSrc`
  - `emitGeneratedPyProtoYapyc`
  - `emitPyProtoAuxChunks`

Примечание:
- `protoDirectImportIncludes`, `protoOutputRel`, `protoPythonResourceKey`, `protoPythonOutputRoot`, `protoPythonNamespaceArg` и связанные proto helper-ы оставить рядом с proto-family, а не в общем `codegen.go`.

### 5. RUN_PROGRAM / downstream-CC family (`PR`)

Это один цельный pipeline:

- эмитим `PR`;
- регистрируем generated outputs;
- при необходимости эмитим downstream `CC`.

Целевая раскладка:

- `emit_pr.go`
  - `emitRunProgram`
  - `emitRunProgramsForAR`

- `emit_codegen_cc.go`
  - `emitPRDownstreamCC`
  - `emitCodegenDownstreamCC`

Примечание:
- `prInputClosure`, `prEmitsIncludes` и прочие helper-ы для этого пути должны лежать рядом.

### 6. Archive family (`AR`)

Архивный pipeline тоже нужно держать целиком:

- `emit_archives.go`
  - `emitArchives`
  - `emitArchive`

- `emit_ar_node.go`
  - `emitARNode`

Примечание:
- если будет удобнее, `emitArchives`, `emitArchive` и `emitARNode` можно оставить в двух файлах:
  - low-level `emit_ar_node.go`
  - high-level `emit_archives.go`

### 7. JV / CF / BI misc family

Сейчас `emitMiscNodes` смешивает несколько семейств. Его не надо переносить "как есть" в новый файл и консервировать смесь.

Нормальная цель:

- `emit_jv.go`
  - `emitJVDownstreamCPCC`
  - JV-ветки из `emitMiscNodes`, если они будут выделены в отдельный entrypoint

- `emit_cf.go`
  - `emitExplicitCF`

- `emit_bi.go`
  - если появится отдельный `emitBuildInfo`, выделить его из `emitMiscNodes`

- `emit_misc_nodes.go`
  - временный файл/агрегатор, пока `emitMiscNodes` не будет распилен по смыслу

Принцип:
- `emitMiscNodes` должен либо исчезнуть совсем, либо стать тонким оркестратором, который вызывает более конкретные emit-функции.
- Не оставлять в нем реальную meat-логику надолго.

### 8. Source-dispatch family

Это верхний уровень маршрутизации по типам исходников.

- `emit_sources.go`
  - `emitOneSource`
  - `emitLibraryProtoSource`

Эти функции можно оставить вместе, потому что это не отдельный `KV`, а точка dispatch-а в compilation pipeline.

### 9. LD plugins family (`CP`)

- `emit_ld_plugins.go`
  - `emitOwnLDPlugins`

## Порядок переноса

### Фаза 1. Вычистить `codegen.go`

Цель:
- убрать из него конкретные emitters;
- оставить только действительно общие helper-ы, если они еще нужны нескольким семействам.

Порядок:
1. Python codegen family
2. Enum family
3. Proto helper-ы, если они логически ближе к `pb.go`

### Фаза 2. Вычистить `m3_misc.go`

Порядок:
1. `PR` family
2. `AR` family
3. `CF`
4. `JV`
5. распилить или упростить `emitMiscNodes`

Ключевая цель:
- `m3_misc.go` не должен остаться "свалкой эмиттеров".

### Фаза 3. Вычистить `pb.go`

Порядок:
1. `emitProtoSrcs`
2. `emitCPPProtoSrcs`
3. весь `PY proto` хвост
4. рядом перенести proto-specific helper-ы

### Фаза 4. Вычистить `resource.go`

Порядок:
1. собрать весь objcopy/resource pipeline в один family
2. рядом оставить типы `objcopyEmitResult`, `objcopyEmit`, `pySrcEntry`, `pySrcChunk`
3. helper-ы либо оставить в том же файле, либо выделить в `emit_py_objcopy_helpers.go`

### Фаза 5. Довести одиночные emit-файлы

Файлы:
- `ar.go`
- `as.go`
- `bison.go`
- `check_config_h.go`
- `cython.go`
- `dynlib.go`
- `swig.go`

Цель:
- чтобы `emit*` из них переехали в явные `emit_*.go`,
- а low-level `Emit*`/helper-ы остались в тематических файлах.

## Что не делать

- Не делать "одна функция = один файл" без причины.
- Не переносить helper-ы в абстрактный `emit_helpers.go`.
- Не оставлять семейство размазанным между `codegen.go`, `pb.go`, `resource.go`, `m3_misc.go` после завершения фазы.

## Критерий завершения

Работа считается законченной, когда:

1. все `emit*` лежат в `emit_*.go`;
2. большие исторические файлы (`codegen.go`, `m3_misc.go`, `pb.go`, `resource.go`) больше не содержат meat-логики emitters;
3. в каждом `emit_*.go` собрана одна смысловая family, а не случайный набор;
4. `go test ./...` проходит после каждого завершенного семейства и в конце целиком.
