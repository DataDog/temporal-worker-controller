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

// WorkerDeploymentSpec defines the desired state of WorkerDeployment
type WorkerDeploymentSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// Number of desired pods. This is a pointer to distinguish between explicit
	// zero and not specified. Defaults to 1.
	// +optional
	Replicas *int32 `json:"replicas,omitempty" protobuf:"varint,1,opt,name=replicas"`

	// Template describes the pods that will be created.
	// The only allowed template.spec.restartPolicy value is "Always".
	Template v1.PodTemplateSpec `json:"template"`

	// Minimum number of seconds for which a newly created pod should be ready
	// without any of its container crashing, for it to be considered available.
	// Defaults to 0 (pod will be considered available as soon as it is ready)
	// +optional
	MinReadySeconds int32 `json:"minReadySeconds,omitempty"`

	// TODO(jlegrone): add godoc
	WorkerOptions WorkerOptions `json:"workerOptions"`
}

type CompatibleVersionSet struct {
	// ReachabilityStatus indicates whether workers in this version set may
	// be eligible to receive tasks from the Temporal server.
	// TODO(jlegrone): Make this an enum, and consider whether multiple values are needed.
	ReachabilityStatus string `json:"reachabilityStatus"`
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

// WorkerDeploymentStatus defines the observed state of WorkerDeployment
type WorkerDeploymentStatus struct {
	// 	Remember, status should be able to be reconstituted from the state of the world, so it’s generally not a good
	//	idea to read from the status of the root object. Instead, you should reconstruct it every run.

	DefaultVersionSet     CompatibleVersionSet   `json:"defaultVersionSet"`
	DeprecatedVersionSets []CompatibleVersionSet `json:"deprecatedVersionSets"`
	// include pointers to managed deployments associated with each version set?
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// WorkerDeployment is the Schema for the workerdeployments API
//
// TODO(jlegrone): Implement default/validate interface https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html
type WorkerDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkerDeploymentSpec   `json:"spec,omitempty"`
	Status WorkerDeploymentStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// WorkerDeploymentList contains a list of WorkerDeployment
type WorkerDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkerDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkerDeployment{}, &WorkerDeploymentList{})
}
