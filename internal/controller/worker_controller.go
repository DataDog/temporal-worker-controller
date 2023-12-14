package controller

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-logr/logr"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	temporaliov1alpha1 "github.com/temporalio/worker-controller/api/v1alpha1"
	"github.com/temporalio/worker-controller/internal/k8s.io/utils"
)

var (
	apiGVStr = temporaliov1alpha1.GroupVersion.String()
)

const (
	// TODO(jlegrone): add this everywhere
	deployOwnerKey = ".metadata.controller"
	buildIDLabel   = "temporal.io/build-id"
)

// TemporalWorkerReconciler reconciles a TemporalWorker object
type TemporalWorkerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// TODO(jlegrone): Support multiple Temporal servers
	WorkflowServiceClient workflowservice.WorkflowServiceClient
}

//+kubebuilder:rbac:groups=temporal.io,resources=temporalworkers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=temporal.io,resources=temporalworkers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=temporal.io,resources=temporalworkers/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *TemporalWorkerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Fetch the worker deployment
	var workerDeploy temporaliov1alpha1.TemporalWorker
	if err := r.Get(ctx, req.NamespacedName, &workerDeploy); err != nil {
		l.Error(err, "unable to fetch TemporalWorker")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Compute a new status from k8s and temporal state
	status, err := r.generateStatus(ctx, req, workerDeploy)
	if err != nil {
		return ctrl.Result{}, err
	}
	workerDeploy.Status = status
	if err := r.Status().Update(ctx, &workerDeploy); err != nil {
		// Ignore "object has been modified" errors, since we'll just re-fetch
		// on the next reconciliation loop.
		if apierrors.IsConflict(err) {
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Second,
			}, nil
		}
		l.Error(err, "unable to update TemporalWorker status")
		return ctrl.Result{}, err
	}

	// Generate a plan to get to desired spec from current status
	plan, err := r.generatePlan(ctx, status, workerDeploy)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Execute the plan, handling any errors
	if err := r.executePlan(ctx, l, plan); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		Requeue: true,
		// TODO(jlegrone): Consider increasing this value if the only thing we need to check for is unreachable versions.
		RequeueAfter: 10 * time.Second,
	}, nil
}

