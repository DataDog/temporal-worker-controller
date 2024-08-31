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
}

func (c *versionedDeploymentCollection) GetDeployment(buildID string) (*appsv1.Deployment, bool) {
	if redirectedBuildID, ok := c.redirectBuildIDFromTo[buildID]; ok {
		return c.GetDeployment(redirectedBuildID)
	}
	d, ok := c.buildIDsToDeployments[buildID]
	return d, ok
}

func (c *versionedDeploymentCollection) AddRedirect(from, to string) {
	c.redirectBuildIDFromTo[from] = to
}

func (c *versionedDeploymentCollection) AddDeployment(buildID string, d *appsv1.Deployment) {
	c.buildIDsToDeployments[buildID] = d
}

func newVersionedDeploymentCollection() versionedDeploymentCollection {
	return versionedDeploymentCollection{
		buildIDsToDeployments: make(map[string]*appsv1.Deployment),
		redirectBuildIDFromTo: make(map[string]string),
	}
}

func (r *TemporalWorkerReconciler) generateStatus(ctx context.Context, req ctrl.Request, workerDeploy temporaliov1alpha1.TemporalWorker) (*temporaliov1alpha1.TemporalWorkerStatus, error) {
	var (
		targetVersion, defaultVersion *temporaliov1alpha1.VersionedDeployment
		deprecatedVersions            []*temporaliov1alpha1.VersionedDeployment
		desiredBuildID                = computeBuildID(workerDeploy.Spec)
		reachability                  = make(reachabilityInfo)
		versionedDeployments          = newVersionedDeploymentCollection()
	)

	// Get managed worker deployments
	var childDeploys appsv1.DeploymentList
	if err := r.List(ctx, &childDeploys, client.InNamespace(req.Namespace), client.MatchingFields{deployOwnerKey: req.Name}); err != nil {
		return nil, fmt.Errorf("unable to list child deployments: %w", err)
	}
	// Sort deployments by creation timestamp
	sort.SliceStable(childDeploys.Items, func(i, j int) bool {
		return childDeploys.Items[i].ObjectMeta.CreationTimestamp.Before(&childDeploys.Items[j].ObjectMeta.CreationTimestamp)
	})

	// Gather build IDs for each managed deployment
	for _, childDeploy := range childDeploys.Items {
		if buildID, ok := childDeploy.GetLabels()[buildIDLabel]; ok {
			childDeploy.GetObjectMeta().GetCreationTimestamp()
			versionedDeployments.AddDeployment(buildID, &childDeploy)
		} else {
			// TODO(jlegrone): implement some error handling (maybe a human deleted the label?)
		}
	}

	// Get all task queue version sets via Temporal API
	var (
		registeredBuildIDs = make(map[string]struct{})
	)

	rules, err := r.WorkflowServiceClient.GetWorkerVersioningRules(ctx, &workflowservice.GetWorkerVersioningRulesRequest{
		Namespace: workerDeploy.Spec.WorkerOptions.TemporalNamespace,
		TaskQueue: workerDeploy.Spec.WorkerOptions.TaskQueue,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to get worker versioning rules: %w", err)
	}
	// Register redirect rules
	for _, rule := range rules.GetCompatibleRedirectRules() {
		versionedDeployments.AddRedirect(rule.GetRule().GetSourceBuildId(), rule.GetRule().GetTargetBuildId())
	}

	var defaultVersionFound bool
	for _, rule := range rules.GetAssignmentRules() {
		// Exit the loop early if we've already found the default version; any assignment rules
		// after this point are not applicable.
		// TODO(jlegrone): Double check that this is correct. We also might need to delete unused
		//                 assignment rules during the plan phase.
		// TODO(jlegrone): Do rules need to be sorted by create time?
		if defaultVersionFound {
			break
		}

		// TODO(jlegrone): Do we need to garbage collect assignment rules for versions that have no deployment?
		if rule.GetRule().GetTargetBuildId() == desiredBuildID {
			// Set the target version
			if d, ok := versionedDeployments.GetDeployment(rule.GetRule().GetTargetBuildId()); ok {
				targetVersion = &temporaliov1alpha1.VersionedDeployment{
					Healthy:      false, // todo
					BuildID:      rule.GetRule().GetTargetBuildId(),
					Reachability: "", // This should be set later on
					Deployment:   newObjectRef(d),
				}
			}
		} else {
			ramp := rule.GetRule().GetRamp()
			// Assign the default version if this is the first with no ramp value
			if ramp == nil {
				defaultVersionFound = true
				if d, ok := versionedDeployments.GetDeployment(rule.GetRule().GetTargetBuildId()); ok {
					deprecatedVersions = append(deprecatedVersions, &temporaliov1alpha1.VersionedDeployment{
						// Some way of indicating this is the default?
						Healthy:      false, // todo
						BuildID:      rule.GetRule().GetTargetBuildId(),
						Reachability: temporaliov1alpha1.ReachabilityStatusReachable, // Should this be updated later on?
						Deployment:   newObjectRef(d),
					})
				}
			} else {
				// Add to deprecated versions
				if d, ok := versionedDeployments.GetDeployment(rule.GetRule().GetTargetBuildId()); ok {
					deprecatedVersions = append(deprecatedVersions, &temporaliov1alpha1.VersionedDeployment{
						// Some way of indicating this is the default?
						Healthy:      false, // todo
						BuildID:      rule.GetRule().GetTargetBuildId(),
						Reachability: temporaliov1alpha1.ReachabilityStatusReachable, // Should this be updated later on?
						Deployment:   newObjectRef(d),
					})
				}
			}
		}
	}

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
		registeredBuildIDs[buildID] = struct{}{}

		fmt.Println(buildID)
		info.GetTaskReachability()
		for _, typeInfo := range info.GetTypesInfo() {
			typeInfo.GetStats()
			typeInfo.GetPollers()
		}
	}

	// Handle unregistered deployments
	for id := range buildIDsToDeployments {
		// Skip if build ID is already registered with Temporal
		if _, ok := registeredBuildIDs[id]; ok {
			continue
		}
		// Otherwise, mark the build ID as unregistered
		reachability[id] = temporaliov1alpha1.ReachabilityStatusNotRegistered

		d := buildIDsToDeployments[id]

		// If the deployment is the desired build ID, then it should be the next version set.
		if id == desiredBuildID {
			// Check if deployment condition is "available"
			var healthy bool
			// TODO(jlegrone): do we need to sort conditions by timestamp?
			for _, c := range d.Status.Conditions {
				if c.Type == appsv1.DeploymentAvailable && c.Status == v1.ConditionTrue {
					healthy = true
				}
			}

			targetVersion = &temporaliov1alpha1.VersionedDeployment{
				Healthy:      healthy,
				BuildID:      desiredBuildID,
				Reachability: "", // This should be set later on
				Deployment:   newObjectRef(d),
			}
		} else {
			// Otherwise it should be deprecated and marked for deletion.
			deprecatedVersions = append(deprecatedVersions, &temporaliov1alpha1.VersionedDeployment{
				Reachability: "", // This should be set later on
				Deployment:   newObjectRef(d),
				BuildID:      id,
			})
		}
	}

	// Set next version set's build ID if it doesn't exist yet.
	// The deployment will be created by the next reconciliation loop.
	if targetVersion == nil && (defaultVersion == nil || defaultVersion.BuildID != desiredBuildID) {
		targetVersion = &temporaliov1alpha1.VersionedDeployment{
			Deployment:   nil,
			BuildID:      desiredBuildID,
			Reachability: "", // This should be set later on
		}
	}

	allVersionSets := append([]*temporaliov1alpha1.VersionedDeployment{}, deprecatedVersions...)
	if defaultVersion != nil {
		allVersionSets = append(allVersionSets, defaultVersion)
	}
	if targetVersion != nil {
		allVersionSets = append(allVersionSets, targetVersion)
	}
	for _, versionSet := range allVersionSets {
		s := reachability.getStatus(versionSet)
		versionSet.Reachability = s
	}

	return &temporaliov1alpha1.TemporalWorkerStatus{
		TargetVersion:      targetVersion,
		DefaultVersion:     defaultVersion,
		DeprecatedVersions: deprecatedVersions,
	}, nil
}

