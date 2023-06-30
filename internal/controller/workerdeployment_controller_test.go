package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	temporaliov1alpha1 "github.com/temporalio/worker-controller/api/v1alpha1"
)

var (
	testPodTemplate = v1.PodTemplateSpec{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "main",
					Image: "foo/bar@sha256:deadbeef",
				},
			},
		},
	}
)

func newTestWorkerSpec(replicas int32) temporaliov1alpha1.WorkerDeploymentSpec {
	return temporaliov1alpha1.WorkerDeploymentSpec{
		Replicas: &replicas,
		Template: testPodTemplate,
		WorkerOptions: temporaliov1alpha1.WorkerOptions{
			TemporalNamespace: "baz",
			TaskQueue:         "qux",
		},
	}
}

func newTestDeployment(podSpec v1.PodTemplateSpec, desiredReplicas int32) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "foo",
			Name:      "bar-575c658769",
			Annotations: map[string]string{
				"temporal.io/build-id": "575c658769",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desiredReplicas,
			Template: podSpec,
		},
	}
}

func newTestVersionset(reachabilityStatus string, deploymentName string) temporaliov1alpha1.CompatibleVersionSet {
	result := temporaliov1alpha1.CompatibleVersionSet{
		ReachabilityStatus: reachabilityStatus,
		InactiveBuildIDs:   nil,
		DefaultBuildID:     "test-id",
		DeployedBuildID:    "test-id",
	}

	if deploymentName != "" {
		result.Deployment = &v1.ObjectReference{
			Namespace: "foo",
			Name:      deploymentName,
		}
	}

	return result
}

func TestGeneratePlan(t *testing.T) {
	type testCase struct {
		observedState temporaliov1alpha1.WorkerDeploymentStatus
		desiredState  temporaliov1alpha1.WorkerDeploymentSpec
		expectedPlan  Plan
	}

	testNamespacedName := types.NamespacedName{Namespace: "foo", Name: "bar"}

	testCases := map[string]testCase{
		"no action needed": {
			observedState: temporaliov1alpha1.WorkerDeploymentStatus{
				DefaultVersionSet: newTestVersionset("reachable", "foo-a"),
			},
			desiredState: newTestWorkerSpec(3),
			expectedPlan: Plan{
				DeleteDeployments:      nil,
				CreateDeployment:       nil,
				RegisterDefaultVersion: "",
			},
		},
		"create deployment": {
			observedState: temporaliov1alpha1.WorkerDeploymentStatus{
				DefaultVersionSet: temporaliov1alpha1.CompatibleVersionSet{
					ReachabilityStatus: "reachable",
					InactiveBuildIDs:   nil,
					DefaultBuildID:     "a",
				},
				DeprecatedVersionSets: nil,
			},
			desiredState: newTestWorkerSpec(3),
			expectedPlan: Plan{
				DeleteDeployments:      nil,
				CreateDeployment:       newTestDeployment(testPodTemplate, 3),
				RegisterDefaultVersion: "",
			},
		},
		"delete unreachable deployments": {
			observedState: temporaliov1alpha1.WorkerDeploymentStatus{
				DefaultVersionSet: newTestVersionset("reachable", "foo-a"),
				DeprecatedVersionSets: []temporaliov1alpha1.CompatibleVersionSet{
					newTestVersionset("unreachable-todo", "foo-b"),
					newTestVersionset("reachable", "foo-c"),
					newTestVersionset("unreachable-todo", "foo-d"),
				},
			},
			desiredState: newTestWorkerSpec(3),
			expectedPlan: Plan{
				DeleteDeployments: []*v1.ObjectReference{
					{
						Namespace: "foo",
						Name:      "foo-b",
					},
					{
						Namespace: "foo",
						Name:      "foo-d",
					},
				},
				CreateDeployment:       nil,
				RegisterDefaultVersion: "",
			},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			actualPlan := generatePlan(testNamespacedName, tc.observedState, tc.desiredState)
			assert.Equal(t, tc.expectedPlan, actualPlan)
		})
	}
}
