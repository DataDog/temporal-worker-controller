package controller

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporaliov1alpha1 "github.com/DataDog/temporal-worker-controller/api/v1alpha1"
)

type plan struct {
	DeleteDeployments []*appsv1.Deployment
	CreateDeployment  *appsv1.Deployment
	ScaleDeployments  map[*v1.ObjectReference]uint32
	// Register a new build ID as the default
	RegisterDefaultVersion string
	// Promote an existing build ID to the default
	PromoteExistingVersion string
	TemporalNamespace      string
	TaskQueue              string
}

func (r *TemporalWorkerReconciler) generatePlan(
	ctx context.Context,
	observedState temporaliov1alpha1.TemporalWorkerStatus,
	desiredState temporaliov1alpha1.TemporalWorker,
) (*plan, error) {
	plan := plan{
		TemporalNamespace: desiredState.Spec.WorkerOptions.TemporalNamespace,
		TaskQueue:         desiredState.Spec.WorkerOptions.TaskQueue,
		ScaleDeployments:  make(map[*v1.ObjectReference]uint32),
	}

	// Scale the active deployment if it doesn't match desired replicas
	if observedState.DefaultVersion != nil && observedState.DefaultVersion.Deployment != nil {
		defaultDeployment := observedState.DefaultVersion.Deployment
		d, err := r.getDeployment(ctx, *defaultDeployment)
		if err != nil {
			return nil, err
		}
		if d.Spec.Replicas != nil && *d.Spec.Replicas != *desiredState.Spec.Replicas {
			plan.ScaleDeployments[defaultDeployment] = uint32(*desiredState.Spec.Replicas)
		}
	}

	// TODO(jlegrone): generate warnings/events on the TemporalWorker resource when version sets exist with no
	//                 corresponding Deployment.

	// Scale or delete deployments based on reachability
	for _, versionSet := range observedState.DeprecatedVersions {
		if versionSet.Deployment == nil {
			// There's nothing we can do if the deployment was already deleted out of band.
			continue
		}

		d, err := r.getDeployment(ctx, *versionSet.Deployment)
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

	desiredBuildID := computeBuildID(desiredState.Spec)

	if nextVersionSet := observedState.TargetVersion; nextVersionSet != nil {
		if nextVersionSet.Deployment == nil {
			// Create new deployment from current pod template when it doesn't exist
			d, err := r.newDeployment(desiredState, desiredBuildID)
			if err != nil {
				return nil, err
			}
			existing, _ := r.getDeployment(ctx, *newObjectRef(*d))
			if existing == nil {
				plan.CreateDeployment = d
			} else {
				plan.ScaleDeployments[newObjectRef(*existing)] = uint32(*desiredState.Spec.Replicas)
			}
		} else if nextVersionSet.BuildID != desiredBuildID {
			// Delete the latest (unregistered) deployment if the desired build ID has changed
			d, err := r.getDeployment(ctx, *nextVersionSet.Deployment)
			if err != nil {
				return nil, err
			}
			plan.DeleteDeployments = append(plan.DeleteDeployments, d)
		} else if nextVersionSet.Healthy {
			// Register the latest deployment as default version set if it is healthy
			switch nextVersionSet.Reachability {
			case temporaliov1alpha1.ReachabilityStatusReachable,
				temporaliov1alpha1.ReachabilityStatusClosedOnly,
				temporaliov1alpha1.ReachabilityStatusUnreachable:
				plan.PromoteExistingVersion = desiredBuildID
			case temporaliov1alpha1.ReachabilityStatusNotRegistered:
				plan.RegisterDefaultVersion = desiredBuildID
			default:
				return nil, fmt.Errorf("unhandled reachability status: %s", nextVersionSet.Reachability)
			}
		}
	}

	return &plan, nil
}

func (r *TemporalWorkerReconciler) getDeployment(ctx context.Context, ref v1.ObjectReference) (*appsv1.Deployment, error) {
	var d appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: ref.Namespace,
		Name:      ref.Name,
	}, &d); err != nil {
		return nil, err
	}
	return &d, nil
}
