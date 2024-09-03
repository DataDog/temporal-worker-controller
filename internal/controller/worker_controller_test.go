// Unless explicitly stated otherwise all files in this repository are licensed under the MIT License.
//
// This product includes software developed at Datadog (https://www.datadoghq.com/). Copyright 2024 Datadog, Inc.

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporaliov1alpha1 "github.com/DataDog/temporal-worker-controller/api/v1alpha1"
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

func newTestWorkerSpec(replicas int32) *temporaliov1alpha1.TemporalWorkerSpec {
	return &temporaliov1alpha1.TemporalWorkerSpec{
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
			Name:      "bar-7476c6b88c",
			Annotations: map[string]string{
				"temporal.io/build-id": "7476c6b88c",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &desiredReplicas,
			Template: podSpec,
		},
	}
}

func newTestVersionedDeployment(reachabilityStatus temporaliov1alpha1.ReachabilityStatus, deploymentName string) *temporaliov1alpha1.VersionedDeployment {
	result := temporaliov1alpha1.VersionedDeployment{
		Healthy:            false,
		BuildID:            "test-id",
		CompatibleBuildIDs: nil,
		Reachability:       reachabilityStatus,
		RampPercentage:     nil,
		Statistics:         nil,
		Deployment:         nil,
	}

	if deploymentName != "" {
		result.Deployment = &v1.ObjectReference{
			Namespace: "foo",
			Name:      deploymentName,
		}
	} else {
		panic("deploymentName required")
	}

	return &result
}

func TestGeneratePlan(t *testing.T) {
	type testCase struct {
		observedState *temporaliov1alpha1.TemporalWorkerStatus
		desiredState  *temporaliov1alpha1.TemporalWorkerSpec
		expectedPlan  plan
	}

	testCases := map[string]testCase{
		"no action needed": {
			observedState: &temporaliov1alpha1.TemporalWorkerStatus{
				DefaultVersion: newTestVersionedDeployment(temporaliov1alpha1.ReachabilityStatusReachable, "foo-a"),
			},
			desiredState: newTestWorkerSpec(3),
			expectedPlan: plan{},
		},
		"create deployment": {
			observedState: &temporaliov1alpha1.TemporalWorkerStatus{
				DefaultVersion: &temporaliov1alpha1.VersionedDeployment{
					Reachability:       temporaliov1alpha1.ReachabilityStatusReachable,
					CompatibleBuildIDs: nil,
					BuildID:            "a",
				},
				DeprecatedVersions: nil,
			},
			desiredState: newTestWorkerSpec(3),
			expectedPlan: plan{
				DeleteDeployments:   nil,
				CreateDeployment:    newTestDeployment(testPodTemplate, 3),
				UpdateVersionConfig: nil,
			},
		},
		"delete unreachable deployments": {
			observedState: &temporaliov1alpha1.TemporalWorkerStatus{
				DefaultVersion: newTestVersionedDeployment(temporaliov1alpha1.ReachabilityStatusReachable, "foo-a"),
				DeprecatedVersions: []*temporaliov1alpha1.VersionedDeployment{
					newTestVersionedDeployment(temporaliov1alpha1.ReachabilityStatusUnreachable, "foo-b"),
					newTestVersionedDeployment(temporaliov1alpha1.ReachabilityStatusReachable, "foo-c"),
					newTestVersionedDeployment(temporaliov1alpha1.ReachabilityStatusUnreachable, "foo-d"),
				},
			},
			desiredState: newTestWorkerSpec(3),
			expectedPlan: plan{
				DeleteDeployments: []*appsv1.Deployment{
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo",
							Name:      "foo-b",
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: "foo",
							Name:      "foo-d",
						},
					},
				},
				CreateDeployment:    nil,
				UpdateVersionConfig: nil,
			},
		},
	}

	c, err := client.New(nil, client.Options{
		HTTPClient:     nil,
		Scheme:         nil,
		Mapper:         nil,
		Cache:          nil,
		WarningHandler: client.WarningHandlerOptions{},
		DryRun:         nil,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := &TemporalWorkerReconciler{
		Client: c,
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			actualPlan, err := r.generatePlan(context.Background(), nil, nil, tc.desiredState, tc.observedState)
			assert.NoError(t, err)
			assert.Equal(t, &tc.expectedPlan, actualPlan)
		})
	}
}
