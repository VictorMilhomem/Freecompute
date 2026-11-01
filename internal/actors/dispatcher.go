package actors

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/internal/protocol"
	"github.com/VictorMilhomem/freecompute/internal/sandbox"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

const (
	DefaultMaxTicks = 50
)

type WorkerPolicyConfig struct {
	AllowImages   []string
	AllowAnyImage bool
	AllowOwnJobs  bool
	MaxConcurrent int
	ImagePolicy   sandbox.ImagePolicy
}

type DispatcherConfig struct {
	NodeID     string
	PublicKey  string
	StateDir   string
	TickAfter  time.Duration
	MaxTicks   int
	Supervisor gen.Atom
	Bridge     gen.Atom
	Store      Store
	Runner     Runner
	RunCtx     context.Context
	Logger     *slog.Logger
}

type EnableWorkerReq struct {
	Policy WorkerPolicyConfig
	Reply  chan error
}

type SubmitJobReq struct {
	JobID    string
	Announce *vcv1.JobAnnounce
	OnEvent  func(JobEvent)
	OnDone   func(JobResult, error)
	Reply    chan error
}

type StopDispatcherMsg struct {
	Reply chan struct{}
}

type AcquireSlotReq struct {
	JobID string
	Task  gen.Atom
}

type SlotGrantedMsg struct {
	JobID string
}

type ReleaseSlotMsg struct {
	JobID string
}

type JobStateUpdate struct {
	State protocol.State
}

type tickCheckMsg struct {
	JobID string
}

type trackedJob struct {
	jobID           string
	state           protocol.State
	jobActorAtom    gen.Atom
	taskActorAtom   gen.Atom
	lastTickOrEntry time.Time
	ticksPosted     int
	tickTimerActive bool
}

type Dispatcher struct {
	act.Actor
	cfg           DispatcherConfig
	jobs          map[string]*trackedJob
	workerEnabled bool
	workerPolicy  WorkerPolicyConfig
	persistedJobs map[string]PersistedTaskState
	activeSlots   map[string]gen.Atom
	waitQueue     []AcquireSlotReq
	stopped       bool
}

func NewDispatcher(cfg DispatcherConfig) *Dispatcher {
	if cfg.TickAfter <= 0 {
		cfg.TickAfter = 10 * time.Second
	}
	if cfg.MaxTicks <= 0 {
		cfg.MaxTicks = DefaultMaxTicks
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Dispatcher{
		cfg:           cfg,
		jobs:          make(map[string]*trackedJob),
		persistedJobs: make(map[string]PersistedTaskState),
		activeSlots:   make(map[string]gen.Atom),
	}
}

func (d *Dispatcher) Init(args ...any) error {
	loaded, err := LoadAllTaskStates(d.cfg.StateDir)
	if err == nil {
		d.persistedJobs = loaded
	}
	return nil
}

func (d *Dispatcher) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case EnableWorkerReq:
		err := d.applyEnableWorker(msg.Policy)
		if msg.Reply != nil {
			msg.Reply <- err
		}

	case SubmitJobReq:
		err := d.applySubmitJob(msg)
		if msg.Reply != nil {
			msg.Reply <- err
		}

	case StopDispatcherMsg:
		d.stopped = true
		d.waitQueue = nil
		if msg.Reply != nil {
			close(msg.Reply)
		}

	case ChainUpdate:
		d.handleChainUpdate(msg)

	case AcquireSlotReq:
		d.handleAcquireSlot(msg)

	case ReleaseSlotMsg:
		d.releaseSlot(msg.JobID)

	case ChildStartedMsg:
		d.handleChildStarted(msg)

	case ChildTerminatedMsg:
		d.handleChildTerminated(msg)

	case tickCheckMsg:
		d.handleTickCheck(msg.JobID)
	}
	return nil
}

func (d *Dispatcher) applyEnableWorker(policy WorkerPolicyConfig) error {
	if d.stopped {
		return fmt.Errorf("dispatcher stopped")
	}
	if policy.MaxConcurrent <= 0 {
		policy.MaxConcurrent = 1
	}
	d.workerEnabled = true
	d.workerPolicy = policy
	if loaded, err := LoadAllTaskStates(d.cfg.StateDir); err == nil {
		d.persistedJobs = loaded
	}
	for _, job := range d.jobs {
		d.maybeSpawnTaskActor(job)
	}
	return nil
}

