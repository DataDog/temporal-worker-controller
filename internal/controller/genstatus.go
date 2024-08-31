// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package controller

import (
	"context"
	"fmt"
	"sort"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/taskqueue/v1"
	"go.temporal.io/api/workflowservice/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporaliov1alpha1 "github.com/DataDog/temporal-worker-controller/api/v1alpha1"
)

type versionedDeploymentCollection struct {
	buildIDsToDeployments map[string]*appsv1.Deployment
	// map of build IDs to redirect (keys) to other build IDs (values)
	redirectBuildIDFromTo map[string]string
	// map of build IDs to ramp percentages [0,100]
	rampPercentages map[string]uint8
	// map of build IDs to task queue stats
	stats map[string]*taskqueue.TaskQueueStats
	// map of build IDs to reachability
	reachabilityStatus map[string]temporaliov1alpha1.ReachabilityStatus
}

func (c *versionedDeploymentCollection) GetDeployment(buildID string) (*appsv1.Deployment, bool) {
	if redirectedBuildID, ok := c.redirectBuildIDFromTo[buildID]; ok {
		return c.GetDeployment(redirectedBuildID)
	}
	d, ok := c.buildIDsToDeployments[buildID]
	return d, ok
}

func (c *versionedDeploymentCollection) GetVersionedDeployment(buildID string) *temporaliov1alpha1.VersionedDeployment {
	result := temporaliov1alpha1.VersionedDeployment{
		Healthy:            false,
		BuildID:            buildID,
		CompatibleBuildIDs: nil,
		Reachability:       "",
		RampPercentage:     nil,
		Deployment:         nil,
	}

	// Set deployment ref and health status
	if d, ok := c.GetDeployment(buildID); ok {
		// Check if deployment condition is "available"
		var healthy bool
		// TODO(jlegrone): do we need to sort conditions by timestamp?
		for _, c := range d.Status.Conditions {
			if c.Type == appsv1.DeploymentAvailable && c.Status == v1.ConditionTrue {
				healthy = true
			}
		}
		result.Healthy = healthy
		result.Deployment = newObjectRef(d)
	}

	// Set ramp percentage
	if ramp, ok := c.rampPercentages[buildID]; ok {
		result.RampPercentage = &ramp
	}

	// Set reachability
	if status, ok := c.reachabilityStatus[buildID]; ok {
		result.Reachability = status
	}

	return &result
}

func (c *versionedDeploymentCollection) AddBuildIDRedirect(from, to string) {
	c.redirectBuildIDFromTo[from] = to
}

func (c *versionedDeploymentCollection) AddDeployment(buildID string, d *appsv1.Deployment) {
	c.buildIDsToDeployments[buildID] = d
}

func (c *versionedDeploymentCollection) AddAssignmentRule(rule *taskqueue.BuildIdAssignmentRule) {
	rule.GetPercentageRamp().GetRampPercentage()
}

func (c *versionedDeploymentCollection) AddReachability(buildID string, info *taskqueue.TaskQueueVersionInfo) error {
	switch info.GetTaskReachability() {
	case enums.BUILD_ID_TASK_REACHABILITY_REACHABLE:
		c.reachabilityStatus[buildID] = temporaliov1alpha1.ReachabilityStatusReachable
	case enums.BUILD_ID_TASK_REACHABILITY_CLOSED_WORKFLOWS_ONLY:
		c.reachabilityStatus[buildID] = temporaliov1alpha1.ReachabilityStatusClosedOnly
	case enums.BUILD_ID_TASK_REACHABILITY_UNREACHABLE:
		c.reachabilityStatus[buildID] = temporaliov1alpha1.ReachabilityStatusUnreachable
	}
	return fmt.Errorf("unhandled build id reachability: %s", info.GetTaskReachability().String())
}

func newVersionedDeploymentCollection() versionedDeploymentCollection {
	return versionedDeploymentCollection{
		buildIDsToDeployments: make(map[string]*appsv1.Deployment),
		redirectBuildIDFromTo: make(map[string]string),
	}
}

