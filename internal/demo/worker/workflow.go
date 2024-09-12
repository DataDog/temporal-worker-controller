package main

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/workflow"
)

func HelloWorld(ctx workflow.Context) (string, error) {
	workflow.GetLogger(ctx).Info("HelloWorld workflow started")
	ctx = setActivityTimeout(ctx, time.Minute)

	// Compute a subject
	var subject string
	if err := workflow.ExecuteActivity(ctx, GetSubject).Get(ctx, &subject); err != nil {
		return "", err
	}

	// Sleep for a while
	if err := workflow.ExecuteActivity(ctx, Sleep, 30).Get(ctx, nil); err != nil {
		return "", err
	}

	// Return the greeting
	return fmt.Sprintf("Hello %s!", subject), nil
}

func GetSubject(ctx context.Context) (string, error) {
	return "Replay", nil
}

func Sleep(ctx context.Context, seconds uint) error {
	time.Sleep(time.Duration(seconds) * time.Second)
	return nil
}

// Deployment diff: https://ddstaging.datadoghq.com/apm/services/hello-world-worker/operations/temporal.RunWorkflow/resources?autoPilotParams=v1%7Cdeployment%7Chello-world-worker-67d75b6c5f&dependencyMap=qson%3A%28data%3A%28telemetrySelection%3Aall_sources%29%2Cversion%3A%210%29&deployments=qson%3A%28data%3A%28hits%3A%28selected%3Aversion_count%29%2Cerrors%3A%28selected%3Aversion_count%29%2Clatency%3A%2195%2CtopN%3A%215%29%2Cversion%3A%210%29&env=staging&errors=qson%3A%28data%3A%28issueSort%3AFIRST_SEEN%29%2Cversion%3A%210%29&fromUser=false&groupMapByOperation=null&infrastructure=qson%3A%28data%3A%28viewType%3Apods%29%2Cversion%3A%210%29&logs=qson%3A%28data%3A%28indexes%3A%5B%5D%29%2Cversion%3A%210%29&panels=qson%3A%28data%3A%28%29%2Cversion%3A%210%29&resources=qson%3A%28data%3A%28visible%3A%21t%2Chits%3A%28selected%3Atotal%29%2Cerrors%3A%28selected%3Atotal%29%2Clatency%3A%28selected%3Ap95%29%2CtopN%3A%215%29%2Cversion%3A%211%29&summary=qson%3A%28data%3A%28visible%3A%21t%2Cerrors%3A%28selected%3Acount%29%2Chits%3A%28selected%3Acount%29%2Clatency%3A%28selected%3Alatency%2Cslot%3A%28agg%3A95%29%2Cdistribution%3A%28isLogScale%3A%21f%29%2CshowTraceOutliers%3A%21t%29%2Csublayer%3A%28slot%3A%28layers%3Aservice%29%2Cselected%3Apercentage%29%2ClagMetrics%3A%28selectedMetric%3A%21s%2CselectedGroupBy%3A%21s%29%29%2Cversion%3A%211%29&traces=qson%3A%28data%3A%28%29%2Cversion%3A%210%29&start=1726090325565&end=1726093925565&paused=false#recentChanges

//http://localhost:8233/namespaces/default/workflows?query=BuildIds+%3D+%22versioned%3A6689d9b994%22

//temporal workflow reset \
//  --type FirstWorkflowTask \
//  --reason "bad version: workflow panic" \
//  --query 'BuildIds = "versioned:6689d9b994"'

// if v := workflow.GetVersion(ctx, "sleep-without-activity", workflow.DefaultVersion, 1); v == 1 {
//		if err := workflow.Sleep(ctx, 10*time.Second); err != nil {
//			return "", err
//		}
//	} else {
//		if err := workflow.ExecuteActivity(
//			workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
//				ScheduleToCloseTimeout: time.Minute,
//			}),
//			Sleep, 30,
//		).Get(ctx, nil); err != nil {
//			return "", err
//		}
//	}

// Create diff files like
//  git diff > v1_enable_versioning.patch
//
// Run through each step of the demo with a script like
//  git apply v1_enable_versioning.patch
//  git add . && git commit -m "enable_versioning" && git push
//  skaffold run --profile demo

// TODO:
//  - Move dashboard to demo org
//  - Commit dashboard json to repo
//  - Update demo readme
