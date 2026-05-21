/home/pg/monorepo/yatool - репозиторий с кодом upstream системы сборки ya/ymake
/home/pg/monorepo/yatool/build - сборочные скрипты
/home/pg/monorepo/yatool/devtools/ymake - код графопостроителя ymake
/home/pg/monorepo/yatool/devtools/ya/bin - код оркестратора ya

Ранее мы свели bit it bit код самой системы сборки

pg:home# ls run.sh sg*
run.sh  sg.json  sg2.dep  sg2.json  sg2_x86_64.json  sg3.dep  sg3.json
pg:home#

Теперь у нас новый проект - мы сводим сборочный граф для конкретного проекта, использующего ya/ymake:

/home/pg/monorepo/ydb (НЕ yatool!)

pg:home# pwd
/home/pg/monorepo/ydb
pg:home# cat run.sh
YA_TC=no YA_NO_RESPAWN=yes ./exp/ya-bin make --no-yt-store --ymake-bin=./exp/ymake -G -j0 \
    -ttt --sandboxing util/ut \
    -DOS_SDK=local --host-platform-flag=OS_SDK=local \
    > sg4.json
pg:home# ls sg4.json
sg4.json
pg:home#

Нужно добавить в validate.sh построение графа для этого проекта, и свести его bit to bit.

Важное отличие - не указаны флаги про musl,  И УКАЗАНЫ флаги про то, что надо генерить графовые ноды для запуска тестов -ttt!

Это самое важное отличие.