func (r *TemporalWorkerReconciler) generateStatus(ctx context.Context, req ctrl.Request, workerDeploy temporaliov1alpha1.TemporalWorker) (temporaliov1alpha1.TemporalWorkerStatus, error) {
	var (
		status         temporaliov1alpha1.TemporalWorkerStatus
		desiredBuildID = computeBuildID(workerDeploy.Spec)
		reachability   = make(reachabilityInfo)
	)

	// Get managed worker deployments
	var childDeploys appsv1.DeploymentList
	if err := r.List(ctx, &childDeploys, client.InNamespace(req.Namespace), client.MatchingFields{deployOwnerKey: req.Name}); err != nil {
		return status, fmt.Errorf("unable to list child deployments: %w", err)
	}
	// Sort deployments by creation timestamp
	sort.SliceStable(childDeploys.Items, func(i, j int) bool {
		return childDeploys.Items[i].ObjectMeta.CreationTimestamp.Before(&childDeploys.Items[j].ObjectMeta.CreationTimestamp)
	})

	// Gather build IDs for each managed deployment
	buildIDsToDeployments := map[string]int{}
	for i, childDeploy := range childDeploys.Items {
		if buildID, ok := childDeploy.GetLabels()[buildIDLabel]; ok {
			childDeploy.GetObjectMeta().GetCreationTimestamp()
			buildIDsToDeployments[buildID] = i
		} else {
			// TODO(jlegrone): implement some error handling (maybe a human deleted the label?)
		}
	}

	// Get all task queue version sets via Temporal API
	var (
		registeredBuildIDs = make(map[string]struct{})
	)
	setsResponse, err := r.WorkflowServiceClient.GetWorkerBuildIdCompatibility(ctx, &workflowservice.GetWorkerBuildIdCompatibilityRequest{
		Namespace: workerDeploy.Spec.WorkerOptions.TemporalNamespace,
		TaskQueue: workerDeploy.Spec.WorkerOptions.TaskQueue,
		MaxSets:   0, // 0 returns all sets.
	})
	if err != nil {
		return status, fmt.Errorf("unable to get worker version sets: %w", err)
	}
	for i, majorVersionSet := range setsResponse.GetMajorVersionSets() {
		ids := majorVersionSet.GetBuildIds()
		if len(ids) > 0 {
			var (
				d               *appsv1.Deployment
				deployedBuildID string
			)
			for _, buildID := range ids {
				if deploymentIndex, ok := buildIDsToDeployments[buildID]; ok {
					d = &childDeploys.Items[deploymentIndex]
					// TODO(jlegrone): Handle case where multiple deployments might exist for major version set, eg.
					//                 due to an out of band major version set merge.
					deployedBuildID = buildID
				}
			}

			set := temporaliov1alpha1.CompatibleVersionSet{
				ReachabilityStatus: "", // populated further down in the function
				DeployedBuildID:    deployedBuildID,
				DefaultBuildID:     ids[len(ids)-1],
				InactiveBuildIDs:   ids[:len(ids)-1],
			}
			if d != nil {
				set.Deployment = newObjectRef(*d)
			}

			// Note default major version set -- this is the last item in the list.
			if i == len(setsResponse.GetMajorVersionSets())-1 {
				status.DefaultVersionSet = &set
			} else {
				if set.DefaultBuildID == desiredBuildID {
					// Handle rollback case
					status.NextVersionSet = &temporaliov1alpha1.NextVersionSet{
						VersionSet: &set,
						// TODO(jlegrone): actually perform check
						DeploymentHealthy: true,
					}
				} else {
					status.DeprecatedVersionSets = append(status.DeprecatedVersionSets, &set)
				}
			}

			for _, id := range ids {
				registeredBuildIDs[id] = struct{}{}
			}
		}
	}

	if len(registeredBuildIDs) > 0 {
		// Get build ID reachability via Temporal API
		ids := make([]string, 0, len(registeredBuildIDs))
		for id := range registeredBuildIDs {
			ids = append(ids, id)
		}

		r, err := getReachability(
			ctx,
			r.WorkflowServiceClient,
			ids,
			workerDeploy.Spec.WorkerOptions.TemporalNamespace,
			workerDeploy.Spec.WorkerOptions.TaskQueue,
		)
		if err != nil {
			return temporaliov1alpha1.TemporalWorkerStatus{}, fmt.Errorf("unable to fetch worker task reachability: %w", err)
		}
		reachability = r
	}

	// Handle unregistered deployments
	for id := range buildIDsToDeployments {
		// Skip if build ID is already registered with Temporal
		if _, ok := registeredBuildIDs[id]; ok {
			continue
		}
		// Otherwise, mark the build ID as unregistered
		reachability[id] = temporaliov1alpha1.ReachabilityStatusNotRegistered

		d := childDeploys.Items[buildIDsToDeployments[id]]

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

			status.NextVersionSet = &temporaliov1alpha1.NextVersionSet{
				VersionSet: &temporaliov1alpha1.CompatibleVersionSet{
					Deployment:         newObjectRef(d),
					ReachabilityStatus: "", // This should be set later on
					DeployedBuildID:    desiredBuildID,
				},
				DeploymentHealthy: healthy,
			}
		} else {
			// Otherwise it should be deprecated and marked for deletion.
			status.DeprecatedVersionSets = append(status.DeprecatedVersionSets, &temporaliov1alpha1.CompatibleVersionSet{
				ReachabilityStatus: "", // This should be set later on
				Deployment:         newObjectRef(d),
				DeployedBuildID:    id,
				DefaultBuildID:     id,
			})
		}
	}

	// Set next version set's build ID if it doesn't exist yet.
	// The deployment will be created by the next reconciliation loop.
	if status.NextVersionSet == nil && (status.DefaultVersionSet == nil || status.DefaultVersionSet.DeployedBuildID != desiredBuildID) {
		status.NextVersionSet = &temporaliov1alpha1.NextVersionSet{
			VersionSet: &temporaliov1alpha1.CompatibleVersionSet{
				Deployment:         nil,
				DefaultBuildID:     desiredBuildID,
				ReachabilityStatus: "", // This should be set later on
			},
		}
	}

	allVersionSets := append([]*temporaliov1alpha1.CompatibleVersionSet{}, status.DeprecatedVersionSets...)
	if status.DefaultVersionSet != nil {
		allVersionSets = append(allVersionSets, status.DefaultVersionSet)
	}
	if status.NextVersionSet != nil {
		allVersionSets = append(allVersionSets, status.NextVersionSet.VersionSet)
	}
	for _, versionSet := range allVersionSets {
		s := reachability.getStatus(versionSet)
		versionSet.ReachabilityStatus = s
	}

	return status, nil
}

