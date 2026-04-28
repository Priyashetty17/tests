package deployments

import (
	"fmt"
	"strings"

	"github.com/rancher/shepherd/clients/rancher"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VerifyDeploymentStatus checks the status of a deployment and verifies that it matches the expected status reason, message, and number of ready replicas.
func VerifyDeploymentStatus(client *rancher.Client, clusterID, namespaceName, deploymentName, statusType, expectedStatusReason, expectedStatusMessage string, expectedReplicaCount int32) error {
	updatedDeploymentList, err := ListDeployments(client, clusterID, namespaceName, metav1.ListOptions{
		FieldSelector: "metadata.name=" + deploymentName,
	})
	if err != nil {
		return err
	}

	if len(updatedDeploymentList.Items) == 0 {
		return fmt.Errorf("deployment %s not found", deploymentName)
	}

	updatedDeployment := updatedDeploymentList.Items[0]

	statusMsg, statusReason, err := GetLatestStatusMessageFromDeployment(&updatedDeployment, statusType)
	if err != nil {
		return err
	}

	if !strings.Contains(statusMsg, expectedStatusMessage) {
		return fmt.Errorf("expected status message: %s, actual status message: %s", expectedStatusMessage, statusMsg)
	}

	if !strings.Contains(statusReason, expectedStatusReason) {
		return fmt.Errorf("expected status reason: %s, actual status reason: %s", expectedStatusReason, statusReason)
	}

	if updatedDeployment.Status.ReadyReplicas != expectedReplicaCount {
		return fmt.Errorf("unexpected number of ready replicas: expected %d, got %d", expectedReplicaCount, updatedDeployment.Status.ReadyReplicas)
	}

	return nil
}
