## CertificateSet CRD (`in-cloud.io/v1alpha1`)

Документ описывает параметры `spec` ресурса `CertificateSet`: что можно/нельзя задавать, какие комбинации валидны, и какие поля можно менять после создания.

---

## Общая логика контроллера

Reconciliation выполняется в 7 шагов:

1. **Создание CA-сертификатов** — всегда создаётся `${name}-ca`, для `system/infra` также `${name}-etcd`, `${name}-proxy`, `${name}-ca-oidc`
2. **Ожидание CA Secret** — cert-manager должен создать Secret с ключами `ca.crt`, `tls.crt`, `tls.key`
3. **Создание client-сертификатов** (если `kubeconfig=true` или `argocdCluster=true`):
   - `Issuer` `${name}-ca` (использует CA Secret)
   - `Certificate` `${name}-super-admin`
4. **Ожидание super-admin Secret** — cert-manager должен выпустить клиентский сертификат
5. **Создание derived-секретов**:
   - `${name}-kubeconfig` (если `kubeconfig=true`)
   - `${name}-argocd-cluster` в namespace `beget-argocd` (если `argocdCluster=true`)
6. **Проверка готовности** — все `Certificate` и `Issuer` должны иметь `Ready=True`
7. **Обновление статуса** — установка `Ready=True` или `Progressing=True`

### Создаваемые ресурсы

| Ресурс | Имя | Когда создаётся |
|--------|-----|-----------------|
| Certificate | `${name}-ca` | всегда |
| Certificate | `${name}-etcd` | `environment: system/infra` |
| Certificate | `${name}-proxy` | `environment: system/infra` |
| Certificate | `${name}-ca-oidc` | `environment: system/infra` |
| Issuer | `${name}-ca` | `kubeconfig=true` или `argocdCluster=true` |
| Certificate | `${name}-super-admin` | `kubeconfig=true` или `argocdCluster=true` |
| Secret | `${name}-kubeconfig` | `kubeconfig=true` |
| Secret | `${name}-argocd-cluster` | `argocdCluster=true` (в ns `beget-argocd`) |

> **Примечание:** Контроллер использует `CreateOrUpdate` для Certificate/Issuer, поэтому изменения в `spec.issuerRef` будут применены к существующим ресурсам.

---

## Поля `spec`

| Поле | Тип | Обяз. | Значения / формат | Можно менять после создания | Примечания |
|------|-----|------:|-------------------|----------------------------|------------|
| `environment` | string | да | `client`, `system`, `infra` | **нет** | Immutable (CRD CEL) |
| `issuerRef` | object | да | `name` (обяз.)<br>`apiVersion` (def `cert-manager.io/v1`)<br>`kind` (def `ClusterIssuer`) | да | Контроллер обновит существующие Certificate через `CreateOrUpdate` |
| `issuerRefOidc` | object | нет | как `issuerRef` | да | Практически обязателен для `environment: infra`; обновляется аналогично |
| `kubeconfig` | bool | да | `true` / `false` | **нет** | Immutable (CRD CEL) |
| `kubeconfigEndpoint` | string | нет* | URL API-сервера, напр. `https://cluster.example.com:6443` | да, **один раз** (если было пусто) | После установки становится immutable (CRD CEL) |
| `argocdCluster` | bool | нет | `true` / `false` | да | При `false` контроллер удаляет ArgoCD secret |

\* `kubeconfigEndpoint` обязателен, если включён `kubeconfig` **или** `argocdCluster` (см. CEL).

---

## Валидации (CEL / XValidation)

На уровне CRD действуют правила:

- **`kubeconfigEndpoint` обязателен**, если `kubeconfig=true` или `argocdCluster=true`:
  - `(!self.kubeconfig && !self.argocdCluster) || (has(self.kubeconfigEndpoint) && self.kubeconfigEndpoint != '')`

- **`environment` immutable**:
  - `self == oldSelf`

- **`kubeconfig` immutable**:
  - `self == oldSelf`

- **`kubeconfigEndpoint` immutable после установки**:
  - `oldSelf == '' || self == oldSelf`

---

## Матрица допустимых комбинаций

| `kubeconfig` | `argocdCluster` | `kubeconfigEndpoint` | Валидно CRD | Итог |
|---:|---:|---|---|---|
| false | false | отсутствует/`""` | да | только CA |
| false | false | задан (не пустой) | да | только CA (но endpoint “зафиксируется”) |
| true | false | не пустой | да | kubeconfig secret + client certs |
| false | true | не пустой | да | ArgoCD cluster secret + client certs |
| true | true | не пустой | да | оба секрета + client certs |
| (любое) | (любое) | `""` при любом включённом флаге | **нет** | CRD отклонит |

---

## Что можно менять после создания

- **Нельзя** (CRD CEL валидация):
  - `spec.environment` (immutable)
  - `spec.kubeconfig` (immutable)
  - `spec.kubeconfigEndpoint`, если он уже был не пустой (immutable-after-set)

- **Можно** (контроллер применит изменения):
  - `spec.argocdCluster`: `true/false` (при выключении удаляется ArgoCD secret)
  - `spec.issuerRef`: контроллер обновит существующие Certificate через `CreateOrUpdate`
  - `spec.issuerRefOidc`: аналогично, обновит OIDC Certificate

---

## ArgoCD secret

Если `spec.argocdCluster=true`, создаётся Secret:

- namespace: `beget-argocd`
- name: `${name}-argocd-cluster`

Если namespace `beget-argocd` отсутствует, reconciliation вернёт ошибку и будет ретраиться.

---

## Примеры

### Только CA

```yaml
apiVersion: in-cloud.io/v1alpha1
kind: CertificateSet
metadata:
  name: demo-ca-only
spec:
  environment: client
  issuerRef:
    name: selfsigned-issuer
  kubeconfig: false
```

### kubeconfig secret

```yaml
apiVersion: in-cloud.io/v1alpha1
kind: CertificateSet
metadata:
  name: demo-kubeconfig
spec:
  environment: client
  issuerRef:
    name: selfsigned-issuer
  kubeconfig: true
  kubeconfigEndpoint: "https://demo.example.com:6443"
```

### ArgoCD cluster secret (без kubeconfig secret)

```yaml
apiVersion: in-cloud.io/v1alpha1
kind: CertificateSet
metadata:
  name: demo-argocd
spec:
  environment: client
  issuerRef:
    name: selfsigned-issuer
  kubeconfig: false
  argocdCluster: true
  kubeconfigEndpoint: "https://demo.example.com:6443"
```


