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
	"maps"
	"time"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	incloudiov1alpha1 "certificate-set/api/v1alpha1"
)

// Certificate duration constants
const (
	// CertDuration20Years is the default duration for CA certificates (20 years)
	CertDuration20Years = 175200 * time.Hour
	// CertDuration1Year is the default duration for client certificates (1 year)
	CertDuration1Year = 8760 * time.Hour
	// CertRenewBefore30Days is when to renew certificates (30 days before expiry)
	CertRenewBefore30Days = 720 * time.Hour
)

func copyAnnotationsForChildResource(source map[string]string) map[string]string {
	result := maps.Clone(source)
	delete(result, "kubectl.kubernetes.io/last-applied-configuration")
	return result
}

// buildObjectMeta creates ObjectMeta for child resources
func buildObjectMeta(cs *incloudiov1alpha1.CertificateSet, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:        name,
		Namespace:   cs.Namespace,
		Labels:      cs.Labels,
		Annotations: copyAnnotationsForChildResource(cs.Annotations),
	}
}

// defaultCAPrivateKey returns the default private key configuration for CA certificates
func defaultCAPrivateKey() *certmanagerv1.CertificatePrivateKey {
	return &certmanagerv1.CertificatePrivateKey{
		Algorithm:      certmanagerv1.RSAKeyAlgorithm,
		RotationPolicy: certmanagerv1.RotationPolicyNever,
		Size:           2048,
	}
}

// caUsages returns the default usages for CA certificates
func caUsages() []certmanagerv1.KeyUsage {
	return []certmanagerv1.KeyUsage{
		certmanagerv1.UsageCertSign,
		certmanagerv1.UsageKeyEncipherment,
		certmanagerv1.UsageDigitalSignature,
	}
}

// buildCACertificateWithName creates a CA certificate with the given name
func buildCACertificateWithName(cs *incloudiov1alpha1.CertificateSet, name string) *certmanagerv1.Certificate {
	gv, _ := schema.ParseGroupVersion(cs.Spec.IssuerRef.APIVersion)
	return &certmanagerv1.Certificate{
		ObjectMeta: buildObjectMeta(cs, name),
		Spec: certmanagerv1.CertificateSpec{
			CommonName:  name,
			Duration:    &metav1.Duration{Duration: CertDuration20Years},
			IsCA:        true,
			IssuerRef:   cmmeta.ObjectReference{Group: gv.Group, Kind: cs.Spec.IssuerRef.Kind, Name: cs.Spec.IssuerRef.Name},
			PrivateKey:  defaultCAPrivateKey(),
			RenewBefore: &metav1.Duration{Duration: CertRenewBefore30Days},
			SecretName:  name,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: cs.Labels,
			},
			Usages: caUsages(),
		},
	}
}

func buildCACertificate(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Certificate {
	return buildCACertificateWithName(cs, CAName(cs))
}

func buildETCDCertificate(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Certificate {
	return buildCACertificateWithName(cs, ETCDName(cs))
}

func buildProxyCertificate(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Certificate {
	return buildCACertificateWithName(cs, ProxyName(cs))
}

func buildIssuer(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Issuer {
	name := CAName(cs)
	return &certmanagerv1.Issuer{
		ObjectMeta: buildObjectMeta(cs, name),
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				CA: &certmanagerv1.CAIssuer{
					SecretName: name,
				},
			},
		},
	}
}

func buildSuperAdminCertificate(cs *incloudiov1alpha1.CertificateSet, issuerName string) *certmanagerv1.Certificate {
	name := SuperAdminName(cs)
	return &certmanagerv1.Certificate{
		ObjectMeta: buildObjectMeta(cs, name),
		Spec: certmanagerv1.CertificateSpec{
			CommonName: name,
			Duration:   &metav1.Duration{Duration: CertDuration1Year},
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
			RenewBefore: &metav1.Duration{Duration: CertRenewBefore30Days},
			SecretName:  name,
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

func buildOIDCCertificate(cs *incloudiov1alpha1.CertificateSet) *certmanagerv1.Certificate {
	name := CAOIDCName(cs)
	cert := &certmanagerv1.Certificate{
		ObjectMeta: buildObjectMeta(cs, name),
		Spec: certmanagerv1.CertificateSpec{
			CommonName:  name,
			Duration:    &metav1.Duration{Duration: CertDuration20Years},
			PrivateKey:  defaultCAPrivateKey(),
			RenewBefore: &metav1.Duration{Duration: CertRenewBefore30Days},
			SecretName:  name,
			SecretTemplate: &certmanagerv1.CertificateSecretTemplate{
				Labels: cs.Labels,
			},
		},
	}

	switch cs.Spec.Environment {
	case incloudiov1alpha1.EnvironmentSystem:
		gv, _ := schema.ParseGroupVersion(cs.Spec.IssuerRef.APIVersion)
		cert.Spec.IsCA = true
		cert.Spec.IssuerRef = cmmeta.ObjectReference{Group: gv.Group, Kind: cs.Spec.IssuerRef.Kind, Name: cs.Spec.IssuerRef.Name}
		cert.Spec.Usages = caUsages()
	case incloudiov1alpha1.EnvironmentInfra:
		if cs.Spec.IssuerRefOidc != nil {
			gv, _ := schema.ParseGroupVersion(cs.Spec.IssuerRefOidc.APIVersion)
			cert.Spec.IsCA = false
			cert.Spec.IssuerRef = cmmeta.ObjectReference{Group: gv.Group, Kind: cs.Spec.IssuerRefOidc.Kind, Name: cs.Spec.IssuerRefOidc.Name}
		}
	}

	return cert
}

func isSystemOrInfra(environment incloudiov1alpha1.EnvironmentType) bool {
	return environment == incloudiov1alpha1.EnvironmentSystem || environment == incloudiov1alpha1.EnvironmentInfra
}
