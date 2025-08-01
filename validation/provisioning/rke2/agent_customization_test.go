//go:build validation || recurring

package rke2

import (
	"os"
	"testing"

	"github.com/rancher/shepherd/clients/rancher"
	management "github.com/rancher/shepherd/clients/rancher/generated/management/v3"
	"github.com/rancher/shepherd/extensions/cloudcredentials"
	"github.com/rancher/shepherd/extensions/users"
	password "github.com/rancher/shepherd/extensions/users/passwordgenerator"
	"github.com/rancher/shepherd/pkg/config"
	"github.com/rancher/shepherd/pkg/config/operations"
	namegen "github.com/rancher/shepherd/pkg/namegenerator"
	"github.com/rancher/shepherd/pkg/session"
	"github.com/rancher/tests/actions/clusters"
	"github.com/rancher/tests/actions/config/defaults"
	"github.com/rancher/tests/actions/machinepools"
	"github.com/rancher/tests/actions/provisioning"
	"github.com/rancher/tests/actions/provisioninginput"
	"github.com/rancher/tests/actions/qase"
	packageDefaults "github.com/rancher/tests/validation/provisioning/rke2/defaults"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

type agentCustomizationTest struct {
	client             *rancher.Client
	session            *session.Session
	standardUserClient *rancher.Client
	cattleConfig       map[string]any
}

func agentCustomizationSetup(t *testing.T) agentCustomizationTest {
	var r agentCustomizationTest
	testSession := session.NewSession()
	r.session = testSession

	client, err := rancher.NewClient("", testSession)
	assert.NoError(t, err)

	r.client = client

	r.cattleConfig = config.LoadConfigFromFile(os.Getenv(config.ConfigEnvironmentKey))

	r.cattleConfig, err = packageDefaults.LoadPackageDefaults(r.cattleConfig, "")
	assert.NoError(t, err)

	r.cattleConfig, err = defaults.SetK8sDefault(client, "rke2", r.cattleConfig)
	assert.NoError(t, err)

	enabled := true
	var testuser = namegen.AppendRandomString("testuser-")
	var testpassword = password.GenerateUserPassword("testpass-")
	user := &management.User{
		Username: testuser,
		Password: testpassword,
		Name:     testuser,
		Enabled:  &enabled,
	}

	newUser, err := users.CreateUserWithRole(client, user, "user")
	assert.NoError(t, err)

	newUser.Password = user.Password

	standardUserClient, err := client.AsUser(newUser)
	assert.NoError(t, err)

	r.standardUserClient = standardUserClient

	return r
}

func TestAgentCustomization(t *testing.T) {
	t.Parallel()
	r := agentCustomizationSetup(t)

	productionPool := []provisioninginput.MachinePools{provisioninginput.EtcdMachinePool, provisioninginput.ControlPlaneMachinePool, provisioninginput.WorkerMachinePool}
	productionPool[0].MachinePoolConfig.Quantity = 3
	productionPool[1].MachinePoolConfig.Quantity = 2
	productionPool[2].MachinePoolConfig.Quantity = 2

	agentCustomization := management.AgentDeploymentCustomization{
		AppendTolerations: []management.Toleration{
			{
				Key:   "TestKeyToleration",
				Value: "TestValueToleration",
			},
		},
		OverrideResourceRequirements: &management.ResourceRequirements{
			Limits: map[string]string{
				"cpu": "750m",
				"mem": "500Mi",
			},
			Requests: map[string]string{
				"cpu": "250m",
			},
		},
		OverrideAffinity: &management.Affinity{
			NodeAffinity: &management.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []management.PreferredSchedulingTerm{
					{
						Preference: &management.NodeSelectorTerm{
							MatchExpressions: []management.NodeSelectorRequirement{
								{
									Key:      "testAffinityKey",
									Operator: "In",
									Values:   []string{"true"},
								},
							},
						},
						Weight: 100,
					},
				},
			},
		},
	}

	customAgents := []string{"fleet-agent", "cluster-agent"}
	tests := []struct {
		name         string
		machinePools []provisioninginput.MachinePools
		client       *rancher.Client
		agent        string
	}{
		{"Custom_Fleet_Agent", productionPool, r.standardUserClient, customAgents[0]},
		{"Custom_Cluster_Agent", productionPool, r.standardUserClient, customAgents[1]},
	}

	for _, tt := range tests {
		var err error
		t.Cleanup(func() {
			logrus.Info("Running cleanup")
			r.session.Cleanup()
		})

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clusterConfig := new(clusters.ClusterConfig)
			operations.LoadObjectFromMap(defaults.ClusterConfigKey, r.cattleConfig, clusterConfig)
			clusterConfig.MachinePools = tt.machinePools

			if tt.agent == "fleet-agent" {
				clusterConfig.FleetAgent = &agentCustomization
				clusterConfig.ClusterAgent = nil
			}

			if tt.agent == "cluster-agent" {
				clusterConfig.ClusterAgent = &agentCustomization
				clusterConfig.FleetAgent = nil
			}

			provider := provisioning.CreateProvider(clusterConfig.Provider)
			credentialSpec := cloudcredentials.LoadCloudCredential(string(provider.Name))
			machineConfigSpec := machinepools.LoadMachineConfigs(string(provider.Name))

			_, err = provisioning.CreateProvisioningCluster(tt.client, provider, credentialSpec, clusterConfig, machineConfigSpec, nil)
			assert.NoError(t, err)
		})

		params := provisioning.GetProvisioningSchemaParams(tt.client, r.cattleConfig)
		err = qase.UpdateSchemaParameters(tt.name, params)
		if err != nil {
			logrus.Warningf("Failed to upload schema parameters %s", err)
		}
	}
}

