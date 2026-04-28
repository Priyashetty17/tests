package secrets

import (
	"fmt"

	"github.com/rancher/shepherd/clients/rancher"
	extclusterapi "github.com/rancher/shepherd/extensions/kubeapi/cluster"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UpdateSecretData updates the data of a secret in the specified namespace using the wrangler context for the given cluster
func UpdateSecretData(client *rancher.Client, clusterID, namespace, secretName string, newData map[string][]byte) (*corev1.Secret, error) {
	ctx, err := extclusterapi.GetClusterWranglerContext(client, clusterID)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster context: %w", err)
	}

	existingSecret, err := ctx.Core.Secret().Get(namespace, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	existingSecret.Data = newData
	updatedSecret, err := ctx.Core.Secret().Update(existingSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to update secret %s: %w", secretName, err)
	}

	return updatedSecret, nil
}

// UpdateProjectScopedSecretData updates the data of an project-scoped secret in the backing namespace of a project
func UpdateProjectScopedSecretData(client *rancher.Client, clusterID, projectID, secretName string, newData map[string][]byte) (*corev1.Secret, error) {
	backingNamespace := fmt.Sprintf("%s-%s", clusterID, projectID)

	return UpdateSecretData(client, extclusterapi.LocalCluster, backingNamespace, secretName, newData)
}