type reachabilityInfo map[string]temporaliov1alpha1.ReachabilityStatus

func (r reachabilityInfo) getStatus(versionSet *temporaliov1alpha1.CompatibleVersionSet) temporaliov1alpha1.ReachabilityStatus {
	if versionSet == nil {
		return ""
	}

	var statuses []temporaliov1alpha1.ReachabilityStatus
	if s, ok := r[versionSet.DefaultBuildID]; ok {
		statuses = append(statuses, s)
	}
	if s, ok := r[versionSet.DeployedBuildID]; ok {
		statuses = append(statuses, s)
	}
	for _, buildID := range versionSet.InactiveBuildIDs {
		if s, ok := r[buildID]; ok {
			statuses = append(statuses, s)
		}
	}

	return findHighestPriorityStatus(statuses)
}

func getReachability(
	ctx context.Context,
	c workflowservice.WorkflowServiceClient,
	buildIDs []string,
	temporalNamespace string,
	taskQueue string,
) (reachabilityInfo, error) {
	var (
		result = make(reachabilityInfo)
		// TODO(jlegrone): Make batch size configurable based on server limits
		buildIDBatches = groupIntoBatches(buildIDs, 4)
	)

	for _, batch := range buildIDBatches {
		reachability, err := c.GetWorkerTaskReachability(ctx, &workflowservice.GetWorkerTaskReachabilityRequest{
			Namespace: temporalNamespace,
			// TODO(jlegrone): Add guard or implement pagination if list of build IDs is larger than server limit
			BuildIds:   batch,
			TaskQueues: []string{taskQueue},
			// Delete deployments if no workflows are open
			Reachability: enums.TASK_REACHABILITY_OPEN_WORKFLOWS,
			// Don't delete deployments if workflows need to be queried
			//Reachability: enums.TASK_REACHABILITY_EXISTING_WORKFLOWS,
		})
		if err != nil {
			return nil, fmt.Errorf("unable to fetch worker task reachability: %w", err)
		}
		for _, r := range reachability.GetBuildIdReachability() {
			for _, tqr := range r.GetTaskQueueReachability() {
				reachabilityTypes := tqr.GetReachability()
				if len(reachabilityTypes) == 0 {
					result[r.GetBuildId()] = temporaliov1alpha1.ReachabilityStatusUnreachable
				}
				for _, reachabilityType := range reachabilityTypes {
					switch reachabilityType {
					case enums.TASK_REACHABILITY_NEW_WORKFLOWS,
						enums.TASK_REACHABILITY_EXISTING_WORKFLOWS,
						enums.TASK_REACHABILITY_OPEN_WORKFLOWS:
						result[r.GetBuildId()] = temporaliov1alpha1.ReachabilityStatusActive
					case enums.TASK_REACHABILITY_CLOSED_WORKFLOWS:
						result[r.GetBuildId()] = temporaliov1alpha1.ReachabilityStatusQueryable
					default:
						return nil, fmt.Errorf("unhandled build id reachability: %s", reachabilityType.String())
					}
				}
			}
		}
	}

	return result, nil
}

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
	if observedState.DefaultVersionSet != nil && observedState.DefaultVersionSet.Deployment != nil {
		defaultDeployment := observedState.DefaultVersionSet.Deployment
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
	for _, versionSet := range observedState.DeprecatedVersionSets {
		if versionSet.Deployment == nil {
			// There's nothing we can do if the deployment was already deleted out of band.
			continue
		}

		d, err := r.getDeployment(ctx, *versionSet.Deployment)
		if err != nil {
			return nil, err
		}

		switch versionSet.ReachabilityStatus {
		case temporaliov1alpha1.ReachabilityStatusUnreachable:
			// Scale down unreachable deployments. We do this instead
			// of deleting them so that they can be scaled back up if
			// their build ID is promoted to default again (i.e. during
			// a rollback).
			if d.Spec.Replicas != nil && *d.Spec.Replicas != 0 {
				plan.ScaleDeployments[versionSet.Deployment] = 0
			}
		case temporaliov1alpha1.ReachabilityStatusQueryable:
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

	if nextVersionSet := observedState.NextVersionSet; nextVersionSet != nil {
		if nextVersionSet.VersionSet.Deployment == nil {
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
		} else if nextVersionSet.VersionSet.DeployedBuildID != desiredBuildID {
			// Delete the latest (unregistered) deployment if the desired build ID has changed
			d, err := r.getDeployment(ctx, *nextVersionSet.VersionSet.Deployment)
			if err != nil {
				return nil, err
			}
			plan.DeleteDeployments = append(plan.DeleteDeployments, d)
		} else if nextVersionSet.DeploymentHealthy {
			// Register the latest deployment as default version set if it is healthy
			switch nextVersionSet.VersionSet.ReachabilityStatus {
			case temporaliov1alpha1.ReachabilityStatusActive,
				temporaliov1alpha1.ReachabilityStatusQueryable,
				temporaliov1alpha1.ReachabilityStatusUnreachable:
				plan.PromoteExistingVersion = desiredBuildID
			case temporaliov1alpha1.ReachabilityStatusNotRegistered:
				plan.RegisterDefaultVersion = desiredBuildID
			default:
				return nil, fmt.Errorf("unhandled reachability status: %s", nextVersionSet.VersionSet.ReachabilityStatus)
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

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalWorkerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &appsv1.Deployment{}, deployOwnerKey, func(rawObj client.Object) []string {
		// grab the job object, extract the owner...
		deploy := rawObj.(*appsv1.Deployment)
		owner := metav1.GetControllerOf(deploy)

		if owner == nil {
			return nil
		}
		// ...make sure it's a TemporalWorker...
		// TODO(jlegrone): double check apiGVStr has the correct value
		if owner.APIVersion != apiGVStr || owner.Kind != "TemporalWorker" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&temporaliov1alpha1.TemporalWorker{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func computeBuildID(spec temporaliov1alpha1.TemporalWorkerSpec) string {
	return utils.ComputeHash(&spec.Template, nil)
}

func (r *TemporalWorkerReconciler) newDeployment(wd temporaliov1alpha1.TemporalWorker, buildID string) (*appsv1.Deployment, error) {
	d := newDeploymentWithoutOwnerRef(wd, buildID)
	if err := ctrl.SetControllerReference(&wd, d, r.Scheme); err != nil {
		return nil, err
	}
	return d, nil
}

func newDeploymentWithoutOwnerRef(deployment temporaliov1alpha1.TemporalWorker, buildID string) *appsv1.Deployment {
	labels := map[string]string{}
	// Merge labels from TemporalWorker with build ID
	for k, v := range deployment.Spec.Selector.MatchLabels {
		labels[k] = v
	}
	labels[buildIDLabel] = buildID
	// Set pod labels
	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = labels
	} else {
		for k, v := range labels {
			deployment.Spec.Template.Labels[k] = v
		}
	}

	for i, container := range deployment.Spec.Template.Spec.Containers {
		container.Env = append(container.Env, v1.EnvVar{
			Name:  "TEMPORAL_NAMESPACE",
			Value: deployment.Spec.WorkerOptions.TemporalNamespace,
		}, v1.EnvVar{
			Name:  "TEMPORAL_TASK_QUEUE",
			Value: deployment.Spec.WorkerOptions.TaskQueue,
		}, v1.EnvVar{
			Name:  "TEMPORAL_BUILD_ID",
			Value: buildID,
		})
		deployment.Spec.Template.Spec.Containers[i] = container
	}

	blockOwnerDeletion := true

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:                       fmt.Sprintf("%s-%s", deployment.ObjectMeta.Name, buildID),
			Namespace:                  deployment.ObjectMeta.Namespace,
			DeletionGracePeriodSeconds: nil,
			Labels:                     labels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         deployment.TypeMeta.APIVersion,
				Kind:               deployment.TypeMeta.Kind,
				Name:               deployment.ObjectMeta.Name,
				UID:                deployment.ObjectMeta.UID,
				BlockOwnerDeletion: &blockOwnerDeletion,
				Controller:         nil,
			}},
			Finalizers: nil,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: deployment.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template:        deployment.Spec.Template,
			MinReadySeconds: deployment.Spec.MinReadySeconds,
		},
	}
}

func newObjectRef(d appsv1.Deployment) *v1.ObjectReference {
	return &v1.ObjectReference{
		Kind:            d.Kind,
		Namespace:       d.Namespace,
		Name:            d.Name,
		UID:             d.UID,
		APIVersion:      d.APIVersion,
		ResourceVersion: d.ResourceVersion,
	}
}