func (d *Dispatcher) applySubmitJob(req SubmitJobReq) error {
	if d.stopped {
		return fmt.Errorf("dispatcher stopped")
	}
	job := d.getOrCreateJob(req.JobID)
	jobAtom := gen.Atom("job_" + req.JobID)
	job.jobActorAtom = jobAtom

	jCfg := JobActorConfig{
		NodeID:    d.cfg.NodeID,
		PublicKey: d.cfg.PublicKey,
		JobID:     req.JobID,
		Announce:  req.Announce,
		Bridge:    d.cfg.Bridge,
		Store:     d.cfg.Store,
		Logger:    d.cfg.Logger,
		OnEvent:   req.OnEvent,
		OnDone:    req.OnDone,
	}
	spec := act.SupervisorChildSpec{
		Name: jobAtom,
		Factory: func() gen.ProcessBehavior {
			return NewJobActor(jCfg)
		},
		Restart: act.SupervisorChildRestart{
			Strategy: act.SupervisorStrategyTransient,
		},
	}
	respAny, err := d.Call(d.cfg.Supervisor, SpawnChildReq{Spec: spec})
	if err != nil {
		return err
	}
	if resp, ok := respAny.(SpawnChildResp); ok && resp.Err != nil {
		return resp.Err
	}
	d.scheduleTickCheck(job)
	return nil
}

func (d *Dispatcher) handleChainUpdate(update ChainUpdate) {
	if update.Reset {
		for _, job := range d.jobs {
			job.state = protocol.State{
				Phase:     protocol.Committing,
				LastIndex: -1,
			}
		}
	}

	touched := make(map[string]*trackedJob)
	for _, e := range update.Entries {
		jobID := EntryJobID(e)
		if jobID == "" {
			continue
		}
		job := d.getOrCreateJob(jobID)
		if e.Index <= job.state.LastIndex {
			continue
		}
		job.state = protocol.Apply(job.state, e)
		job.lastTickOrEntry = time.Now()
		touched[jobID] = job
	}

	for _, job := range touched {
		if job.state.Announce == nil {
			continue
		}
		d.maybeSpawnTaskActor(job)
		d.scheduleTickCheck(job)
		d.notifyJobActors(job)
		if job.state.Finalize != nil || job.state.Phase == protocol.Expired {
			d.releaseSlot(job.jobID)
		}
	}
}

func (d *Dispatcher) getOrCreateJob(jobID string) *trackedJob {
	job, ok := d.jobs[jobID]
	if !ok {
		job = &trackedJob{
			jobID: jobID,
			state: protocol.State{
				Phase:     protocol.Committing,
				LastIndex: -1,
			},
			lastTickOrEntry: time.Now(),
		}
		d.jobs[jobID] = job
	}
	return job
}

func (d *Dispatcher) maybeSpawnTaskActor(job *trackedJob) {
	if d.stopped || !d.workerEnabled || job.taskActorAtom != "" || job.state.Announce == nil {
		return
	}
	if job.state.Finalize != nil || job.state.Phase == protocol.Expired {
		_ = RemoveTaskState(d.cfg.StateDir, job.jobID)
		return
	}

	_, hasPersisted := d.persistedJobs[job.jobID]
	if !hasPersisted {
		if !d.workerPolicy.AllowOwnJobs && job.state.Submitter == d.cfg.PublicKey {
			d.cfg.Logger.Info("ignoring own job announce", "job_id", job.jobID)
			return
		}
		if d.workerPolicy.ImagePolicy != nil {
			if err := d.workerPolicy.ImagePolicy.Allow(sandbox.ImageRef(job.state.Announce.ImageRef)); err != nil {
				d.cfg.Logger.Warn("ignoring job announce: image not allowed", "job_id", job.jobID, "image", job.state.Announce.ImageRef, "err", err)
				return
			}
		}
	}

	taskAtom := gen.Atom("task_" + job.jobID)
	job.taskActorAtom = taskAtom

	tCfg := TaskActorConfig{
		NodeID:            d.cfg.NodeID,
		PublicKey:         d.cfg.PublicKey,
		JobID:             job.jobID,
		StateDir:          d.cfg.StateDir,
		TickAfter:         d.cfg.TickAfter,
		HasLocalSubmitter: job.jobActorAtom != "",
		Dispatcher:        d.Name(),
		Bridge:            d.cfg.Bridge,
		Store:             d.cfg.Store,
		Runner:            d.cfg.Runner,
		RunCtx:            d.cfg.RunCtx,
		Logger:            d.cfg.Logger,
	}
	spec := act.SupervisorChildSpec{
		Name: taskAtom,
		Factory: func() gen.ProcessBehavior {
			return NewTaskActor(tCfg)
		},
		Restart: act.SupervisorChildRestart{
			Strategy: act.SupervisorStrategyTransient,
		},
	}
	_, _ = d.Call(d.cfg.Supervisor, SpawnChildReq{Spec: spec})
	d.scheduleTickCheck(job)
}

func (d *Dispatcher) notifyJobActors(job *trackedJob) {
	if job.state.Announce == nil {
		return
	}
	msg := JobStateUpdate{State: job.state}
	if job.jobActorAtom != "" {
		_ = d.Send(job.jobActorAtom, msg)
	}
	if job.taskActorAtom != "" {
		_ = d.Send(job.taskActorAtom, msg)
	}
}

