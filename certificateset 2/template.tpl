apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  annotations: {{ .main.metadata.annotation }}
  labels: {{ .main.metadata.labels }}
  name: {{ .main.metadata.name }}-ca
  namespace: {{ .main.metadata.namespace }}
spec:
  commonName: {{ .main.metadata.name }}-ca
  duration: 175200h
  isCA: true
  issuerRef:
    group: {{ .main.spec.issuerRef.group }}
    kind: {{ .main.spec.issuerRef.kind }}
    name: {{ .main.spec.issuerRef.name }}
  privateKey:
    algorithm: RSA
    rotationPolicy: Never
    size: 2048
  renewBefore: 720h
  secretName: {{ .main.metadata.name }}-ca
  secretTemplate:
    labels: {{ .main.metadata.labels }}
  usages:
    - cert sign
    - key encipherment
    - digital signature

---
apiVersion: cert-manager.io/v1
kind: Issuer
metadata:
  annotations: {{ .main.metadata.annotation }}
  labels: {{ .main.metadata.labels }}
  name: {{ .main.metadata.name }}-ca
  namespace: {{ .main.metadata.namespace }}
spec:
  ca:
    secretName: {{ .main.metadata.name }}-ca

---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  annotations: {{ .main.metadata.annotation }}
  labels: {{ .main.metadata.labels }}
  name: {{ .main.metadata.name }}-super-admin
  namespace: {{ .main.metadata.namespace }}
spec:
  commonName: {{ .main.metadata.name }}-super-admin
  duration: 8760h
  ipAddresses:
  - 127.0.0.1
  isCA: false
  issuerRef:
    group: cert-manager.io
    kind: Issuer
    name: {{ .createdResource.issuer.metadata.name }}
  privateKey:
    algorithm: RSA
    rotationPolicy: Always
    size: 2048
  renewBefore: 720h
  secretName: {{ .main.metadata.name }}-super-admin
  secretTemplate:
    labels: {{ .main.metadata.labels }}
  subject:
    organizations:
    - system:masters
  usages:
  - client auth
  - data encipherment
  - key encipherment

---
apiVersion: v1
stringData:
  value: |
    apiVersion: v1
    clusters:
        - cluster:
            certificate-authority-data: {{ .generated.secret.super-admin.data."ca.crt" | b64enc }}
            server: {{ .createdResource.spec.kubeconfigEndpoint }}
          name: {{ .main.metadata.name }}
    contexts:
        - context:
            cluster: {{ .main.metadata.name }}
            user: {{ .main.metadata.name }}-super-admin
          name: {{ .main.metadata.name }}-super-admin@{{ .main.metadata.name }}
    current-context: {{ .main.metadata.name }}-super-admin@{{ .main.metadata.name }}
    kind: Config
    users:
        - name: {{ .main.metadata.name }}-super-admin
          user:
            client-certificate-data: {{ .generated.secret.super-admin.data."tls.crt" | b64enc }}
            client-key-data: {{ .generated.secret.super-admin.data."tls.key" | b64enc }}
kind: Secret
metadata:
  annotations: {{ .main.metadata.annotation }}
  labels: {{ .main.metadata.labels }}
  name: {{ .main.metadata.name }}-kubeconfig
  namespace: {{ .main.metadata.namespace }}
type: Opaque


{{ if .main.spec.argocdCluster }}
---
apiVersion: v1
stringData:
  config: |
    {
      "tlsClientConfig": {
        "caData": {{ .generated.secret.super-admin.data."ca.crt" | b64enc | quote }},
        "certData": {{ .generated.secret.super-admin.data."tls.crt" | b64enc | quote }},
        "insecure": false,
        "keyData": {{ .generated.secret.super-admin.data."tls.key" | b64enc | quote }},
      }
    }
  name: {{ .main.metadata.name }}
  server: {{ .createdResource.spec.kubeconfigEndpoint }}
kind: Secret
metadata:
  annotations: {{ .main.metadata.annotation }}
  labels: 
    {{ .main.metadata.labels }}
    argocd.argoproj.io/secret-type: cluster
  name: {{ .main.metadata.name }}-argocd-cluster
  namespace: beget-argocd
type: opaque
{{ end }}

{{ if or (eq .main.spec.environment "system" ) (eq .main.spec.environment "infra" )}}
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  annotations: {{ .main.metadata.annotation }}
  labels: {{ .main.metadata.labels }}
  name: {{ .main.metadata.name }}-etcd
  namespace: {{ .main.metadata.namespace }}
spec:
  commonName: {{ .main.metadata.name }}-etcd
  duration: 175200h
  isCA: true
  issuerRef:
    group: {{ .main.spec.issuerRef.group }}
    kind: {{ .main.spec.issuerRef.kind }}
    name: {{ .main.spec.issuerRef.name }}
  privateKey:
    algorithm: RSA
    rotationPolicy: Never
    size: 2048
  renewBefore: 720h
  secretName: {{ .main.metadata.name }}-etcd
  secretTemplate:
    labels: {{ .main.metadata.labels }}
  usages:
    - cert sign
    - key encipherment
    - digital signature
---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  annotations: {{ .main.metadata.annotation }}
  labels: {{ .main.metadata.labels }}
  name: {{ .main.metadata.name }}-proxy
  namespace: {{ .main.metadata.namespace }}
spec:
  commonName: {{ .main.metadata.name }}-proxy
  duration: 175200h
  isCA: true
  issuerRef:
    group: {{ .main.spec.issuerRef.group }}
    kind: {{ .main.spec.issuerRef.kind }}
    name: {{ .main.spec.issuerRef.name }}
  privateKey:
    algorithm: RSA
    rotationPolicy: Never
    size: 2048
  renewBefore: 720h
  secretName: {{ .main.metadata.name }}-proxy
  secretTemplate:
    labels: {{ .main.metadata.labels }}
  usages:
    - cert sign
    - key encipherment
    - digital signature

---
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  annotations: {{ .main.metadata.annotation }}
  labels: {{ .main.metadata.labels }}
  name: {{ .main.metadata.name }}-ca-oidc
  namespace: {{ .main.metadata.namespace }}
spec:
  commonName: {{ .main.metadata.name }}-ca-oidc
  duration: 175200h
  {{ if eq .main.spec.environment "infra" }}
  isCA: false
  issuerRef:
    group: {{ .main.spec.issuerRefOidc.group }}
    kind: {{ .main.spec.issuerRefOidc.kind }}
    name: {{ .main.spec.issuerRefOidc.name }}
  {{ end }}
  {{ if eq .main.spec.environment "system" }}
  isCA: true
  issuerRef:
    group: {{ .main.spec.issuerRef.group }}
    kind: {{ .main.spec.issuerRef.kind }}
    name: {{ .main.spec.issuerRef.name }}
  usages:
    - cert sign
    - key encipherment
    - digital signature
  {{ end }}
  privateKey:
    algorithm: RSA
    rotationPolicy: Never
    size: 2048
  renewBefore: 720h
  secretName: {{ .main.metadata.name }}-ca-oidc
  secretTemplate:
    labels: {{ .main.metadata.labels }}
{{ end }}