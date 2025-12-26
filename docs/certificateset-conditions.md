# CertificateSet Conditions

Документ описывает логику простановки Conditions в статусе ресурса `CertificateSet`.

---

## Типы Conditions

| Condition | Описание |
|-----------|----------|
| `Ready` | Все ресурсы (Certificates, Issuer, Secrets) созданы и готовы |
| `Progressing` | Reconciliation в процессе, ждём готовности ресурсов |
| `Degraded` | Произошла ошибка при reconciliation |

---

## Состояния

### Успешное состояние (Ready)

Когда все ресурсы созданы и cert-manager выпустил сертификаты:

| Condition | Status | Reason | Message |
|-----------|--------|--------|---------|
| `Ready` | `True` | `AllResourcesReady` | All certificate resources created and ready |
| `Progressing` | `False` | `Complete` | Reconciliation complete |
| `Degraded` | `False` | `Healthy` | No errors |

### В процессе (Progressing)

Ресурсы созданы, но ещё не все готовы (cert-manager ещё не выпустил сертификаты):

| Condition | Status | Reason | Message |
|-----------|--------|--------|---------|
| `Ready` | `False` | `WaitingForResources` | `Certificate <name> is not ready` / `Issuer <name> is not ready` |
| `Progressing` | `True` | `ResourcesPending` | (то же сообщение) |
| `Degraded` | `False` | `Healthy` | No errors |

### Ошибка (Degraded)

При ошибках на любом этапе `Degraded=True` с соответствующим Reason:

| Reason | Когда возникает |
|--------|-----------------|
| `CAFailed` | Ошибка создания CA Certificate или дополнительных сертификатов (etcd, proxy, oidc) |
| `IssuerFailed` | Ошибка создания Issuer |
| `Phase2Failed` | Ошибка создания super-admin Certificate |
| `Phase3Failed` | Ошибка создания kubeconfig/ArgoCD secrets |
| `ArgoCDCleanupFailed` | Ошибка удаления ArgoCD secret при выключении `argocdCluster` |
| `ArgoCDNamespaceNotFound` | Namespace `beget-argocd` не существует (при `argocdCluster=true`) |
| `CheckFailed` | Ошибка проверки готовности ресурсов |
| `Error` | Общая ошибка |

---

## Проверка готовности ресурсов

Контроллер проверяет готовность всех созданных ресурсов перед установкой `Ready=True`:

### 1. Certificates (проверяется `status.conditions[type=Ready].status == True`)

| Certificate | Когда создаётся |
|-------------|-----------------|
| `${name}-ca` | Всегда |
| `${name}-etcd` | `environment: system` или `infra` |
| `${name}-proxy` | `environment: system` или `infra` |
| `${name}-ca-oidc` | `environment: system` или `infra` |
| `${name}-super-admin` | `kubeconfig=true` или `argocdCluster=true` |

### 2. Issuer (проверяется `status.conditions[type=Ready].status == True`)

| Issuer | Когда создаётся |
|--------|-----------------|
| `${name}-ca` | `kubeconfig=true` или `argocdCluster=true` |

---

## Диаграмма переходов

```
                    ┌─────────────────────────────┐
                    │       Reconcile start       │
                    └─────────────┬───────────────┘
                                  │
                    ┌─────────────▼───────────────┐
                    │      Create resources       │
                    │   (Certificates, Issuer,    │
                    │      Secrets)               │
                    └─────────────┬───────────────┘
                                  │
                         error?───┼───no error
                           │      │
              ┌────────────▼──┐   │
              │   Degraded    │   │
              │   = True      │   │
              │  (with Reason)│   │
              └───────────────┘   │
                                  │
                    ┌─────────────▼───────────────┐
                    │  checkAllResourcesReady()   │
                    │                             │
                    │  - All Certificates Ready?  │
                    │  - Issuer Ready?            │
                    └─────────────┬───────────────┘
                                  │
                    ┌─────────────┼─────────────┐
                    │             │             │
               not ready       error         ready
                    │             │             │
         ┌──────────▼──┐   ┌──────▼─────┐  ┌───▼────────────┐
         │ Progressing │   │  Degraded  │  │     Ready      │
         │ = True      │   │  = True    │  │     = True     │
         │ Ready=False │   │            │  │ Progressing    │
         │ (requeue    │   │            │  │   = False      │
         │  5 sec)     │   │            │  │ Degraded=False │
         └─────────────┘   └────────────┘  └────────────────┘
```

---

## Пример status

```yaml
status:
  conditions:
  - type: Ready
    status: "True"
    reason: AllResourcesReady
    message: All certificate resources created and ready
    lastTransitionTime: "2025-01-15T10:30:00Z"
    observedGeneration: 1
  - type: Progressing
    status: "False"
    reason: Complete
    message: Reconciliation complete
    lastTransitionTime: "2025-01-15T10:30:00Z"
    observedGeneration: 1
  - type: Degraded
    status: "False"
    reason: Healthy
    message: No errors
    lastTransitionTime: "2025-01-15T10:30:00Z"
    observedGeneration: 1
```