func TestAgentCustomizationFailure(t *testing.T) {
	t.Parallel()
	r := agentCustomizationSetup(t)

	productionPool := []provisioninginput.MachinePools{provisioninginput.EtcdMachinePool, provisioninginput.ControlPlaneMachinePool, provisioninginput.WorkerMachinePool}
	productionPool[0].MachinePoolConfig.Quantity = 3
	productionPool[1].MachinePoolConfig.Quantity = 2
	productionPool[2].MachinePoolConfig.Quantity = 2

	agentCustomization := management.AgentDeploymentCustomization{
		AppendTolerations: []management.Toleration{
			{
				Key:   "BadLabel",
				Value: "123\"[];'{}-+=",
			},
		},
		OverrideAffinity:             &management.Affinity{},
		OverrideResourceRequirements: &management.ResourceRequirements{},
	}

	customAgents := []string{"fleet-agent", "cluster-agent"}
	tests := []struct {
		name         string
		machinePools []provisioninginput.MachinePools
		client       *rancher.Client
		agent        string
	}{
		{"Invalid_Custom_Fleet_Agent", productionPool, r.standardUserClient, customAgents[0]},
		{"Invalid_Custom_Cluster_Agent", productionPool, r.standardUserClient, customAgents[1]},
	}

	for _, tt := range tests {
		var err error
		t.Cleanup(func() {
			logrus.Info("Running cleanup")
			r.session.Cleanup()
		})

		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			clusterConfig := new(clusters.ClusterConfig)
			operations.LoadObjectFromMap(defaults.ClusterConfigKey, r.cattleConfig, clusterConfig)
			clusterConfig.MachinePools = tt.machinePools

			if tt.agent == "fleet-agent" {
				clusterConfig.FleetAgent = &agentCustomization
				clusterConfig.ClusterAgent = nil
			}

			if tt.agent == "cluster-agent" {
				clusterConfig.ClusterAgent = &agentCustomization
				clusterConfig.FleetAgent = nil
			}

			provider := provisioning.CreateProvider(clusterConfig.Provider)
			credentialSpec := cloudcredentials.LoadCloudCredential(string(provider.Name))
			machineConfigSpec := machinepools.LoadMachineConfigs(string(provider.Name))

			_, err = provisioning.CreateProvisioningCluster(tt.client, provider, credentialSpec, clusterConfig, machineConfigSpec, nil)
			assert.NoError(t, err)
		})

		params := provisioning.GetProvisioningSchemaParams(tt.client, r.cattleConfig)
		err = qase.UpdateSchemaParameters(tt.name, params)
		if err != nil {
			logrus.Warningf("Failed to upload schema parameters %s", err)
		}
	}
}
