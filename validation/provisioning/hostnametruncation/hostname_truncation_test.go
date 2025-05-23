//go:build (validation || stress) && !infra.any && !infra.aks && !infra.eks && !infra.rke2k3s && !infra.gke && !infra.rke1 && !cluster.any && !cluster.custom && !cluster.nodedriver && !sanity && !extended

package hostnametruncation

import (
	"fmt"
	"testing"

	"github.com/rancher/shepherd/clients/rancher"
	"github.com/rancher/shepherd/extensions/cloudcredentials"
	"github.com/rancher/shepherd/pkg/config"
	namegen "github.com/rancher/shepherd/pkg/namegenerator"
	"github.com/rancher/shepherd/pkg/session"
	"github.com/rancher/tests/actions/clusters"
	"github.com/rancher/tests/actions/machinepools"
	"github.com/rancher/tests/actions/provisioning"
	"github.com/rancher/tests/actions/provisioning/permutations"
	"github.com/rancher/tests/actions/provisioninginput"
	"github.com/rancher/tests/actions/reports"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type HostnameTruncationTestSuite struct {
	suite.Suite
	client         *rancher.Client
	session        *session.Session
	clustersConfig *provisioninginput.Config
}

func (r *HostnameTruncationTestSuite) TearDownSuite() {
	r.session.Cleanup()
}

func (r *HostnameTruncationTestSuite) SetupSuite() {
	testSession := session.NewSession()
	r.session = testSession

	r.clustersConfig = new(provisioninginput.Config)
	config.LoadConfig(provisioninginput.ConfigurationFileKey, r.clustersConfig)

	client, err := rancher.NewClient("", testSession)
	require.NoError(r.T(), err)

	r.client = client
}

func (r *HostnameTruncationTestSuite) TestProvisioningRKE2ClusterTruncation() {
	tests := []struct {
		name                        string
		machinePoolNameLengths      []int
		hostnameLengthLimits        []int
		defaultHostnameLengthLimits []int
	}{
		{"Cluster level truncation", []int{10, 31, 63}, []int{10, 31, 63}, []int{10, 31, 63}},
		{"Machine pool level truncation - 10 characters", []int{10, 10, 10}, []int{10, 31, 63}, []int{10, 16, 63}},
		{"Machine pool level truncation - 31 characters", []int{10, 31, 63}, []int{31, 31, 31}, []int{10, 31, 63}},
		{"Machine pool level truncation - 63 characters", []int{10, 31, 63}, []int{63, 63, 63}, []int{10, 31, 63}},
		{"Cluster and machine pool level truncation - 31 characters", []int{10, 31, 63}, []int{31, 31, 31}, []int{10, 63, 31}},
	}
	for _, tt := range tests {
		for _, defaultLength := range tt.defaultHostnameLengthLimits {
			r.Run(tt.name+fmt.Sprintf("_defaultHostnameLimit:%d", defaultLength), func() {
				var hostnamePools []machinepools.HostnameTruncation

				for i, nameLength := range tt.machinePoolNameLengths {
					currentTruncationPool := machinepools.HostnameTruncation{
						Name:                   namegen.RandStringLower(nameLength),
						ClusterNameLengthLimit: defaultLength,
					}

					if len(tt.hostnameLengthLimits) >= i && len(tt.hostnameLengthLimits) > 0 {
						currentTruncationPool.PoolNameLengthLimit = tt.hostnameLengthLimits[i]
					}

					hostnamePools = append(hostnamePools, currentTruncationPool)
				}

				testConfig := clusters.ConvertConfigToClusterConfig(r.clustersConfig)
				testConfig.KubernetesVersion = r.clustersConfig.RKE2KubernetesVersions[0]
				testConfig.CNI = r.clustersConfig.CNIs[0]

				rke2Provider, _, _, _ := permutations.GetClusterProvider(permutations.RKE2ProvisionCluster, (*testConfig.Providers)[0], r.clustersConfig)

				credentialSpec := cloudcredentials.LoadCloudCredential(string(rke2Provider.Name))
				machineConfigSpec := machinepools.LoadMachineConfigs(string(rke2Provider.Name))

				clusterObject, err := provisioning.CreateProvisioningCluster(r.client, *rke2Provider, credentialSpec, testConfig, machineConfigSpec, hostnamePools)
				reports.TimeoutClusterReport(clusterObject, err)
				require.NoError(r.T(), err)

				provisioning.VerifyCluster(r.T(), r.client, testConfig, clusterObject)
				provisioning.VerifyHostnameLength(r.T(), r.client, clusterObject)
			})
		}
	}
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestProvisioningHostnameTruncationTestSuite(t *testing.T) {
	suite.Run(t, new(HostnameTruncationTestSuite))
}
