package secrets

import (
	"fmt"

	extclusterapi "github.com/rancher/shepherd/extensions/kubeapi/cluster"
	namegen "github.com/rancher/shepherd/pkg/namegenerator"

	"github.com/rancher/shepherd/clients/rancher"
	corev1 "k8s.io/api/core/v1"
)

// CreateSecret is a helper to create a secret using wrangler client
func CreateSecret(client *rancher.Client, clusterID, namespaceName string, data map[string][]byte, secretType corev1.SecretType, labels, annotations map[string]string) (*corev1.Secret, error) {
	ctx, err := extclusterapi.GetClusterWranglerContext(client, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster context: %w", err)
	}

	if labels == nil {
		labels = make(map[string]string)
	}
	if annotations == nil {
		annotations = make(map[string]string)
	}

	secretName := namegen.AppendRandomString("testsecret")
	secretTemplate := NewSecretTemplate(secretName, namespaceName, data, secretType, labels, annotations)

	createdSecret, err := ctx.Core.Secret().Create(&secretTemplate)
	if err != nil {
		return nil, fmt.Errorf("failed to create secret: %w", err)
	}

	return createdSecret, nil
}

// CreateSecretForCluster is a helper function that uses wrangler context to create a secret in the specified namespace in the given cluster using the secret object.
func CreateSecretForCluster(client *rancher.Client, secret *corev1.Secret, clusterID, namespace string) (*corev1.Secret, error) {
	return CreateSecret(client, clusterID, namespace, secret.Data, secret.Type, secret.Labels, secret.Annotations)
}

// CreateProjectScopedSecret creates a project-scoped secret in the project's backing namespace in the local cluster
func CreateProjectScopedSecret(client *rancher.Client, clusterID, projectID string, data map[string][]byte, secretType corev1.SecretType) (*corev1.Secret, error) {
	backingNamespace := fmt.Sprintf("%s-%s", clusterID, projectID)

	labels := map[string]string{
		ProjectScopedSecretLabel: projectID,
	}

	return CreateSecret(client, extclusterapi.LocalCluster, backingNamespace, data, secretType, labels, nil)
}
