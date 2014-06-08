package testutils

import (
	"errors"
	"sync"
	"time"

	"github.com/flynn/flynn-host/types"
	"github.com/flynn/go-flynn/cluster"
)

func NewFakeCluster() *FakeCluster {
	return &FakeCluster{hostClients: make(map[string]*FakeHostClient)}
}

type FakeCluster struct {
	hosts       map[string]host.Host
	hostClients map[string]*FakeHostClient
	mtx         sync.RWMutex
}

func (c *FakeCluster) ListHosts() (map[string]host.Host, error) {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	hosts := make(map[string]host.Host, len(c.hosts))
	for id, _ := range c.hosts {
		hosts[id] = c.GetHost(id)
	}
	return hosts, nil
}

func (c *FakeCluster) GetHost(id string) host.Host {
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	h := c.hosts[id]

	// copy the jobs to avoid races
	jobs := make([]*host.Job, len(h.Jobs))
	copy(jobs, h.Jobs)

	return host.Host{ID: h.ID, Jobs: jobs, Attributes: h.Attributes}
}

func (c *FakeCluster) DialHost(id string) (cluster.Host, error) {
	client, ok := c.hostClients[id]
	if !ok {
		return nil, errors.New("FakeCluster: unknown host")
	}
	return client, nil
}

func (c *FakeCluster) AddJobs(req *host.AddJobsReq) (*host.AddJobsRes, error) {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for hostID, jobs := range req.HostJobs {
		host, ok := c.hosts[hostID]
		if !ok {
			return nil, errors.New("FakeCluster: unknown host")
		}
		if client, ok := c.hostClients[hostID]; ok {
			for _, job := range jobs {
				// Send the start event asynchronously after a short period of time
				// to give the caller chance to finish processing previous events
				// before it receives the start event (e.g. when restarting jobs,
				// the scheduler needs to track the new hostID and jobID before it
				// can deal with a start event for that job).
				go func() {
					time.Sleep(100 * time.Millisecond)
					client.SendEvent("start", job.ID)
				}()
			}
		}
		host.Jobs = append(host.Jobs, jobs...)
		c.hosts[hostID] = host
	}
	return &host.AddJobsRes{State: c.hosts}, nil
}

func (c *FakeCluster) RemoveJob(hostID, jobID string, errored bool) error {
	c.mtx.Lock()
	h, ok := c.hosts[hostID]
	if !ok {
		return errors.New("FakeCluster: unknown host")
	}
	jobs := make([]*host.Job, 0, len(h.Jobs))
	for _, job := range h.Jobs {
		if job.ID != jobID {
			jobs = append(jobs, job)
		}
	}
	h.Jobs = jobs
	c.hosts[hostID] = h
	c.mtx.Unlock()

	if client, ok := c.hostClients[hostID]; ok {
		if errored {
			client.SendEvent("error", jobID)
		} else {
			client.SendEvent("stop", jobID)
		}
	}
	return nil
}

func (c *FakeCluster) SetHosts(h map[string]host.Host) {
	c.hosts = h
}

func (c *FakeCluster) SetHostClient(id string, h *FakeHostClient) {
	h.cluster = c
	c.hostClients[id] = h
}
