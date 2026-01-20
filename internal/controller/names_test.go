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
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	incloudiov1alpha1 "certificate-set/api/v1alpha1"
)

func TestSystemIssuerName(t *testing.T) {
	cs := &incloudiov1alpha1.CertificateSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	got := SystemIssuerName(cs)
	want := "test-cluster-system-issuer"

	if got != want {
		t.Errorf("SystemIssuerName() = %q, want %q", got, want)
	}
}

func TestAllCertificateNames_WithSystemIssuer(t *testing.T) {
	tests := []struct {
		name           string
		cs             *incloudiov1alpha1.CertificateSet
		wantContains   []string
		wantNotContain []string
	}{
		{
			name: "client environment without system issuer",
			cs: &incloudiov1alpha1.CertificateSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: incloudiov1alpha1.CertificateSetSpec{
					Environment: incloudiov1alpha1.EnvironmentClient,
					Kubeconfig:  false,
				},
			},
			wantContains:   []string{"test-ca"},
			wantNotContain: []string{"test-system-issuer"},
		},
		{
			name: "client environment with system issuer configured",
			cs: &incloudiov1alpha1.CertificateSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: incloudiov1alpha1.CertificateSetSpec{
					Environment: incloudiov1alpha1.EnvironmentClient,
					Kubeconfig:  false,
					IssuerRefSystemIss: &incloudiov1alpha1.IssuerReference{
						Kind: "ClusterIssuer",
						Name: "my-issuer",
					},
				},
			},
			wantContains: []string{"test-ca", "test-system-issuer"},
		},
		{
			name: "infra environment with system issuer configured",
			cs: &incloudiov1alpha1.CertificateSet{
				ObjectMeta: metav1.ObjectMeta{Name: "infra-cluster"},
				Spec: incloudiov1alpha1.CertificateSetSpec{
					Environment: incloudiov1alpha1.EnvironmentInfra,
					Kubeconfig:  true,
					IssuerRefSystemIss: &incloudiov1alpha1.IssuerReference{
						Kind: "ClusterIssuer",
						Name: "my-issuer",
					},
					KubeconfigEndpoint: "https://api.example.com:6443",
				},
			},
			wantContains: []string{
				"infra-cluster-ca",
				"infra-cluster-etcd",
				"infra-cluster-proxy",
				"infra-cluster-ca-oidc",
				"infra-cluster-super-admin",
				"infra-cluster-system-issuer",
			},
		},
		{
			name: "system environment without system issuer",
			cs: &incloudiov1alpha1.CertificateSet{
				ObjectMeta: metav1.ObjectMeta{Name: "sys"},
				Spec: incloudiov1alpha1.CertificateSetSpec{
					Environment: incloudiov1alpha1.EnvironmentSystem,
					Kubeconfig:  false,
				},
			},
			wantContains: []string{
				"sys-ca",
				"sys-etcd",
				"sys-proxy",
				"sys-ca-oidc",
			},
			wantNotContain: []string{"sys-system-issuer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AllCertificateNames(tt.cs)

			for _, want := range tt.wantContains {
				if !slices.Contains(got, want) {
					t.Errorf("AllCertificateNames() missing %q, got %v", want, got)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if slices.Contains(got, notWant) {
					t.Errorf("AllCertificateNames() should not contain %q, got %v", notWant, got)
				}
			}
		})
	}
}
