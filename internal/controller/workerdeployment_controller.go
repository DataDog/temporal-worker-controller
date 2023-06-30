/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	temporaliov1alpha1 "github.com/temporalio/worker-controller/api/v1alpha1"
)

var (
	deployOwnerKey    = ".metadata.controller"
	buildIDAnnotation = "temporal.io/build-id"
	apiGVStr          = appsv1.SchemeGroupVersion.String()
)

// WorkerDeploymentReconciler reconciles a WorkerDeployment object
type WorkerDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// TODO(jlegrone): Support multiple Temporal servers
	WorkflowServiceClient workflowservice.WorkflowServiceClient
}

//+kubebuilder:rbac:groups=temporal.io.temporal.io,resources=workerdeployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=temporal.io.temporal.io,resources=workerdeployments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=temporal.io.temporal.io,resources=workerdeployments/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *WorkerDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	// Fetch the worker deployment
	var workerDeploy temporaliov1alpha1.WorkerDeployment
	if err := r.Get(ctx, req.NamespacedName, &workerDeploy); err != nil {
		l.Error(err, "unable to fetch WorkerDeployment")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Compute a new status from k8s and temporal state
	status, err := r.generateStatus(ctx, req, l, workerDeploy)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Generate a plan to get to desired spec from current status
	plan := generatePlan(req.NamespacedName, status, workerDeploy.Spec)

	// Execute the plan, handling any errors
	if err := r.executePlan(ctx, req, l, plan); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{
		Requeue: true,
		// TODO(jlegrone): Consider increasing this value if the only thing we need to check for is unreachable versions.
		RequeueAfter: 10 * time.Second,
	}, nil
}

func (r *WorkerDeploymentReconciler) generateStatus(ctx context.Context, req ctrl.Request, l logr.Logger, workerDeploy temporaliov1alpha1.WorkerDeployment) (temporaliov1alpha1.WorkerDeploymentStatus, error) {
	// Get managed worker deployments
	var childDeploys appsv1.DeploymentList
	if err := r.List(ctx, &childDeploys, client.InNamespace(req.Namespace), client.MatchingFields{deployOwnerKey: req.Name}); err != nil {
		l.Error(err, "unable to list child Deployments")
		return temporaliov1alpha1.WorkerDeploymentStatus{}, err
	}

	// Gather build IDs for each managed deployment
	buildIDsToDeployments := map[string]int{}
	for i, childDeploy := range childDeploys.Items {
		if buildID, ok := childDeploy.GetAnnotations()[buildIDAnnotation]; ok {
			buildIDsToDeployments[buildID] = i
		} else {
			// TODO(jlegrone): implement some error handling (maybe a human deleted the annotation?)
		}
	}

	// Get all task queue version sets via Temporal API
	var (
		allBuildIDs, majorBuildIDs []string
	)
	setsResponse, err := r.WorkflowServiceClient.GetWorkerBuildIdCompatibility(ctx, &workflowservice.GetWorkerBuildIdCompatibilityRequest{
		Namespace: workerDeploy.Spec.WorkerOptions.TemporalNamespace,
		TaskQueue: workerDeploy.Spec.WorkerOptions.TaskQueue,
		MaxSets:   0, // 0 returns all sets.
	})
	if err != nil {
		l.Error(err, "unable to get worker version sets")
		return temporaliov1alpha1.WorkerDeploymentStatus{}, err
	}
	for _, majorVersionSet := range setsResponse.GetMajorVersionSets() {
		if ids := majorVersionSet.GetBuildIds(); len(ids) > 0 {
			allBuildIDs = append(allBuildIDs, ids...)
			// The last build ID represents the current default for the major version set.
			majorBuildIDs = append(majorBuildIDs, ids[len(ids)-1])
		}
	}

	// Get build ID reachability via Temporal API
	reachableBuildIDs := map[string]struct{}{}
	reachability, err := r.WorkflowServiceClient.GetWorkerTaskReachability(ctx, &workflowservice.GetWorkerTaskReachabilityRequest{
		Namespace: workerDeploy.Spec.WorkerOptions.TemporalNamespace,
		// TODO(jlegrone): Add guard or implement pagination if list of build IDs is larger than server limit
		BuildIds:     allBuildIDs,
		TaskQueues:   []string{workerDeploy.Spec.WorkerOptions.TaskQueue},
		Reachability: enums.TASK_REACHABILITY_NEW_WORKFLOWS,
	})
	if err != nil {
		l.Error(err, "unable to fetch worker task reachability")
		return temporaliov1alpha1.WorkerDeploymentStatus{}, err
	}
	for _, r := range reachability.GetBuildIdReachability() {
		r.GetBuildId() // should map to a live deployment resource

		for _, tqr := range r.GetTaskQueueReachability() {
			tqr.GetTaskQueue() // should match worker's task queue

			for _, reachabilityType := range tqr.GetReachability() {
				switch reachabilityType {
				case enums.TASK_REACHABILITY_NEW_WORKFLOWS,
					enums.TASK_REACHABILITY_EXISTING_WORKFLOWS,
					enums.TASK_REACHABILITY_OPEN_WORKFLOWS,
					enums.TASK_REACHABILITY_CLOSED_WORKFLOWS:
					reachableBuildIDs[r.GetBuildId()] = struct{}{}
				default:
					// TODO(jlegrone): fail or emit an error log for unhandled reachability status
				}
			}
		}
	}

	// Find all unreachable deployments
	var unreachableDeployments []int
	for _, buildID := range allBuildIDs {
		if _, ok := reachableBuildIDs[buildID]; !ok {
			if deploymentIndex, ok := buildIDsToDeployments[buildID]; ok {
				unreachableDeployments = append(unreachableDeployments, deploymentIndex)
			}
		}
	}

	// TODO(jlegrone): fill in the status
	return temporaliov1alpha1.WorkerDeploymentStatus{}, nil
}

