package connectivity

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"

	provv1 "github.com/rancher/rancher/pkg/apis/provisioning.cattle.io/v1"
	"github.com/rancher/shepherd/clients/rancher"
	steveV1 "github.com/rancher/shepherd/clients/rancher/v1"
	"github.com/rancher/shepherd/extensions/charts"
	extensionClusters "github.com/rancher/shepherd/extensions/clusters"
	kubeapinodes "github.com/rancher/shepherd/extensions/kubeapi/nodes"
	"github.com/rancher/shepherd/extensions/kubectl"
	"github.com/rancher/shepherd/extensions/workloads"
	"github.com/rancher/shepherd/pkg/namegenerator"
	"github.com/rancher/tests/actions/clusters"
	"github.com/rancher/tests/actions/ssh"
	"github.com/rancher/tests/actions/workloads/pods"
	"github.com/sirupsen/logrus"
	appv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	pingPodProjectName     = "ping-project"
	containerName          = "test1"
	containerImage         = "ranchertest/mytestcontainer"
	kubeSystemNamespace    = "kube-system"
	cloudControllerManager = "aws-cloud-controller-manager"
)

type resourceNames struct {
	core   map[string]string
	random map[string]string
}

// newNames returns a new resourceNames struct
// it creates a random names with random suffix for each resource by using core and coreWithSuffix names
func newNames() *resourceNames {
	const (
		projectName             = "upgrade-wl-project"
		namespaceName           = "namespace"
		deploymentName          = "deployment"
		daemonsetName           = "daemonset"
		secretName              = "secret"
		serviceName             = "service"
		ingressName             = "ingress"
		defaultRandStringLength = 3
	)

	names := &resourceNames{
		core: map[string]string{
			"projectName":    projectName,
			"namespaceName":  namespaceName,
			"deploymentName": deploymentName,
			"daemonsetName":  daemonsetName,
			"secretName":     secretName,
			"serviceName":    serviceName,
			"ingressName":    ingressName,
		},
	}

	names.random = map[string]string{}
	for k, v := range names.core {
		names.random[k] = v + "-" + namegenerator.RandStringLower(defaultRandStringLength)
	}

	return names
}

// newPodTemplateWithTestContainer is a private constructor that returns pod template spec for workload creations
func newPodTemplateWithTestContainer() corev1.PodTemplateSpec {
	testContainer := newTestContainerMinimal()
	containers := []corev1.Container{testContainer}
	return workloads.NewPodTemplate(containers, nil, []corev1.LocalObjectReference{}, nil, nil)
}

// newTestContainerMinimal is a private constructor that returns container for minimal workload creations
func newTestContainerMinimal() corev1.Container {
	pullPolicy := corev1.PullAlways
	return workloads.NewContainer(containerName, containerImage, pullPolicy, nil, nil, nil, nil, nil)
}

// curlCommand is a helper to run a curl command on an job service
func curlCommand(client *rancher.Client, clusterID string, url string) (string, error) {
	logrus.Infof("Executing the kubectl command curl %s on the node", url)
	execCmd := []string{"curl", url}
	log, err := kubectl.Command(client, nil, clusterID, execCmd, "")
	if err != nil {
		return "", err
	}
	return log, nil
}

// This must be a valid port number, 10250 < hostPort < 65536
// The range 1-10250 are reserved to Rancher
// Using a random port to avoid 'port in use' failures and allow the test to be rerun
func getHostPort() int {
	return rand.IntN(55283) + 10251
}

// It will allocate a port from a range 30000-32767
// Using a random port to avoid 'port in use' failures and allow the test to be rerun
func getNodePort() int {
	return rand.IntN(2767) + 30000
}

