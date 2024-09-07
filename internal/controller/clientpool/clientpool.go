package clientpool

import (
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

func (cp *ClientPool) GetWorkflowServiceClient(hostPort string) (workflowservice.WorkflowServiceClient, bool) {
	cp.mux.RLock()
	defer cp.mux.RUnlock()

	c, ok := cp.clients[hostPort]
	if ok {
		return c.WorkflowService(), true
	}
	return nil, false
}

func (cp *ClientPool) UpsertClient(hostPort string) (workflowservice.WorkflowServiceClient, error) {
	c, err := client.Dial(client.Options{
		HostPort: hostPort,
	})
	if err != nil {
		return nil, err
	}

	cp.mux.Lock()
	defer cp.mux.Unlock()

	cp.clients[hostPort] = c

	return c.WorkflowService(), nil
}

func (cp *ClientPool) Close() {
	cp.mux.Lock()
	defer cp.mux.Unlock()

	for _, c := range cp.clients {
		c.Close()
	}

	cp.clients = make(map[string]client.Client)
}