func (r *TemporalWorkerReconciler) generateStatus(ctx context.Context, req ctrl.Request, workerDeploy temporaliov1alpha1.TemporalWorker) (*temporaliov1alpha1.TemporalWorkerStatus, error) {
	var (
		desiredBuildID, defaultBuildID string
		deployedBuildIDs               []string
		versions                       = newVersionedDeploymentCollection()
	)

	desiredBuildID = computeBuildID(workerDeploy.Spec)

	// GetVersionedDeployment managed worker deployments
	var childDeploys appsv1.DeploymentList
	if err := r.List(ctx, &childDeploys, client.InNamespace(req.Namespace), client.MatchingFields{deployOwnerKey: req.Name}); err != nil {
		return nil, fmt.Errorf("unable to list child deployments: %w", err)
	}
	// Sort deployments by creation timestamp
	sort.SliceStable(childDeploys.Items, func(i, j int) bool {
		return childDeploys.Items[i].ObjectMeta.CreationTimestamp.Before(&childDeploys.Items[j].ObjectMeta.CreationTimestamp)
	})
	// Track each deployment by build ID
	for _, childDeploy := range childDeploys.Items {
		if buildID, ok := childDeploy.GetLabels()[buildIDLabel]; ok {
			versions.AddDeployment(buildID, &childDeploy)
			deployedBuildIDs = append(deployedBuildIDs, buildID)
			continue
		}
		// TODO(jlegrone): implement some error handling (maybe a human deleted the label?)
	}

	// GetVersionedDeployment worker versioning rules via Temporal API
	rules, err := r.WorkflowServiceClient.GetWorkerVersioningRules(ctx, &workflowservice.GetWorkerVersioningRulesRequest{
		Namespace: workerDeploy.Spec.WorkerOptions.TemporalNamespace,
		TaskQueue: workerDeploy.Spec.WorkerOptions.TaskQueue,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get worker versioning rules: %w", err)
	}
	// Register redirect rules
	for _, rule := range rules.GetCompatibleRedirectRules() {
		versions.AddBuildIDRedirect(rule.GetRule().GetSourceBuildId(), rule.GetRule().GetTargetBuildId())
	}
	// Set default version and deprecated versions based on assignment rules
	for _, rule := range rules.GetAssignmentRules() {
		ruleTargetBuildID := rule.GetRule().GetTargetBuildId()
		// Register the rule
		versions.AddAssignmentRule(rule.GetRule())

		// TODO(jlegrone): Do rules need to be sorted by create time?

		// Set the default build ID if this is the first assignment rule without
		// a ramp.
		if defaultBuildID == "" && rule.GetRule().GetRamp() == nil {
			defaultBuildID = ruleTargetBuildID
			continue
		}
		// Don't mark the desired build ID as deprecated
		if ruleTargetBuildID == desiredBuildID {
			continue
		}
		// All rules after this point are not applicable since there is already a default build ID,
		// so assume they are deprecated.
		// TODO(jlegrone): Double check that this is correct. We also might need to delete unused
		//                 assignment rules during the plan phase.
		// TODO(jlegrone): Do we need to garbage collect assignment rules for versions that have no deployment?
	}

	// GetVersionedDeployment reachability info for all build IDs associated with the task queue via the Temporal API
	tq, err := r.WorkflowServiceClient.DescribeTaskQueue(ctx, &workflowservice.DescribeTaskQueueRequest{
		Namespace: workerDeploy.Spec.WorkerOptions.TemporalNamespace,
		TaskQueue: &taskqueue.TaskQueue{
			Name: workerDeploy.Spec.WorkerOptions.TaskQueue,
			//Kind: 0, // defaults to "normal"
		},
	})
	if err != nil {
		return nil, fmt.Errorf("unable to describe task queue: %w", err)
	}
	for buildID, info := range tq.GetVersionsInfo() {
		if err := versions.AddReachability(buildID, info); err != nil {
			return nil, fmt.Errorf("error computing reachability for build ID %q: %w", buildID, err)
		}
	}

	var deprecatedVersions []*temporaliov1alpha1.VersionedDeployment
	for _, buildID := range deployedBuildIDs {
		switch buildID {
		case desiredBuildID, defaultBuildID:
			continue
		}
		deprecatedVersions = append(deprecatedVersions, versions.GetVersionedDeployment(buildID))
	}

	return &temporaliov1alpha1.TemporalWorkerStatus{
		TargetVersion:      versions.GetVersionedDeployment(desiredBuildID),
		DefaultVersion:     versions.GetVersionedDeployment(defaultBuildID),
		DeprecatedVersions: deprecatedVersions,
	}, nil
}

type reachabilityInfo map[string]temporaliov1alpha1.ReachabilityStatus

func (r reachabilityInfo) getStatus(versionSet *temporaliov1alpha1.VersionedDeployment) temporaliov1alpha1.ReachabilityStatus {
	if versionSet == nil {
		return ""
	}

	var statuses []temporaliov1alpha1.ReachabilityStatus
	if s, ok := r[versionSet.BuildID]; ok {
		statuses = append(statuses, s)
	}
	//if s, ok := r[versionSet.DeployedBuildID]; ok {
	//	statuses = append(statuses, s)
	//}
	for _, buildID := range versionSet.CompatibleBuildIDs {
		if s, ok := r[buildID]; ok {
			statuses = append(statuses, s)
		}
	}

	return findHighestPriorityStatus(statuses)
}
