package telefonist

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type CronManager struct {
	mu        sync.Mutex
	c         *cron.Cron
	hub       *WsHub
	testStore *TestStore
	entryIDs  map[int]cron.EntryID
	jobQueue  chan CronJobRequest
	ctx       context.Context
	cancel    context.CancelFunc
}

type CronJobRequest struct {
	Project  string
	Testfile string
}

func NewCronManager(hub *WsHub, store *TestStore) *CronManager {
	ctx, cancel := context.WithCancel(context.Background())
	// Use standard cron format (minute, hour, dom, month, dow)
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

	cm := &CronManager{
		c:         cron.New(cron.WithParser(parser)),
		hub:       hub,
		testStore: store,
		entryIDs:  make(map[int]cron.EntryID),
		jobQueue:  make(chan CronJobRequest, 100),
		ctx:       ctx,
		cancel:    cancel,
	}

	go cm.workerLoop()
	return cm
}

// Start begins the cron scheduler and loads active jobs from DB.
func (cm *CronManager) Start() {
	cm.c.Start()
	cm.ReloadAll()
}

// Stop halts the scheduler and workers.
func (cm *CronManager) Stop() {
	cm.cancel()
	_ = cm.c.Stop()
}

// ReloadAll clears existing memory schedule and repopulates from DB.
func (cm *CronManager) ReloadAll() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	for _, entryID := range cm.entryIDs {
		cm.c.Remove(entryID)
	}
	cm.entryIDs = make(map[int]cron.EntryID)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	jobs, err := cm.testStore.ListCronJobs(ctx)
	if err != nil {
		log.Printf("cron: failed to list jobs from db: %v", err)
		return
	}

	for _, j := range jobs {
		if !j.Active {
			continue
		}
		
		project := j.Project
		testfile := j.Testfile
		
		id, err := cm.c.AddFunc(j.CronExpr, func() {
			select {
			case cm.jobQueue <- CronJobRequest{Project: project, Testfile: testfile}:
				log.Printf("cron: queued job for Project: %q, Testfile: %q", project, testfile)
			default:
				log.Printf("cron: queue full, skipping active trigger for Project: %q, Testfile: %q", project, testfile)
			}
		})

		if err != nil {
			log.Printf("cron: failed to schedule job ID %d (%s): %v", j.ID, j.CronExpr, err)
			continue
		}
		cm.entryIDs[j.ID] = id
	}
}

// workerLoop executes queued runs synchronously, respecting the global inlineRunActive lock.
func (cm *CronManager) workerLoop() {
	for {
		select {
		case <-cm.ctx.Done():
			return
		case req := <-cm.jobQueue:
			cm.executeJob(req)
		}
	}
}

func (cm *CronManager) executeJob(req CronJobRequest) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	// Block until the active test file run lock is released, respecting shutdown context
	for cm.hub.inlineRunActive.Load() {
		select {
		case <-cm.ctx.Done():
			return
		case <-ticker.C:
			// check again
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var batch []TestfileData
	if req.Testfile == "" {
		all, err := cm.testStore.List(ctx, true)
		if err != nil {
			log.Printf("cron: failed to list testfiles for project %s: %v", req.Project, err)
			return
		}
		for _, tf := range all {
			if tf.ProjectName == req.Project {
				batch = append(batch, TestfileData{Name: tf.Name, ProjectName: tf.ProjectName, Content: tf.Content})
			}
		}
	} else {
		tf, err := cm.testStore.Load(ctx, req.Testfile, req.Project)
		if err != nil {
			log.Printf("cron: failed to load testfile %s for project %s: %v", req.Testfile, req.Project, err)
			return
		}
		batch = append(batch, TestfileData{Name: tf.Name, ProjectName: tf.ProjectName, Content: tf.Content})
	}

	if len(batch) == 0 {
		log.Printf("cron: no testfiles found for Project: %q, Testfile: %q", req.Project, req.Testfile)
		return
	}

	// Try triggering the run until we successfully grab the atomic lock (in case of a race)
	retryTicker := time.NewTicker(2 * time.Second)
	defer retryTicker.Stop()

	for {
		if runTestfilesBatch(cm.hub, batch) {
			log.Printf("cron: started run for Project: %q, Testfile: %q", req.Project, req.Testfile)
			
			// Wait for the run to genuinely start so we don't accidentally pull the next queue item
			select {
			case <-time.After(1 * time.Second):
			case <-cm.ctx.Done():
				return
			}
			
			// Now we block the cron worker until this test actively finishes 
			// so we don't spam the queue
			for cm.hub.inlineRunActive.Load() {
				select {
				case <-cm.ctx.Done():
					return
				case <-ticker.C:
				}
			}
			break
		}
		
		select {
		case <-cm.ctx.Done():
			return
		case <-retryTicker.C:
			// wait for UI active run to finish and try again
		}
	}
}
