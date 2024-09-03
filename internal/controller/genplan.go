// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package controller

import (
	"context"
	"fmt"

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
	typeMeta *metav1.TypeMeta,
	objectMeta *metav1.ObjectMeta,
	desiredState *temporaliov1alpha1.TemporalWorkerSpec,
	observedState *temporaliov1alpha1.TemporalWorkerStatus,
) (*plan, error) {
	plan := plan{
		TemporalNamespace: desiredState.WorkerOptions.TemporalNamespace,
		TaskQueue:         desiredState.WorkerOptions.TaskQueue,
		ScaleDeployments:  make(map[*v1.ObjectReference]uint32),
	}

	// Scale the active deployment if it doesn't match desired replicas
	if observedState.DefaultVersion != nil && observedState.DefaultVersion.Deployment != nil {
		defaultDeployment := observedState.DefaultVersion.Deployment
		d, err := r.getDeployment(ctx, defaultDeployment)
		if err != nil {
			return nil, err
		}
		if d.Spec.Replicas != nil && *d.Spec.Replicas != *desiredState.Replicas {
			plan.ScaleDeployments[defaultDeployment] = uint32(*desiredState.Replicas)
		}
	}

	// TODO(jlegrone): generate warnings/events on the TemporalWorker resource when buildIDs are reachable
	//                 but have no corresponding Deployment.

	// Scale or delete deployments based on reachability
	for _, versionSet := range observedState.DeprecatedVersions {
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

	desiredBuildID := computeBuildID(desiredState)

	if targetVersion := observedState.TargetVersion; targetVersion != nil {
		if targetVersion.Deployment == nil {
			// Create new deployment from current pod template when it doesn't exist
			d, err := r.newDeployment(typeMeta, objectMeta, desiredState, desiredBuildID)
			if err != nil {
				return nil, err
			}
			existing, _ := r.getDeployment(ctx, newObjectRef(d))
			if existing == nil {
				plan.CreateDeployment = d
			} else {
				plan.ScaleDeployments[newObjectRef(existing)] = uint32(*desiredState.Replicas)
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
			plan.UpdateVersionConfig = getVersionConfig(desiredState.RolloutStrategy, observedState)
			if plan.UpdateVersionConfig != nil {
				plan.UpdateVersionConfig.conflictToken = observedState.VersionConflictToken
			}
		}
	}

	return &plan, nil
}

func getVersionConfig(strategy *temporaliov1alpha1.RolloutStrategy, status *temporaliov1alpha1.TemporalWorkerStatus) *versionConfig {
	// Do nothing if rollout strategy is unset or manual
	if strategy == nil || strategy.Manual != nil {
		return nil
	}
	// Do nothing if target version's deployment is not healthy yet
	if status.TargetVersion.Healthy {
		return nil
	}

	// Set new default version in blue/green rollout mode as soon as next version is healthy
	if strategy.BlueGreen != nil {
		return &versionConfig{
			buildID:    status.TargetVersion.BuildID,
			setDefault: true,
		}
	}

	// Determine the correct percentage ramp
	if prog := strategy.Progressive; prog != nil {
		// TODO(jlegrone): break if wait duration not elapsed
		var currentRamp uint8
		for _, s := range prog.Steps {
			if s.RampPercentage != nil {
				currentRamp = *s.RampPercentage
			}
		}
		return &versionConfig{
			buildID:        status.TargetVersion.BuildID,
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
	typeMeta *metav1.TypeMeta,
	objectMeta *metav1.ObjectMeta,
	spec *temporaliov1alpha1.TemporalWorkerSpec,
	buildID string,
) (*appsv1.Deployment, error) {
	d := newDeploymentWithoutOwnerRef(typeMeta, objectMeta, spec, buildID)
	if err := ctrl.SetControllerReference(objectMeta, d, r.Scheme); err != nil {
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
