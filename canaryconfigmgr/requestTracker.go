package canaryconfigmgr

import (
	"sync"

	"github.com/fission/fission/pkg/apis/fission.io/v1"
)

// TODO : Replace with APIs to prometheus to getMetrics
// things to keep in mind :
// 1. clock sync between prom server and router instances
// 2. also fault tolerance when - 1. prom server is restarting/dead  2. somehow values are missing for that instant

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

func (reqTracker *RequestTracker) reset(triggerRef *v1.TriggerReference) {
	reqTracker.mutex.Lock()
	defer reqTracker.mutex.Unlock()

	reqCounter := &RequestCounter{}
	reqTracker.Counter[*triggerRef] = reqCounter
}


func calculatePercentageFailure(reqCounter *RequestCounter) int {
	if reqCounter.TotalRequests != 0 {
		return int(reqCounter.FailedRequests / reqCounter.TotalRequests * 100)
	}

	return 0
}
