//go:build (validation || infra.any || cluster.any || extended) && !sanity && !stress && (2.9 || 2.10 || 2.11)

package fleetworkspaces

import (
	"strings"
	"testing"

	"github.com/rancher/shepherd/clients/rancher"
	management "github.com/rancher/shepherd/clients/rancher/generated/management/v3"
	"github.com/rancher/shepherd/extensions/clusters"
	"github.com/rancher/shepherd/extensions/users"
	"github.com/rancher/shepherd/pkg/session"
	"github.com/rancher/tests/actions/projects"
	"github.com/rancher/tests/actions/rbac"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type MoveClusterToFleetWorkspaceTestSuite struct {
	suite.Suite
	client  *rancher.Client
	session *session.Session
	cluster *management.Cluster
}

func (fw *MoveClusterToFleetWorkspaceTestSuite) TearDownSuite() {
	currentWorkspace, err := getClusterFleetWorkspace(fw.client, fw.cluster.ID)
	require.NoError(fw.T(), err)

	if currentWorkspace != fw.cluster.FleetWorkspaceName {
		_, err := moveClusterToNewWorkspace(fw.client, fw.cluster, fw.cluster.FleetWorkspaceName)
		if err != nil {
			log.Warn("Failed to move cluster back to the original workspace:", err)
		}
	}

	fw.session.Cleanup()
}

func (fw *MoveClusterToFleetWorkspaceTestSuite) SetupSuite() {
	fw.session = session.NewSession()

	client, err := rancher.NewClient("", fw.session)
	require.NoError(fw.T(), err)
	fw.client = client

	log.Info("Getting cluster name from the config file and append cluster details in fw")
	clusterName := client.RancherConfig.ClusterName
	require.NotEmptyf(fw.T(), clusterName, "Cluster name to install should be set")
	clusterID, err := clusters.GetClusterIDByName(fw.client, clusterName)
	require.NoError(fw.T(), err, "Error getting cluster ID")
	fw.cluster, err = fw.client.Management.Cluster.ByID(clusterID)
	require.NoError(fw.T(), err)
}

func (fw *MoveClusterToFleetWorkspaceTestSuite) testMoveClusterAndVerifyBindings(client *rancher.Client, user *management.User, role rbac.Role, newFleetWorkspace string, oldFleetWorkspace string) {
	log.Info("Retrieve the role bindings created for the cluster in the old fleet workspace .")
	allRbList, err := getAllRoleBindingsForCluster(fw.client, fw.cluster.ID, oldFleetWorkspace)
	require.NoError(fw.T(), err)
	allRbCount := len(allRbList)

	log.Info("Move the downstream RKE1 cluster to the newly created fleet workspace.")
	updatedCluster, err := moveClusterToNewWorkspace(client, fw.cluster, newFleetWorkspace)
	require.NoError(fw.T(), err, "Failed to update cluster with the new workspace")

	log.Info("Verify that the user can still list the downstream cluster.")
	newUserClient, err := fw.client.AsUser(user)
	require.NoError(fw.T(), err)
	rbac.VerifyUserCanListCluster(fw.T(), client, newUserClient, fw.cluster.ID, role)

	log.Info("Verify that the cluster is no longer available in the old fleet workspace.")
	_, _, err = clusters.GetProvisioningClusterByName(fw.client, updatedCluster.ID, oldFleetWorkspace)
	require.Error(fw.T(), err)

	if role != rbac.ClusterMember {
		log.Info("Verify that the old role bindings for the user are deleted from the old fleet workspace.")
		err = verifyRoleBindingsForUser(fw.client, user, updatedCluster.ID, oldFleetWorkspace, 0)
		require.NoError(fw.T(), err)
	}

	log.Info("Verify that the new role binding is created for the user in the new fleet workspace.")
	err = verifyRoleBindingsForUser(fw.client, user, updatedCluster.ID, newFleetWorkspace, 1)
	require.NoError(fw.T(), err)

	if role == rbac.ProjectOwner || role == rbac.ProjectMember {
		log.Info("Verify that the project role template binding exists for the user.")
		_, err = rbac.VerifyProjectRoleTemplateBindingForUser(fw.client, user.ID, 1)
		require.NoError(fw.T(), err)
	} else {
		log.Info("Verify that the cluster role template binding exists for the user.")
		_, err = rbac.VerifyClusterRoleTemplateBindingForUser(fw.client, user.ID, 1)
		require.NoError(fw.T(), err)
	}

	log.Info("Verify that the new role bindings are created for the cluster in the new fleet workspace .")
	allRbList, err = getAllRoleBindingsForCluster(fw.client, fw.cluster.ID, newFleetWorkspace)
	require.NoError(fw.T(), err)
	require.Equal(fw.T(), allRbCount, len(allRbList))
}

func (fw *MoveClusterToFleetWorkspaceTestSuite) TestMoveClusterToNewFleetWorkspace() {
	subSession := fw.session.NewSession()
	defer subSession.Cleanup()

	tests := []struct {
		role rbac.Role
	}{
		{rbac.ClusterOwner},
		{rbac.ClusterMember},
		{rbac.ProjectOwner},
		{rbac.ProjectMember},
	}

	for _, tt := range tests {
		fw.Run("Validate moving cluster to a new fleet workspace and verifying the rolebindings for user with role "+tt.role.String(), func() {
			defer func() {
				_, err := moveClusterToNewWorkspace(fw.client, fw.cluster, fw.cluster.FleetWorkspaceName)
				if err != nil {
					log.Warn("Failed to move cluster back to the original workspace:", err)
				}
			}()

			log.Info("Create a project and a namespace in the project.")
			adminProject, _, err := projects.CreateProjectAndNamespaceUsingWrangler(fw.client, fw.cluster.ID)
			assert.NoError(fw.T(), err)

			log.Infof("Create a standard user and add the user to a cluster/project as role %s", tt.role)
			standardUser, _, err := rbac.AddUserWithRoleToCluster(fw.client, rbac.StandardUser.String(), tt.role.String(), fw.cluster, adminProject)
			assert.NoError(fw.T(), err)

			log.Info("Verify that the user can list the downstream cluster.")
			newUserClient, err := fw.client.AsUser(standardUser)
			assert.NoError(fw.T(), err)
			rbac.VerifyUserCanListCluster(fw.T(), fw.client, newUserClient, fw.cluster.ID, tt.role)

			if strings.Contains(tt.role.String(), "project") {
				log.Info("Verify that the project role template binding is created for the user.")
				_, err = rbac.VerifyProjectRoleTemplateBindingForUser(fw.client, standardUser.ID, 1)
				require.NoError(fw.T(), err)
			} else {
				log.Info("Verify that the cluster role template binding is created for the user.")
				_, err = rbac.VerifyClusterRoleTemplateBindingForUser(fw.client, standardUser.ID, 1)
				assert.NoError(fw.T(), err)
			}

			log.Info("Verify the role bindings created for the user in the fleet-default workspace.")
			err = verifyRoleBindingsForUser(fw.client, standardUser, fw.cluster.ID, defaultFleetWorkspace, 1)
			assert.NoError(fw.T(), err)

			log.Info("Create a new fleet workspace.")
			createdFleetWorkspace, err := createFleetWorkspace(fw.client)
			assert.NoError(fw.T(), err, "Failed to create workspace")

			fw.testMoveClusterAndVerifyBindings(fw.client, standardUser, tt.role, createdFleetWorkspace.Name, defaultFleetWorkspace)
		})
	}
}

func (fw *MoveClusterToFleetWorkspaceTestSuite) TestMoveClusterToNewFleetWorkspaceWithCustomRole() {
	subSession := fw.session.NewSession()
	defer subSession.Cleanup()

	log.Info("Create a custom global role with the following rules: fleetworkspaces with all verbs, gitrepos with all verbs, clusters with all verbs.")
	createdGlobalRole, err := createGlobalRoleWithFleetWorkspaceRules(fw.client)
	require.NoError(fw.T(), err)

	log.Info("Create a user with global role standard user and custom global role.")
	standardUser, err := users.CreateUserWithRole(fw.client, users.UserConfig(), rbac.StandardUser.String(), createdGlobalRole.Name)
	require.NoError(fw.T(), err)

	log.Info("Add the user as cluster owner to the downstream cluster.")
	_, err = rbac.CreateClusterRoleTemplateBinding(fw.client, fw.cluster.ID, standardUser, rbac.ClusterOwner.String())
	require.NoError(fw.T(), err)

	log.Info("Verify that the cluster role template binding is created for the user.")
	_, err = rbac.VerifyClusterRoleTemplateBindingForUser(fw.client, standardUser.ID, 1)
	require.NoError(fw.T(), err)

	log.Info("Verify that the role binding is created for the cluster in the fleet workspace.")
	err = verifyRoleBindingsForUser(fw.client, standardUser, fw.cluster.ID, defaultFleetWorkspace, 1)
	require.NoError(fw.T(), err)

	log.Info("Verify that the user can list the downstream cluster.")
	newUserClient, err := fw.client.AsUser(standardUser)
	require.NoError(fw.T(), err)
	rbac.VerifyUserCanListCluster(fw.T(), fw.client, newUserClient, fw.cluster.ID, rbac.ClusterOwner)

	log.Info("Create a new fleet workspace.")
	createdFleetWorkspace, err := createFleetWorkspace(newUserClient)
	require.NoError(fw.T(), err, "Failed to create workspace")

	fw.testMoveClusterAndVerifyBindings(newUserClient, standardUser, rbac.ClusterOwner, createdFleetWorkspace.Name, defaultFleetWorkspace)
}

func (fw *MoveClusterToFleetWorkspaceTestSuite) TestMoveClusterBackToOriginalWorkspace() {
	subSession := fw.session.NewSession()
	defer subSession.Cleanup()

	log.Infof("Create a standard user and add the user to a cluster/project as role %s", rbac.ClusterOwner.String())
	createdUser, _, err := rbac.AddUserWithRoleToCluster(fw.client, rbac.StandardUser.String(), rbac.ClusterOwner.String(), fw.cluster, nil)
	require.NoError(fw.T(), err)

	log.Info("Verify that the cluster role template binding is created for the user.")
	_, err = rbac.VerifyClusterRoleTemplateBindingForUser(fw.client, createdUser.ID, 1)
	require.NoError(fw.T(), err)

	log.Info("Verify that the role binding is created for the user in the fleet workspace.")
	err = verifyRoleBindingsForUser(fw.client, createdUser, fw.cluster.ID, defaultFleetWorkspace, 1)
	require.NoError(fw.T(), err)

	log.Info("Verify that the user can list the downstream cluster.")
	newUserClient, err := fw.client.AsUser(createdUser)
	require.NoError(fw.T(), err)
	rbac.VerifyUserCanListCluster(fw.T(), fw.client, newUserClient, fw.cluster.ID, rbac.ClusterOwner)

	log.Info("Create a new fleet workspace.")
	createdFleetWorkspace, err := createFleetWorkspace(fw.client)
	require.NoError(fw.T(), err, "Failed to create workspace")

	fw.testMoveClusterAndVerifyBindings(fw.client, createdUser, rbac.ClusterOwner, createdFleetWorkspace.Name, defaultFleetWorkspace)

	fw.testMoveClusterAndVerifyBindings(fw.client, createdUser, rbac.ClusterOwner, defaultFleetWorkspace, createdFleetWorkspace.Name)
}

func TestMoveClusterToFleetWorkspaceTestSuite(t *testing.T) {
	suite.Run(t, new(MoveClusterToFleetWorkspaceTestSuite))
}
