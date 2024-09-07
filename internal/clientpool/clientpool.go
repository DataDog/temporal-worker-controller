package clientpool

import (
	"fmt"
	"sync"

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

func (cp *ClientPool) GetClient(k8sName, k8sNamespace, temporalNamespace string) client.Client {
	key := newClientKey(k8sName, k8sNamespace, temporalNamespace)

	cp.mux.RLock()
	defer cp.mux.RUnlock()

	c, ok := cp.clients[key]
	if ok {
		return c
	}
	return nil
}

func (cp *ClientPool) UpsertClient(k8sName, k8sNamespace, temporalNamespace string, c client.Client) {
	key := newClientKey(k8sName, k8sNamespace, temporalNamespace)

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

func newClientKey(k8sName, k8sNamespace, temporalNamespace string) string {
	return fmt.Sprintf("%s/%s/%s", k8sName, k8sNamespace, temporalNamespace)
}
