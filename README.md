# Temporal Worker Controller

> ⚠️ This project is 100% experimental. Please do not attempt to install the controller in any production and/or shared environment.

The goal of the Temporal Worker Controller is to make it easy to run workers on Kubernetes while leveraging task queue
version partitions.

## Why

Temporal's [deterministic constraints](https://docs.temporal.io/workflows#deterministic-constraints) can cause headaches
when rolling out or rolling back workflow code changes.

The traditional approach to workflow determinism is to gate new behavior behind
[versioning checks](https://docs.temporal.io/workflows#workflow-versioning). Over time these checks can become a
source of technical debt, as safely removing them from a codebase is a careful process that often involves querying all
running workflows.

[Worker Versioning](https://docs.temporal.io/workers#worker-versioning) introduces an alternative approach which enables
workflow executions to be sticky to workers running a specific code revision. This allows a workflow author
to omit version checks in code and instead run multiple versions of their worker in parallel, relying on Temporal to
keep workflow executions pinned to workers running compatible code.

This project aims to provide automation which simplifies the bookkeeping around tracking which worker versions still
have active workflows, managing the lifecycle of versioned worker deployments, and calling Temporal APIs to update task
queue partitions when new versions are rolled out.

## Features

- [ ] Registration of new task queue version sets
- [ ] Creation of versioned worker deployment resources
- [ ] Deletion of unreachable versioned worker deployments
- [ ] Autoscaling of versioned worker deployments
- [ ] Automated merging of compatible version sets
- [ ] Optional cancellation after timeout for workflows on old version sets
- [ ] Passing `ContinueAsNew` signal to workflows on old version sets

## Usage

In order to be compatible with this controller, workers need to be configured using these standard environment
variables:

- `WORKER_BINARY_CHECKSUM`
- `TEMPORAL_TASK_QUEUE`
- `TEMPORAL_NAMESPACE`

Each of these will be automatically set in the pod template's env, and do not need to be manually specified outside the
`WorkerDeployment` spec.

## How It Works

Every `WorkerDeployment` resource manages one or more standard `Deployment` resources. Each deployment manages pods
which in turn poll Temporal for tasks in their respective task queue version partitions.

```mermaid
flowchart TD
    wd[WorkerDeployment]

    subgraph "Latest/default worker version"
        d5["Deployment v5"]
        rs5["ReplicaSet v5"]
        p5a["Pod v5-a"]
        p5b["Pod v5-b"]
        p5c["Pod v5-c"]
        d5 --> rs5
        rs5 --> p5a
        rs5 --> p5b
        rs5 --> p5c
    end

    subgraph "Deprecated worker versions"
        d1["Deployment v1"]
        rs1["ReplicaSet v1"]
        p1a["Pod v1-a"]
        p1b["Pod v1-b"]
        d1 --> rs1
        rs1 --> p1a
        rs1 --> p1b

        dN["Deployment ..."]
    end

    wd --> d1
    wd --> dN
    wd --> d5

    p1a -. "poll partition v1" .-> server
    p1b -. "poll partition v1" .-> server

    p5a -. "poll partition v5" .-> server
    p5b -. "poll partition v5" .-> server
    p5c -. "poll partition v5" .-> server

    server["Temporal Server"]
```

### Worker Lifecycle

When a new worker version is deployed, the worker controller automates the registration of a new default task queue
partition in Temporal.

As older workflows finish executing and deprecated worker versions are no longer needed, the worker controller also
frees up resources by deleting old deployments.

```mermaid
sequenceDiagram
    autonumber
    participant Dev as Developer
    participant K8s as Kubernetes
    participant Ctl as WorkerController
    participant T as Temporal

    Dev->>K8s: Create WorkerDeployment "foo" (v1)
    K8s-->>Ctl: Notify WorkerDeployment "foo" created
    Ctl->>K8s: Create Deployment "foo-v1"
    Ctl->>T: Register v1 as new default set
    Dev->>K8s: Update WorkerDeployment "foo" (v2)
    K8s-->>Ctl: Notify WorkerDeployment "foo" updated
    Ctl->>K8s: Create Deployment "foo-v2"
    Ctl->>T: Register v2 as new default set
    
    Ctl->>Ctl: Run breaking change detection between v1 and v2
    Ctl->>T: If versions compatible, merge v1 and v2 version sets.
    
    loop Poll Temporal API
        Ctl-->>T: Wait for v1 workflow executions to close
    end
    
    Ctl->>K8s: Delete Deployment "foo-v1"
```

## Contributing

This project is in very early stages and is not yet soliciting external contributions.

Please reach out to `@jlegrone` on the [Temporal Slack](https://t.mp/slack) if you have questions, suggestions, or are
interested in contributing.
