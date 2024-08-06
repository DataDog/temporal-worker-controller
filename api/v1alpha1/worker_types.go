// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type WorkerOptions struct {
	TemporalNamespace string `json:"temporalNamespace"`
	TaskQueue         string `json:"taskQueue"`
}

// TemporalWorkerSpec defines the desired state of TemporalWorker
type TemporalWorkerSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Number of desired pods. This is a pointer to distinguish between explicit
	// zero and not specified. Defaults to 1.
	// +optional
	Replicas *int32 `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`

	// Label selector for pods. Existing ReplicaSets whose pods are
	// selected by this will be the ones affected by this deployment.
	// It must match the pod template's labels.
	Selector *metav1.LabelSelector `json:"selector" protobuf:"bytes,2,opt,name=selector"`

	// Template describes the pods that will be created.
	// The only allowed template.spec.restartPolicy value is "Always".
	Template v1.PodTemplateSpec `json:"template"`

	// Minimum number of seconds for which a newly created pod should be ready
	// without any of its container crashing, for it to be considered available.
	// Defaults to 0 (pod will be considered available as soon as it is ready)
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty"`

	// The maximum time in seconds for a deployment to make progress before it
	// is considered to be failed. The deployment controller will continue to
	// process failed deployments and a condition with a ProgressDeadlineExceeded
	// reason will be surfaced in the deployment status. Note that progress will
	// not be estimated during the time a deployment is paused. Defaults to 600s.
	ProgressDeadlineSeconds *int32 `json:"progressDeadlineSeconds,omitempty" protobuf:"varint,9,opt,name=progressDeadlineSeconds"`

	// TODO(jlegrone): add godoc
	WorkerOptions WorkerOptions `json:"workerOptions"`
}

// ReachabilityStatus indicates whether the version set is processing tasks.
// +enum
type ReachabilityStatus string

const (
	// ReachabilityStatusReachable indicates that the build ID may be used by
	// new workflows or activities (base on versioning rules), or there MAY
	// be open workflows or backlogged activities assigned to it.
	ReachabilityStatusReachable ReachabilityStatus = "Reachable"
	// ReachabilityStatusClosedOnly indicates that the build ID does not have
	// open workflows and is not reachable by new workflows, but MAY have
	// closed workflows within the namespace retention period.
	ReachabilityStatusClosedOnly ReachabilityStatus = "ClosedWorkflows"
	// ReachabilityStatusUnreachable indicates that the build ID is not used
	// for new executions, nor it has been used by any existing execution
	// within the retention period.
	ReachabilityStatusUnreachable ReachabilityStatus = "Unreachable"
	// ReachabilityStatusNotRegistered indicates that the build ID is not registered
	// with Temporal for the given task queue.
	ReachabilityStatusNotRegistered ReachabilityStatus = "NotRegistered"
)

type CompatibleVersionSet struct {
	// ReachabilityStatus indicates whether workers in this version set may
	// be eligible to receive tasks from the Temporal server.
	// TODO(jlegrone): Make this an enum, and consider whether multiple values are needed.
	ReachabilityStatus ReachabilityStatus `json:"reachabilityStatus"`
	// Build IDs that were previously registered as the default.
	InactiveBuildIDs []string `json:"inactiveBuildIDs,omitempty"`
	// The default build ID currently registered with Temporal.
	DefaultBuildID string `json:"defaultBuildID"`
	// The build ID associated with the version set's current deployment.
	//
	// This should usually align with the DefaultBuildID, but may be different
	// in cases where the version set has been recently updated or frozen.
	DeployedBuildID string `json:"deployedBuildID"`
	// A pointer to the version set's managed deployment.
	Deployment *v1.ObjectReference `json:"active,omitempty"`
}

// TemporalWorkerStatus defines the observed state of TemporalWorker
type TemporalWorkerStatus struct {
	// 	Remember, status should be able to be reconstituted from the state of the world, so it’s generally not a good
	//	idea to read from the status of the root object. Instead, you should reconstruct it every run.

	// NextVersionSet is the desired next version. If the deployment is nil,
	// then the controller should create it. If not nil, the controller should
	// wait for it to become healthy and then move it to the DefaultVersionSet.
	NextVersionSet *NextVersionSet `json:"nextVersionSet,omitempty"`
	// DefaultVersionSet is the version set that is currently registered with
	// Temporal as the default. Deployment must not be nil in this version set.
	DefaultVersionSet *CompatibleVersionSet `json:"defaultVersionSet,omitempty"`
	// DeprecatedVersionSets are version sets that are no longer the default. Any
	// deployments that are unreachable should be deleted by the controller.
	DeprecatedVersionSets []*CompatibleVersionSet `json:"deprecatedVersionSets,omitempty"`
}

type NextVersionSet struct {
	VersionSet *CompatibleVersionSet `json:"versionSet"`
	// DeploymentHealthy indicates whether the deployment is healthy.
	DeploymentHealthy bool `json:"deploymentHealthy"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TemporalWorker is the Schema for the temporalworkers API
//
// TODO(jlegrone): Implement default/validate interface https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html
type TemporalWorker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TemporalWorkerSpec   `json:"spec,omitempty"`
	Status TemporalWorkerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TemporalWorkerList contains a list of TemporalWorker
type TemporalWorkerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TemporalWorker `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemporalWorker{}, &TemporalWorkerList{})
}
