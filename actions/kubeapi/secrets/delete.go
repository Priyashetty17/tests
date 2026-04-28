package secrets

import (
	"fmt"

	"github.com/rancher/shepherd/clients/rancher"
	extclusterapi "github.com/rancher/shepherd/extensions/kubeapi/cluster"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeleteSecret deletes a secret from a specific namespace in the given cluster using the wrangler client
func DeleteSecret(client *rancher.Client, clusterID, namespaceName, secretName string) error {
	ctx, err := extclusterapi.GetClusterWranglerContext(client, clusterID)
	if err != nil {
		return fmt.Errorf("failed to get cluster context: %w", err)
	}

	return ctx.Core.Secret().Delete(namespaceName, secretName, &metav1.DeleteOptions{})
}
