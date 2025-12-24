/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"maps"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	incloudiov1alpha1 "certificate-set/api/v1alpha1"
)

func copyAnnotationsForChildResource(source map[string]string) map[string]string {
	result := make(map[string]string)
	for k, v := range source {
		if k != "kubectl.kubernetes.io/last-applied-configuration" {
			result[k] = v
		}
	}
	return result
}

// CertificateData contains data extracted from a cert-manager generated Secret
type CertificateData struct {
	CACert  string // base64-encoded CA certificate
	TLSCert string // base64-encoded TLS certificate
	TLSKey  string // base64-encoded TLS private key
}

func buildCACertificate(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Certificate {
	gv, _ := schema.ParseGroupVersion(cs.Spec.IssuerRef.APIVersion)

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-ca", cs.Name),
			Namespace:   cs.Namespace,
			Labels:      cs.Labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: fmt.Sprintf("%s-ca", cs.Name),
			Duration:   &metav1.Duration{Duration: 175200 * 60 * 60 * 1000000000}, // 175200h in nanoseconds
			IsCA:       true,
			IssuerRef: cmmeta.ObjectReference{
				Group: gv.Group,
				Kind:  cs.Spec.IssuerRef.Kind,
				Name:  cs.Spec.IssuerRef.Name,
			},
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm:      certmanagerv1.RSAKeyAlgorithm,
				RotationPolicy: certmanagerv1.RotationPolicyNever,
				Size:           2048,
			},
			RenewBefore: &metav1.Duration{Duration: 720 * 60 * 60 * 1000000000}, // 720h
			SecretName:  fmt.Sprintf("%s-ca", cs.Name),
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: cs.Labels,
			},
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageCertSign,
				certmanagerv1.UsageKeyEncipherment,
				certmanagerv1.UsageDigitalSignature,
			},
		},
	}
}

func buildIssuer(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Issuer {
	return &certmanagerv1.Issuer{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-ca", cs.Name),
			Namespace:   cs.Namespace,
			Labels:      cs.Labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: fmt.Sprintf("%s-ca", cs.Name),
				},
			},
		},
	}
}

func buildSuperAdminCertificate(cs *incloudiov1alpha1.CertificateSet, issuerName string) *certmanagerv1.Certificate {
	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-super-admin", cs.Name),
			Namespace:   cs.Namespace,
			Labels:      cs.Labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: fmt.Sprintf("%s-super-admin", cs.Name),
			Duration:   &metav1.Duration{Duration: 8760 * 60 * 60 * 1000000000}, // 8760h (1 year)
			IsCA:       false,
			IssuerRef: cmmeta.ObjectReference{
				Group: certmanagerv1.SchemeGroupVersion.Group,
				Kind:  certmanagerv1.IssuerKind,
				Name:  issuerName,
			},
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm:      certmanagerv1.RSAKeyAlgorithm,
				RotationPolicy: certmanagerv1.RotationPolicyAlways,
				Size:           2048,
			},
			RenewBefore: &metav1.Duration{Duration: 720 * 60 * 60 * 1000000000}, // 720h
			SecretName:  fmt.Sprintf("%s-super-admin", cs.Name),
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: cs.Labels,
			},
			Subject: &certmanagerv1.X509Subject{
				Organizations: []string{"system:masters"},
			},
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageClientAuth,
				certmanagerv1.UsageDataEncipherment,
				certmanagerv1.UsageKeyEncipherment,
			},
		},
	}
}

func buildETCDCertificate(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Certificate {
	gv, _ := schema.ParseGroupVersion(cs.Spec.IssuerRef.APIVersion)

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-etcd", cs.Name),
			Namespace:   cs.Namespace,
			Labels:      cs.Labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: fmt.Sprintf("%s-etcd", cs.Name),
			Duration:   &metav1.Duration{Duration: 175200 * 60 * 60 * 1000000000}, // 175200h
			IsCA:       true,
			IssuerRef: cmmeta.ObjectReference{
				Group: gv.Group,
				Kind:  cs.Spec.IssuerRef.Kind,
				Name:  cs.Spec.IssuerRef.Name,
			},
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm:      certmanagerv1.RSAKeyAlgorithm,
				RotationPolicy: certmanagerv1.RotationPolicyNever,
				Size:           2048,
			},
			RenewBefore: &metav1.Duration{Duration: 720 * 60 * 60 * 1000000000},
			SecretName:  fmt.Sprintf("%s-etcd", cs.Name),
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: cs.Labels,
			},
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageCertSign,
				certmanagerv1.UsageKeyEncipherment,
				certmanagerv1.UsageDigitalSignature,
			},
		},
	}
}