func getReachability(
	ctx context.Context,
	c workflowservice.WorkflowServiceClient,
	buildIDs []string,
	temporalNamespace string,
	taskQueue string,
) (reachabilityInfo, error) {
	result := make(reachabilityInfo)

	tq, err := c.DescribeTaskQueue(ctx, &workflowservice.DescribeTaskQueueRequest{
		Namespace: temporalNamespace,
		TaskQueue: &taskqueue.TaskQueue{
			Name: taskQueue,
			//Kind: 0, // defaults to "normal"
		},
		ReportTaskReachability: true,
	})
	if err != nil {
		return nil, fmt.Errorf("unable to describe task queue: %w", err)
	}

	for _, buildID := range buildIDs {
		versionInfo, ok := tq.GetVersionsInfo()[buildID]
		if !ok {
			result[buildID] = temporaliov1alpha1.ReachabilityStatusNotRegistered
			continue
		}
		switch versionInfo.GetTaskReachability() {
		case enums.BUILD_ID_TASK_REACHABILITY_REACHABLE:
			result[buildID] = temporaliov1alpha1.ReachabilityStatusReachable
		case enums.BUILD_ID_TASK_REACHABILITY_CLOSED_WORKFLOWS_ONLY:
			result[buildID] = temporaliov1alpha1.ReachabilityStatusClosedOnly
		case enums.BUILD_ID_TASK_REACHABILITY_UNREACHABLE:
			result[buildID] = temporaliov1alpha1.ReachabilityStatusUnreachable
		default:
			return nil, fmt.Errorf("unhandled build id reachability: %s", versionInfo.GetTaskReachability().String())
		}
	}

	return result, nil
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