func (d *Dispatcher) handleAcquireSlot(req AcquireSlotReq) {
	if d.stopped {
		return
	}
	if _, already := d.activeSlots[req.JobID]; already {
		_ = d.Send(req.Task, SlotGrantedMsg{JobID: req.JobID})
		return
	}
	maxConc := d.workerPolicy.MaxConcurrent
	if maxConc <= 0 {
		maxConc = 1
	}
	if len(d.activeSlots) < maxConc {
		d.activeSlots[req.JobID] = req.Task
		_ = d.Send(req.Task, SlotGrantedMsg{JobID: req.JobID})
		return
	}
	for _, q := range d.waitQueue {
		if q.JobID == req.JobID {
			return
		}
	}
	d.waitQueue = append(d.waitQueue, req)
}

func (d *Dispatcher) releaseSlot(jobID string) {
	delete(d.activeSlots, jobID)
	filtered := d.waitQueue[:0]
	for _, q := range d.waitQueue {
		if q.JobID != jobID {
			filtered = append(filtered, q)
		}
	}
	d.waitQueue = filtered

	if d.stopped {
		return
	}
	maxConc := d.workerPolicy.MaxConcurrent
	if maxConc <= 0 {
		maxConc = 1
	}
	for len(d.activeSlots) < maxConc && len(d.waitQueue) > 0 {
		next := d.waitQueue[0]
		d.waitQueue = d.waitQueue[1:]
		job := d.jobs[next.JobID]
		if job == nil || job.state.Finalize != nil || job.state.Phase == protocol.Expired {
			continue
		}
		d.activeSlots[next.JobID] = next.Task
		_ = d.Send(next.Task, SlotGrantedMsg{JobID: next.JobID})
	}
}

func (d *Dispatcher) handleChildStarted(msg ChildStartedMsg) {
	for _, job := range d.jobs {
		if job.jobActorAtom == msg.Name || job.taskActorAtom == msg.Name {
			if job.state.Announce != nil {
				_ = d.Send(msg.Name, JobStateUpdate{State: job.state})
			}
			if job.taskActorAtom == msg.Name && d.activeSlots[job.jobID] == msg.Name {
				_ = d.Send(msg.Name, SlotGrantedMsg{JobID: job.jobID})
			}
			return
		}
	}
}

func (d *Dispatcher) handleChildTerminated(msg ChildTerminatedMsg) {
	for _, job := range d.jobs {
		if job.taskActorAtom == msg.Name {
			if msg.Reason == gen.TerminateReasonNormal || msg.Reason == gen.TerminateReasonShutdown {
				d.releaseSlot(job.jobID)
			}
			return
		}
	}
}

// Local TickAfter timer only decides when to post a heartbeat Tick to advance quiet
// chains; protocol phases and deadlines remain strictly a function of chain indices.
func (d *Dispatcher) scheduleTickCheck(job *trackedJob) {
	if d.stopped || job.tickTimerActive || d.cfg.TickAfter <= 0 {
		return
	}
	if job.jobActorAtom == "" && job.taskActorAtom == "" {
		return
	}
	job.tickTimerActive = true
	_, _ = d.SendAfter(d.PID(), tickCheckMsg{JobID: job.jobID}, d.cfg.TickAfter)
}

func (d *Dispatcher) handleTickCheck(jobID string) {
	job := d.jobs[jobID]
	if job == nil {
		return
	}
	if d.stopped || job.state.Announce == nil || job.state.Finalize != nil || job.state.Finalizable ||
		(job.state.Phase != protocol.Committing && job.state.Phase != protocol.Revealing) ||
		job.ticksPosted >= d.cfg.MaxTicks {
		job.tickTimerActive = false
		return
	}

	if time.Since(job.lastTickOrEntry) >= d.cfg.TickAfter {
		job.lastTickOrEntry = time.Now()
		job.ticksPosted++
		env := &vcv1.Envelope{
			Version: 1,
			JobId:   jobID,
			Body: &vcv1.Envelope_Tick{
				Tick: &vcv1.Tick{JobId: jobID},
			},
		}
		_, _ = d.Call(d.cfg.Bridge, PublishReq{Envelope: env})
	}

	_, _ = d.SendAfter(d.PID(), tickCheckMsg{JobID: jobID}, d.cfg.TickAfter)
}

func EntryJobID(e protocol.Entry) string {
	if e.Envelope == nil {
		return ""
	}
	if e.Envelope.JobId != "" {
		return e.Envelope.JobId
	}
	switch b := e.Envelope.Body.(type) {
	case *vcv1.Envelope_Announce:
		id, err := protocol.JobID(b.Announce)
		if err == nil {
			return id
		}
	case *vcv1.Envelope_Tick:
		if b.Tick != nil {
			return b.Tick.JobId
		}
	}
	return ""
}
