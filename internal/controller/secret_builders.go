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
	"bytes"
	"fmt"
	"maps"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	incloudiov1alpha1 "certificate-set/api/v1alpha1"
)

// CertificateData contains data extracted from a cert-manager generated Secret
type CertificateData struct {
	CACert  string // base64-encoded CA certificate
	TLSCert string // base64-encoded TLS certificate
	TLSKey  string // base64-encoded TLS private key
}

// kubeconfigData holds data for kubeconfig template rendering
type kubeconfigData struct {
	ClusterName string
	Server      string
	CACert      string
	TLSCert     string
	TLSKey      string
}

var kubeconfigTemplate = template.Must(template.New("kubeconfig").Parse(`apiVersion: v1
clusters:
    - cluster:
        certificate-authority-data: {{.CACert}}
        server: {{.Server}}
      name: {{.ClusterName}}
contexts:
    - context:
        cluster: {{.ClusterName}}
        user: {{.ClusterName}}-super-admin
      name: {{.ClusterName}}-super-admin@{{.ClusterName}}
current-context: {{.ClusterName}}-super-admin@{{.ClusterName}}
kind: Config
users:
    - name: {{.ClusterName}}-super-admin
      user:
        client-certificate-data: {{.TLSCert}}
        client-key-data: {{.TLSKey}}`))

var argoCDConfigTemplate = template.Must(template.New("argocd").Parse(`{
  "tlsClientConfig": {
    "caData": "{{.CACert}}",
    "certData": "{{.TLSCert}}",
    "insecure": false,
    "keyData": "{{.TLSKey}}"
  }
}`))

func buildKubeconfigSecret(cs *incloudiov1alpha1.CertificateSet, certData CertificateData) (*corev1.Secret, error) {
	var buf bytes.Buffer
	if err := kubeconfigTemplate.Execute(&buf, kubeconfigData{
		ClusterName: cs.Name,
		Server:      cs.Spec.KubeconfigEndpoint,
		CACert:      certData.CACert,
		TLSCert:     certData.TLSCert,
		TLSKey:      certData.TLSKey,
	}); err != nil {
		return nil, fmt.Errorf("failed to render kubeconfig template: %w", err)
	}
	kubeconfigContent := buf.String()

	labels := make(map[string]string)
	maps.Copy(labels, cs.Labels)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        KubeconfigName(cs),
			Namespace:   cs.Namespace,
			Labels:      labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"value": []byte(kubeconfigContent),
		},
	}, nil
}

func buildArgoCDClusterSecret(cs *incloudiov1alpha1.CertificateSet, certData CertificateData) (*corev1.Secret, error) {
	var buf bytes.Buffer
	if err := argoCDConfigTemplate.Execute(&buf, certData); err != nil {
		return nil, fmt.Errorf("failed to render ArgoCD config template: %w", err)
	}

	labels := make(map[string]string)
	maps.Copy(labels, cs.Labels)
	labels["argocd.argoproj.io/secret-type"] = "cluster"

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        ArgoCDClusterName(cs),
			Namespace:   ArgoCDNamespace,
			Labels:      labels,
			Annotations: copyAnnotationsForChildResource(cs.Annotations),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"config": buf.Bytes(),
			"name":   []byte(cs.Name),
			"server": []byte(cs.Spec.KubeconfigEndpoint),
		},
	}, nil
}