func buildProxyCertificate(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Certificate {
	gv, _ := schema.ParseGroupVersion(cs.Spec.IssuerRef.APIVersion)

	return &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-proxy", cs.Name),
			Namespace:   cs.Namespace,
			Labels:      cs.Labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: fmt.Sprintf("%s-proxy", cs.Name),
			Duration:   &metav1.Duration{Duration: 175200 * 60 * 60 * 1000000000},
			IsCA:       true,
			IssuerRef: cmmeta.ObjectReference{
				Group: gv.Group,
				Kind:  cs.Spec.IssuerRef.Kind,
				Name:  cs.Spec.IssuerRef.Name,
			},
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm:      certmanagerv1.RSAKeyAlgorithm,
				RotationPolicy: certmanagerv1.RotationPolicyNever,
				Size:           2048,
			},
			RenewBefore: &metav1.Duration{Duration: 720 * 60 * 60 * 1000000000},
			SecretName:  fmt.Sprintf("%s-proxy", cs.Name),
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: cs.Labels,
			},
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageCertSign,
				certmanagerv1.UsageKeyEncipherment,
				certmanagerv1.UsageDigitalSignature,
			},
		},
	}
}

func buildOIDCCertificate(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Certificate {
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-ca-oidc", cs.Name),
			Namespace:   cs.Namespace,
			Labels:      cs.Labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Spec: certmanagerv1.CertificateSpec{
			CommonName: fmt.Sprintf("%s-ca-oidc", cs.Name),
			Duration:   &metav1.Duration{Duration: 175200 * 60 * 60 * 1000000000},
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm:      certmanagerv1.RSAKeyAlgorithm,
				RotationPolicy: certmanagerv1.RotationPolicyNever,
				Size:           2048,
			},
			RenewBefore: &metav1.Duration{Duration: 720 * 60 * 60 * 1000000000},
			SecretName:  fmt.Sprintf("%s-ca-oidc", cs.Name),
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: cs.Labels,
			},
		},
	}

	switch cs.Spec.Environment {
	case incloudiov1alpha1.EnvironmentSystem:
		gv, _ := schema.ParseGroupVersion(cs.Spec.IssuerRef.APIVersion)
		cert.Spec.IsCA = true
		cert.Spec.IssuerRef = cmmeta.ObjectReference{
			Group: gv.Group,
			Kind:  cs.Spec.IssuerRef.Kind,
			Name:  cs.Spec.IssuerRef.Name,
		}
		cert.Spec.Usages = []certmanagerv1.KeyUsage{
			certmanagerv1.UsageCertSign,
			certmanagerv1.UsageKeyEncipherment,
			certmanagerv1.UsageDigitalSignature,
		}
	case incloudiov1alpha1.EnvironmentInfra:
		if cs.Spec.IssuerRefOidc != nil {
			gv, _ := schema.ParseGroupVersion(cs.Spec.IssuerRefOidc.APIVersion)
			cert.Spec.IsCA = false
			cert.Spec.IssuerRef = cmmeta.ObjectReference{
				Group: gv.Group,
				Kind:  cs.Spec.IssuerRefOidc.Kind,
				Name:  cs.Spec.IssuerRefOidc.Name,
			}
		}
	}

	return cert
}

func buildKubeconfigSecret(cs *incloudiov1alpha1.CertificateSet, certData CertificateData) *corev1.Secret {
	kubeconfigTemplate := `apiVersion: v1
clusters:
    - cluster:
        certificate-authority-data: %s
        server: %s
      name: %s
contexts:
    - context:
        cluster: %s
        user: %s-super-admin
      name: %s-super-admin@%s
current-context: %s-super-admin@%s
kind: Config
users:
    - name: %s-super-admin
      user:
        client-certificate-data: %s
        client-key-data: %s`

	kubeconfigContent := fmt.Sprintf(kubeconfigTemplate,
		certData.CACert,
		cs.Spec.KubeconfigEndpoint,
		cs.Name,
		cs.Name,
		cs.Name,
		cs.Name,
		cs.Name,
		cs.Name,
		cs.Name,
		cs.Name,
		certData.TLSCert,
		certData.TLSKey,
	)

	labels := make(map[string]string)
	maps.Copy(labels, cs.Labels)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-kubeconfig", cs.Name),
			Namespace:   cs.Namespace,
			Labels:      labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"value": []byte(kubeconfigContent),
		},
	}
}

func buildArgoCDClusterSecret(cs *incloudiov1alpha1.CertificateSet, certData CertificateData) *corev1.Secret {
	configTemplate := `{
  "tlsClientConfig": {
    "caData": "%s",
    "certData": "%s",
    "insecure": false,
    "keyData": "%s"
  }
}`

	configContent := fmt.Sprintf(configTemplate, certData.CACert, certData.TLSCert, certData.TLSKey)

	labels := make(map[string]string)
	maps.Copy(labels, cs.Labels)

	// Add ArgoCD label for cluster secret discovery
	labels["argocd.argoproj.io/secret-type"] = "cluster"

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        fmt.Sprintf("%s-argocd-cluster", cs.Name),
			Namespace:   ArgoCDNamespace,
			Labels:      labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"config": []byte(configContent),
			"name":   []byte(cs.Name),
			"server": []byte(cs.Spec.KubeconfigEndpoint),
		},
	}
}

func isSystemOrInfra(environment incloudiov1alpha1.EnvironmentType) bool {
	return environment == incloudiov1alpha1.EnvironmentSystem || environment == incloudiov1alpha1.EnvironmentInfra
}