// isCloudManagerEnabled is a helper function that verifies whether the cloud manager is enabled
func isCloudManagerEnabled(client *rancher.Client, clusterID string) (bool, error) {
	logrus.Info("Checking cluster version and if the cloud-controller-manager is installed")
	catalogClient, err := client.GetClusterCatalogClient(clusterID)
	if err != nil {
		return false, err
	}

	provisioningClusterID, err := extensionClusters.GetV1ProvisioningClusterByName(client, client.RancherConfig.ClusterName)
	if err != nil {
		return false, err
	}

	cluster, err := client.Steve.SteveType(extensionClusters.ProvisioningSteveResourceType).ByID(provisioningClusterID)
	if err != nil {
		return false, err
	}

	newCluster := &provv1.Cluster{}
	err = steveV1.ConvertToK8sType(cluster, newCluster)
	if err != nil {
		return false, err
	}

	if strings.Contains(newCluster.Spec.KubernetesVersion, "k3s") {
		// Runs only for k3s clusters.
		wranglerContext, err := client.WranglerContext.DownStreamClusterWranglerContext(clusterID)
		if err != nil {
			return false, err
		}
		latestDaemonset, err := wranglerContext.Apps.DaemonSet().List("kube-system", metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		for _, item := range latestDaemonset.Items {
			if strings.Contains(item.Name, "svclb-traefik") {
				return true, nil
			}
		}
		return false, nil
	} else {
		// This block runs for non K3s clusters, so that the `catalogClient.Apps(kubeSystemNamespace).Get` doesn't run if the cluster type is K3s
		app, err := catalogClient.Apps(kubeSystemNamespace).Get(context.TODO(), cloudControllerManager, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		return app != nil, nil
	}
}

// validateLoadBalancer is a helper function that verifies the cluster is able to connect to the load balancer
func validateLoadBalancer(client *rancher.Client, clusterID string, steveClient *steveV1.Client, nodePort int, workloadName string) error {
	logrus.Infof("Getting the node using the label [%v]", clusters.LabelWorker)
	query, err := url.ParseQuery(clusters.LabelWorker)
	if err != nil {
		return err
	}

	nodeList, err := steveClient.SteveType("node").List(query)
	if err != nil {
		return err
	}

	for _, machine := range nodeList.Data {
		logrus.Info("Getting the node IP")
		newNode := &corev1.Node{}
		err = steveV1.ConvertToK8sType(machine.JSONResp, newNode)
		if err != nil {
			return err
		}

		nodeIP := kubeapinodes.GetNodeIP(newNode, corev1.NodeExternalIP)
		if nodeIP == "" {
			nodeIP = kubeapinodes.GetNodeIP(newNode, corev1.NodeInternalIP)
		}

		log, err := curlCommand(client, clusterID, fmt.Sprintf("%s:%s/name.html", nodeIP, strconv.Itoa(nodePort)))
		if strings.Contains(log, workloadName) && err == nil {
			return nil
		}
	}

	return errors.New("Unable to connect to the load balancer")
}

// validateHostPortSSH is a helper function that verifies the cluster is able to connect to the node host port by ssh shell
func validateHostPortSSH(client *rancher.Client, clusterID string, clusterName string, steveClient *steveV1.Client, hostPort int, workloadName string) error {
	logrus.Infof("Getting the node using the label [%v]", clusters.LabelWorker)
	query, err := url.ParseQuery(clusters.LabelWorker)
	if err != nil {
		return err
	}

	nodeList, err := steveClient.SteveType("node").List(query)
	if err != nil {
		return err
	}

	for _, machine := range nodeList.Data {
		logrus.Info("Getting the node IP")
		newNode := &corev1.Node{}
		err = steveV1.ConvertToK8sType(machine.JSONResp, newNode)
		if err != nil {
			return err
		}

		nodeIP := kubeapinodes.GetNodeIP(newNode, corev1.NodeInternalIP)

		sshNode, err := ssh.CreateSSHNode(client, clusterName, clusterID)
		if err != nil {
			return err
		}

		curlCommand := fmt.Sprintf("curl %s:%s/name.html", nodeIP, strconv.Itoa(hostPort))

		logrus.Infof("curl command {%v}", curlCommand)

		log, err := sshNode.ExecuteCommand(curlCommand)
		if err != nil {
			continue
		}

		logrus.Infof("Log of the curl command {%v}", log)
		if strings.Contains(log, workloadName) {
			return nil
		}
	}

	return errors.New("Unable to connect to the host port")
}

// validateNodePort is a helper function that verifies the cluster is able to connect to the node port by job service
func validateNodePort(client *rancher.Client, clusterID string, steveClient *steveV1.Client, nodePort int, workloadName string) error {
	logrus.Infof("Getting the node using the label [%v]", clusters.LabelWorker)
	query, err := url.ParseQuery(clusters.LabelWorker)
	if err != nil {
		return err
	}

	nodeList, err := steveClient.SteveType("node").List(query)
	if err != nil {
		return err
	}

	for _, machine := range nodeList.Data {
		logrus.Info("Getting the node IP")
		newNode := &corev1.Node{}
		err = steveV1.ConvertToK8sType(machine.JSONResp, newNode)

		nodeIP := kubeapinodes.GetNodeIP(newNode, corev1.NodeExternalIP)
		if nodeIP == "" {
			nodeIP = kubeapinodes.GetNodeIP(newNode, corev1.NodeInternalIP)
		}

		log, err := curlCommand(client, clusterID, fmt.Sprintf("%s:%s/name.html", nodeIP, strconv.Itoa(nodePort)))
		if err != nil {
			return err
		}
		if strings.Contains(log, workloadName) {
			return nil
		}
	}

	return errors.New("Unable to connect to the node port")
}

// validateWorkload is a helper function that verifies if all pods are running by image
func validateWorkload(client *rancher.Client, clusterID string, deployment *appv1.Deployment, image string, expectedReplicas int, namespaceName string) error {
	logrus.Info("Waiting deployment comes up active")
	err := charts.WatchAndWaitDeployments(client, clusterID, namespaceName, metav1.ListOptions{
		FieldSelector: "metadata.name=" + deployment.Name,
	})
	if err != nil {
		return err
	}

	logrus.Info("Waiting for all pods to be running")
	err = pods.WatchAndWaitPodContainerRunning(client, clusterID, namespaceName, deployment)
	if err != nil {
		return err
	}

	logrus.Infof("Counting all pods running by image %s", image)
	countPods, err := pods.CountPodContainerRunningByImage(client, clusterID, namespaceName, image)
	if err != nil {
		return err
	}

	if expectedReplicas == countPods {
		return nil
	}

	return errors.New("Unable to run all pods")
}
