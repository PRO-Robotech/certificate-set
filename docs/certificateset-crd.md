## CertificateSet CRD (`in-cloud.io/v1alpha1`)

Документ описывает параметры `spec` ресурса `CertificateSet`: что можно/нельзя задавать, какие комбинации валидны, и какие поля можно менять после создания.

---

## Общая логика контроллера

- **Всегда** создаётся CA:
  - `Certificate` `${name}-ca`
  - `Secret` `${name}-ca` (создаёт cert-manager)

- Если включён **хотя бы один** флаг `spec.kubeconfig` или `spec.argocdCluster`, то дополнительно создаются:
  - `Issuer` `${name}-ca`
  - `Certificate` `${name}-super-admin`
  - `Secret` `${name}-super-admin` (создаёт cert-manager)
  - секреты kubeconfig / ArgoCD (см. ниже)

- Для `environment: system|infra` дополнительно создаются:
  - `Certificate` `${name}-etcd`
  - `Certificate` `${name}-proxy`
  - `Certificate` `${name}-ca-oidc`

Важное ограничение реализации: `Certificate` и `Issuer` создаются **только если их нет** (контроллер не обновляет их при изменении spec).

---

## Поля `spec`

| Поле | Тип | Обяз. | Значения / формат | Можно менять после создания | Примечания |
|------|-----|------:|-------------------|----------------------------|------------|
| `environment` | string | да | `client`, `system`, `infra` | **нет** | Immutable (CRD CEL) |
| `issuerRef` | object | да | `name` (обяз.)<br>`apiVersion` (def `cert-manager.io/v1`)<br>`kind` (def `ClusterIssuer`) | да (но эффект ограничен) | Влияет только на новые дочерние ресурсы, уже созданные не будут обновлены |
| `issuerRefOidc` | object | нет | как `issuerRef` | да (но эффект ограничен) | Практически обязателен для `environment: infra` |
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

- **Нельзя**:
  - `spec.environment` (immutable)
  - `spec.kubeconfig` (immutable)
  - `spec.kubeconfigEndpoint`, если он уже был не пустой (immutable-after-set)

- **Можно** (и отрабатывает контроллер):
  - `spec.argocdCluster`: `true/false` (при выключении удаляется ArgoCD secret)

- **Формально можно, но эффекта почти не будет** (из-за create-if-not-exists для Certificates/Issuers):
  - `spec.issuerRef`
  - `spec.issuerRefOidc`

Если нужно “переиграть” сертификаты с новым `issuerRef/environment` — обычно требуется пересоздание CR или ручное удаление дочерних `Certificate`/`Issuer` (осторожно).

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


