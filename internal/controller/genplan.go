// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package controller

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/api/workflowservice/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporaliov1alpha1 "github.com/DataDog/temporal-worker-controller/api/v1alpha1"
)

type plan struct {
	// Where to take actions

	TemporalNamespace string
	TaskQueue         string

	// Which actions to take

	DeleteDeployments []*appsv1.Deployment
	CreateDeployment  *appsv1.Deployment
	ScaleDeployments  map[*v1.ObjectReference]uint32
	// Register a new build ID as the default or with ramp
	UpdateVersionConfig *versionConfig
}

type versionConfig struct {
	// Token to use for conflict detection
	conflictToken []byte
	// build ID for which this config applies
	buildID string

	// One of rampPercentage OR setDefault must be set to a non-zero value.

	// Set this as the default build ID for all new executions
	setDefault bool
	// Acceptable values [0,100]
	rampPercentage uint8
}

func (r *TemporalWorkerReconciler) generatePlan(
	ctx context.Context,
	w *temporaliov1alpha1.TemporalWorker,
	rules *workflowservice.GetWorkerVersioningRulesResponse,
) (*plan, error) {
	plan := plan{
		TemporalNamespace: w.Spec.WorkerOptions.TemporalNamespace,
		TaskQueue:         w.Spec.WorkerOptions.TaskQueue,
		ScaleDeployments:  make(map[*v1.ObjectReference]uint32),
	}

	// Scale the active deployment if it doesn't match desired replicas
	if w.Status.DefaultVersion != nil && w.Status.DefaultVersion.Deployment != nil {
		defaultDeployment := w.Status.DefaultVersion.Deployment
		d, err := r.getDeployment(ctx, defaultDeployment)
		if err != nil {
			return nil, err
		}
		if d.Spec.Replicas != nil && *d.Spec.Replicas != *w.Spec.Replicas {
			plan.ScaleDeployments[defaultDeployment] = uint32(*w.Spec.Replicas)
		}
	}

	// TODO(jlegrone): generate warnings/events on the TemporalWorker resource when buildIDs are reachable
	//                 but have no corresponding Deployment.

	// Scale or delete deployments based on reachability
	for _, versionSet := range w.Status.DeprecatedVersions {
		if versionSet.Deployment == nil {
			// There's nothing we can do if the deployment was already deleted out of band.
			continue
		}

		d, err := r.getDeployment(ctx, versionSet.Deployment)
		if err != nil {
			return nil, err
		}

		switch versionSet.Reachability {
		case temporaliov1alpha1.ReachabilityStatusUnreachable:
			// Scale down unreachable deployments. We do this instead
			// of deleting them so that they can be scaled back up if
			// their build ID is promoted to default again (i.e. during
			// a rollback).
			if d.Spec.Replicas != nil && *d.Spec.Replicas != 0 {
				plan.ScaleDeployments[versionSet.Deployment] = 0
			}
		case temporaliov1alpha1.ReachabilityStatusClosedOnly:
			// TODO(jlegrone): Compute scale based on load? Or percentage of replicas?
			// Scale down queryable deployments
			if d.Spec.Replicas != nil && *d.Spec.Replicas != 1 {
				plan.ScaleDeployments[versionSet.Deployment] = 1
			}
		case temporaliov1alpha1.ReachabilityStatusNotRegistered:
			// Delete unregistered deployments
			plan.DeleteDeployments = append(plan.DeleteDeployments, d)
		}
	}

	desiredBuildID := computeBuildID(&w.Spec)

	if targetVersion := w.Status.TargetVersion; targetVersion != nil {
		if targetVersion.Deployment == nil {
			// Create new deployment from current pod template when it doesn't exist
			d, err := r.newDeployment(w, desiredBuildID)
			if err != nil {
				return nil, err
			}
			existing, _ := r.getDeployment(ctx, newObjectRef(d))
			if existing == nil {
				plan.CreateDeployment = d
			} else {
				// TODO(jlegrone): check for nil pointer
				plan.ScaleDeployments[newObjectRef(existing)] = uint32(*w.Spec.Replicas)
			}
		} else if targetVersion.BuildID != desiredBuildID {
			// Delete the latest (unregistered) deployment if the desired build ID has changed
			d, err := r.getDeployment(ctx, targetVersion.Deployment)
			if err != nil {
				return nil, err
			}
			plan.DeleteDeployments = append(plan.DeleteDeployments, d)
		} else {
			// Update version configuration
			plan.UpdateVersionConfig = getVersionConfigDiff(rules, w.Spec.RolloutStrategy, &w.Status)
			if plan.UpdateVersionConfig != nil {
				plan.UpdateVersionConfig.conflictToken = w.Status.VersionConflictToken
			}
		}
	}

	return &plan, nil
}

