package clientpool

import (
	"fmt"
	"sync"

	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

type ClientPool struct {
	mux     sync.RWMutex
	clients map[string]client.Client
}

func New() *ClientPool {
	return &ClientPool{
		clients: make(map[string]client.Client),
	}
}

func (cp *ClientPool) GetWorkflowServiceClient(k8sName, k8sNamespace string) (workflowservice.WorkflowServiceClient, bool) {
	key := newClientKey(k8sName, k8sNamespace)

	cp.mux.RLock()
	defer cp.mux.RUnlock()

	c, ok := cp.clients[key]
	if ok {
		return c.WorkflowService(), true
	}
	return nil, false
}

func (cp *ClientPool) UpsertClient(k8sName, k8sNamespace string, c client.Client) {
	key := newClientKey(k8sName, k8sNamespace)

	cp.mux.Lock()
	defer cp.mux.Unlock()

	cp.clients[key] = c
}

func (cp *ClientPool) Close() {
	cp.mux.Lock()
	defer cp.mux.Unlock()

	for _, c := range cp.clients {
		c.Close()
	}

	cp.clients = make(map[string]client.Client)
}

func newClientKey(k8sName, k8sNamespace string) string {
	return fmt.Sprintf("%s/%s", k8sName, k8sNamespace)
}
