/home/pg/monorepo/yatool_orig - origin upstream система сборки
/home/pg/monorepo/yatool_orig/build - тонкие настройки сборки
/home/pg/monorepo/yatool_orig/devtools/ymake - мясо upstream, графогенератор
/home/pg/monorepo/yatool_orig/devtools/ya - upstream оркестратор сборки
pwd - переписывание ее на go

У меня сложилось ощущение, что в процессе переписывания скопилось много техдолга:

* копипаста
* хаки для тех или иных таргетов, которые пилили на ранних стадиях портирования, лишь бы собиралось

Нужен четкий план, по пунктам, чего порефакторить, чего схлопнуть