func getVersionConfigDiff(rules *workflowservice.GetWorkerVersioningRulesResponse, strategy *temporaliov1alpha1.RolloutStrategy, status *temporaliov1alpha1.TemporalWorkerStatus) *versionConfig {
	resp := getVersionConfig(strategy, status)
	if resp == nil {
		return nil
	}
	resp.buildID = status.TargetVersion.BuildID

	if assignmentRules := rules.GetAssignmentRules(); len(assignmentRules) > 0 {
		first := assignmentRules[0].GetRule()
		if first.GetTargetBuildId() == resp.buildID {
			ramp := first.GetPercentageRamp()
			// Skip making changes if build id is already the default
			if ramp == nil && resp.setDefault {
				return nil
			}
			// Skip making changes if ramp is already set to the desired value
			if ramp != nil && ramp.GetRampPercentage() == float32(resp.rampPercentage) {
				return nil
			}
		}
	}

	return resp
}

func getVersionConfig(strategy *temporaliov1alpha1.RolloutStrategy, status *temporaliov1alpha1.TemporalWorkerStatus) *versionConfig {
	// Do nothing if rollout strategy is unset or manual
	if strategy == nil || strategy.Manual != nil {
		return nil
	}
	// Do nothing if target version's deployment is not healthy yet
	if status.TargetVersion.HealthySince == nil {
		return nil
	}
	// Do nothing if build ID is already the default
	if status.TargetVersion.Reachability == temporaliov1alpha1.ReachabilityStatusReachable {
		return nil
	}

	// Set new default version in blue/green rollout mode as soon as next version is healthy
	if strategy.BlueGreen != nil {
		return &versionConfig{
			setDefault: true,
		}
	}

	// Determine the correct percentage ramp
	if prog := strategy.Progressive; prog != nil {
		var (
			healthyDuration    = time.Now().Sub(status.TargetVersion.HealthySince.Time)
			currentRamp        uint8
			totalPauseDuration time.Duration
		)
		for _, s := range prog.Steps {
			if s.RampPercentage != nil {
				currentRamp = *s.RampPercentage
			}
			// TODO(jlegrone): Correctly parse pause durations
			pauseDuration, err := time.ParseDuration(s.PauseDuration.String())
			if err != nil {
				continue
			}
			totalPauseDuration += pauseDuration
			if totalPauseDuration < healthyDuration {
				break
			}
		}
		// We've progressed through all steps; it should now be safe to update the default version
		if totalPauseDuration > healthyDuration {
			return &versionConfig{
				setDefault: true,
			}
		}
		// We haven't finished waiting for all steps; use the latest ramp value
		return &versionConfig{
			rampPercentage: currentRamp,
		}
	}

	return nil
}

func (r *TemporalWorkerReconciler) getDeployment(ctx context.Context, ref *v1.ObjectReference) (*appsv1.Deployment, error) {
	var d appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (r *TemporalWorkerReconciler) newDeployment(
	w *temporaliov1alpha1.TemporalWorker,
	buildID string,
) (*appsv1.Deployment, error) {
	d := newDeploymentWithoutOwnerRef(&w.TypeMeta, &w.ObjectMeta, &w.Spec, buildID)
	if err := ctrl.SetControllerReference(w, d, r.Scheme); err != nil {
		return nil, err
	}
	return d, nil
}

func newDeploymentWithoutOwnerRef(
	typeMeta *metav1.TypeMeta,
	objectMeta *metav1.ObjectMeta,
	spec *temporaliov1alpha1.TemporalWorkerSpec,
	buildID string,
) *appsv1.Deployment {
	labels := map[string]string{}
	// Merge labels from TemporalWorker with build ID
	for k, v := range spec.Selector.MatchLabels {
		labels[k] = v
	}
	labels[buildIDLabel] = buildID
	// Set pod labels
	if spec.Template.Labels == nil {
		spec.Template.Labels = labels
	} else {
		for k, v := range labels {
			spec.Template.Labels[k] = v
		}
	}

	for i, container := range spec.Template.Spec.Containers {
		container.Env = append(container.Env, v1.EnvVar{
			Name:  "TEMPORAL_NAMESPACE",
			Value: spec.WorkerOptions.TemporalNamespace,
		}, v1.EnvVar{
			Name:  "TEMPORAL_TASK_QUEUE",
			Value: spec.WorkerOptions.TaskQueue,
		}, v1.EnvVar{
			Name:  "TEMPORAL_BUILD_ID",
			Value: buildID,
		})
		spec.Template.Spec.Containers[i] = container
	}

	blockOwnerDeletion := true

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:                       fmt.Sprintf("%s-%s", objectMeta.Name, buildID),
			Namespace:                  objectMeta.Namespace,
			DeletionGracePeriodSeconds: nil,
			Labels:                     labels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         typeMeta.APIVersion,
				Kind:               typeMeta.Kind,
				Name:               objectMeta.Name,
				UID:                objectMeta.UID,
				BlockOwnerDeletion: &blockOwnerDeletion,
				Controller:         nil,
			}},
			// TODO(jlegrone): Add finalizer managed by the controller in order to prevent
			//                 deleting deployments that are still reachable.
			Finalizers: nil,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template:        spec.Template,
			MinReadySeconds: spec.MinReadySeconds,
		},
	}
}
