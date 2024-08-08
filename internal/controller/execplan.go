package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"go.temporal.io/api/workflowservice/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *TemporalWorkerReconciler) executePlan(ctx context.Context, l logr.Logger, p *plan) error {
	// Create deployment
	if p.CreateDeployment != nil {
		l.Info("creating deployment", "deployment", p.CreateDeployment)
		if err := r.Create(ctx, p.CreateDeployment); err != nil {
			l.Error(err, "unable to create deployment", "deployment", p.CreateDeployment)
			return err
		}
	}

	// Delete deployments
	for _, d := range p.DeleteDeployments {
		l.Info("deleting deployment", "deployment", d)
		if err := r.Delete(ctx, d); err != nil {
			l.Error(err, "unable to delete deployment", "deployment", d)
			return err
		}
	}
	// Scale deployments
	for d, replicas := range p.ScaleDeployments {
		l.Info("scaling deployment", "deployment", d, "replicas", replicas)
		dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Namespace:       d.Namespace,
			Name:            d.Name,
			ResourceVersion: d.ResourceVersion,
			UID:             d.UID,
		}}

		scale := &autoscalingv1.Scale{Spec: autoscalingv1.ScaleSpec{Replicas: int32(replicas)}}
		if err := r.Client.SubResource("scale").Update(ctx, dep, client.WithSubResourceBody(scale)); err != nil {
			l.Error(err, "unable to scale deployment", "deployment", d, "replicas", replicas)
			return fmt.Errorf("unable to scale deployment: %w", err)
		}
	}

	// Register default version set
	if p.RegisterDefaultVersion != "" {
		// Check out API here:
		// https://github.com/temporalio/api/blob/cfa1a15b960920a47de8ec272873a4ee4db574c4/temporal/api/workflowservice/v1/request_response.proto#L1073-L1132
		l.Info("registering new default version set", "buildID", p.RegisterDefaultVersion)
		if _, err := r.WorkflowServiceClient.UpdateWorkerBuildIdCompatibility(ctx, &workflowservice.UpdateWorkerBuildIdCompatibilityRequest{
			Namespace: p.TemporalNamespace,
			TaskQueue: p.TaskQueue,
			Operation: &workflowservice.UpdateWorkerBuildIdCompatibilityRequest_AddNewBuildIdInNewDefaultSet{
				AddNewBuildIdInNewDefaultSet: p.RegisterDefaultVersion,
			},
		}); err != nil {
			return fmt.Errorf("unable to register default version set: %w", err)
		}
	} else if p.PromoteExistingVersion != "" {
		l.Info("promoting existing version set", "buildID", p.PromoteExistingVersion)
		if _, err := r.WorkflowServiceClient.UpdateWorkerBuildIdCompatibility(ctx, &workflowservice.UpdateWorkerBuildIdCompatibilityRequest{
			Namespace: p.TemporalNamespace,
			TaskQueue: p.TaskQueue,
			Operation: &workflowservice.UpdateWorkerBuildIdCompatibilityRequest_PromoteSetByBuildId{
				PromoteSetByBuildId: p.PromoteExistingVersion,
			},
		}); err != nil {
			return fmt.Errorf("unable to promote version set: %w", err)
		}
	}

	return nil
}
