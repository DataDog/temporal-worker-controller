
 ## Demo
 
This demo assumes you have [skaffold](https://skaffold.dev/docs/install/) and [minikube](https://minikube.sigs.k8s.io/docs/) and installed, but any
Kubernetes cluster should work if you're able to build and pull the demo worker image.

1. Start local Temporal server:
    ```bash
    make start-temporal-server
    ```
1. Open Temporal UI: http://localhost:8233/namespaces/default/workflows
1. Start local k8s:
    ```bash
    minikube start
    ```
1. Start the controller:
    ```bash
   skaffold dev --profile manager
    ```
1. Start applying load:
    ```bash
   make apply-load-sample-workflow
    ```
1. Inspect versioning rules: http://localhost:8233/namespaces/default/task-queues/hello_world
1. Deploy worker v1, observe worker versions in terminal:
    ```bash
   skaffold run --profile demo
    ```
1. Upgrade to v2 by editing args in [internal/demo/temporal_worker.yaml](temporal_worker.yaml), and
redeploy:
    ```bash
   skaffold run --profile demo
    ```
   1. Observe multiple worker versions making progress in parallel
   1. Show that v1 eventually scales down
   1. Note that it's not fully deleted yet in order to support rolling back the default version externally, eg. with the `temporal` CLI
1. Roll out v3
1. Roll out v4, observe that worker fails to enter running state, but new workflows continue executing
1. Roll back to v2, and then restart Temporal server
    1. Note that now all worker deployments other than v2 are deleted (simulates workflow expiration)
