# Режимы запуска контроллера CertificateSet

## Параметры селекторов

| Параметр | Описание | Формат |
|----------|----------|--------|
| `--cluster-wide` | Обрабатывает ВСЕ CertificateSets во всех namespace | флаг (без значения) |
| `--namespace` | Фильтрация по конкретному namespace | `--namespace=<name>` |
| `--label-selector` | Фильтрация по label | `--label-selector=key=value` |

---

## Правила совместимости

```
┌─────────────────────────────────────────────────────────────────┐
│                    ОБЯЗАТЕЛЬНО указать хотя бы ОДИН:            │
│                                                                 │
│    --cluster-wide    ИЛИ    --namespace / --label-selector      │
│                                                                 │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  --cluster-wide НЕЛЬЗЯ комбинировать с другими селекторами      │
│                                                                 │
│  --namespace И --label-selector можно комбинировать (AND)       │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Матрица допустимых комбинаций

| cluster-wide | namespace | label-selector | Результат |
|:------------:|:---------:|:--------------:|-----------|
| - | - | - | ОШИБКА: требуется селектор |
| + | - | - | Все namespace, все ресурсы |
| + | + | - | ОШИБКА: несовместимо |
| + | - | + | ОШИБКА: несовместимо |
| + | + | + | ОШИБКА: несовместимо |
| - | + | - | Только указанный namespace |
| - | - | + | Все namespace, только с label |
| - | + | + | Указанный namespace + label (AND) |

---

## Примеры запуска

### Cluster-wide режим
```bash
# Обрабатывает ВСЕ CertificateSets во всех namespace
./manager --cluster-wide
```

### Namespace-scoped режим
```bash
# Только CertificateSets в namespace "production"
./manager --namespace=production
```

### Label-based режим
```bash
# Все CertificateSets с label target-cluster=cluster-foo
./manager --label-selector=target-cluster=cluster-foo
```

### Комбинированный режим (namespace + label)
```bash
# CertificateSets в namespace "production" И с label team=platform
./manager --namespace=production --label-selector=team=platform
```

---

## Ошибки валидации

### Без селекторов
```bash
$ ./manager
ERROR: Selector required: use --cluster-wide OR (--namespace and/or --label-selector)
```

### cluster-wide с другими селекторами
```bash
$ ./manager --cluster-wide --namespace=foo
ERROR: --cluster-wide cannot be combined with --namespace or --label-selector
```

### Неверный формат label-selector
```bash
$ ./manager --label-selector=invalid
ERROR: Invalid label-selector format, expected key=value
```

---

## Типичные сценарии использования

| Сценарий | Команда |
|----------|---------|
| Dev/Test - один контроллер на всё | `--cluster-wide` |
| Multi-tenant - по namespace | `--namespace=tenant-a` |
| Target cluster operator | `--label-selector=target-cluster=prod-cluster` |
| Команда в namespace | `--namespace=team-x --label-selector=owner=team-x` |