type Plan struct {
	DeleteDeployments      []*v1.ObjectReference
	CreateDeployment       *appsv1.Deployment
	RegisterDefaultVersion string
}

func generatePlan(
	ref types.NamespacedName,
	observedState temporaliov1alpha1.WorkerDeploymentStatus,
	desiredState temporaliov1alpha1.WorkerDeploymentSpec,
) Plan {
	var plan Plan

	// Scale down any deployments that are not reachable by open workflows.
	// For deployments that _are_ reachable but currently scaled down, scale them back up.
	// TODO(jlegrone): should this behavior be configurable via options like MinExecutorReplicas and MinQueryReplicas?
	// ...

	// TODO(jlegrone): deal with the "rollback" case where an existing deployment may already exist and needs to be
	//                 promoted to the default version set.

	// TODO(jlegrone): generate warnings/events on the WorkerDeployment resource when version sets exist with no
	//                 corresponding Deployment.

	// Delete deployments with no reachability
	for _, versionSet := range observedState.DeprecatedVersionSets {
		if versionSet.ReachabilityStatus == "unreachable-todo" {
			plan.DeleteDeployments = append(plan.DeleteDeployments, versionSet.Deployment)
		}
	}

	desiredBuildID := computeBuildID(desiredState)

	if deployment := observedState.DefaultVersionSet.Deployment; deployment != nil {
		// Register new deployment as default major version set if it is healthy
		// if observedState.DefaultVersionSet.Deployment.IsHealthy() {
		//     plan.RegisterDefaultVersion = desiredBuildID
		// }
	} else {
		// Create new deployment from current pod template when it doesn't exist
		plan.CreateDeployment = &appsv1.Deployment{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				// TODO(jlegrone): Build ID in name might need to be shortened to fit k8s length constraints
				Name:                       fmt.Sprintf("%s-%s", ref.Name, desiredBuildID),
				Namespace:                  ref.Namespace,
				ResourceVersion:            "",
				DeletionGracePeriodSeconds: nil,
				Labels:                     nil,
				Annotations: map[string]string{
					buildIDAnnotation: desiredBuildID,
				},
				OwnerReferences: nil,
				Finalizers:      nil,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: desiredState.Replicas,
				Selector: nil,
				// TODO(jlegrone): Inject Temporal task queue, namespace, and build ID into pod template
				Template:        desiredState.Template,
				MinReadySeconds: desiredState.MinReadySeconds,
			},
		}
	}

	return plan
}

func (r *WorkerDeploymentReconciler) executePlan(ctx context.Context, req ctrl.Request, l logr.Logger, p Plan) error {
	// Check out API here:
	// https://github.com/temporalio/api/blob/cfa1a15b960920a47de8ec272873a4ee4db574c4/temporal/api/workflowservice/v1/request_response.proto#L1073-L1132

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *WorkerDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &appsv1.Deployment{}, deployOwnerKey, func(rawObj client.Object) []string {
		// grab the job object, extract the owner...
		deploy := rawObj.(*appsv1.Deployment)
		owner := metav1.GetControllerOf(deploy)
		if owner == nil {
			return nil
		}
		// ...make sure it's a WorkerDeployment...
		// TODO(jlegrone): double check apiGVStr has the correct value
		if owner.APIVersion != apiGVStr || owner.Kind != "WorkerDeployment" {
			return nil
		}

		// ...and if so, return it
		return []string{owner.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&temporaliov1alpha1.WorkerDeployment{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// TODO(jlegrone): consider alternatives, also don't hardcode main.
func computeBuildID(spec temporaliov1alpha1.WorkerDeploymentSpec) string {
	for _, container := range spec.Template.Spec.Containers {
		if container.Name == "main" {
			return container.Image
		}
	}
	return ""
}
