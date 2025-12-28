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
| `CACertificatesFailed` | Ошибка создания CA Certificate или дополнительных сертификатов (ETCD, Proxy, OIDC) |
| `ClientCertificatesFailed` | Ошибка создания Issuer или super-admin Certificate |
| `DerivedSecretsFailed` | Ошибка создания kubeconfig или ArgoCD secrets |
| `ArgoCDCleanupFailed` | Ошибка удаления ArgoCD secret при выключении `argocdCluster` |
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

## Reconciliation Flow

```
Step 1: reconcileCACertificates()
        ├─ Create ${name}-ca Certificate
        └─ If system/infra: Create etcd, proxy, oidc Certificates
                │
                ▼ error?  ──────────► Degraded=True (CACertificatesFailed)
                │
Step 2: Wait for CA Secret (ca.crt, tls.crt, tls.key)
                │
                ▼ not ready? ──────► Requeue after 5s
                │
Step 3: reconcileClientCertificates() [if kubeconfig || argocdCluster]
        ├─ Create Issuer ${name}-ca
        └─ Create ${name}-super-admin Certificate
                │
                ▼ error?  ──────────► Degraded=True (ClientCertificatesFailed)
                │
Step 4: Wait for super-admin Secret
                │
                ▼ not ready? ──────► Requeue after 5s
                │
Step 5: reconcileDerivedSecrets()
        ├─ If kubeconfig: Create ${name}-kubeconfig Secret
        └─ If argocdCluster: Create ${name}-argocd-cluster Secret
                │
                ▼ error?  ──────────► Degraded=True (DerivedSecretsFailed)
                │
Step 6: checkAllResourcesReady()
        ├─ All Certificates have Ready=True?
        └─ Issuer has Ready=True? (if needed)
                │
        ┌───────┴───────┐
        │               │
   not ready          ready
        │               │
        ▼               ▼
  Progressing=True   Ready=True
  Ready=False        Progressing=False
  (requeue 5s)       Degraded=False
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
