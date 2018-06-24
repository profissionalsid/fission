package canaryconfigmgr

import (
	"sync"

	"github.com/fission/fission/pkg/apis/fission.io/v1"
)

type(
	RequestTracker struct {
		mutex *sync.Mutex
		Counter map[v1.TriggerReference]*RequestCounter
	}

	RequestCounter struct {
		TotalRequests int
		FailedRequests int
	}
)

func makeRequestTracker() *RequestTracker {
	return &RequestTracker{
		mutex : &sync.Mutex{},
		Counter: make(map[v1.TriggerReference]*RequestCounter, 0),
	}
}

// cant do better than to use mutex here. we need it because we are reading the value and modifying it in memory and
// there can be concurrent go routines calling this method.
func (reqTracker *RequestTracker) set(triggerRef *v1.TriggerReference, failedReq bool) {
	var value *RequestCounter

	reqTracker.mutex.Lock()
	defer reqTracker.mutex.Unlock()
	value, ok := reqTracker.Counter[*triggerRef]

	if !ok {
		value = &RequestCounter{}
	}
	if failedReq {
		value.FailedRequests += 1
	}
	value.TotalRequests += 1
}

func (reqTracker *RequestTracker) get(triggerRef *v1.TriggerReference) *RequestCounter {
	reqTracker.mutex.Lock()
	defer reqTracker.mutex.Unlock()

	return reqTracker.Counter[*triggerRef]
}
