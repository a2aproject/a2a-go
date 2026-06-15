// Copyright 2026 The A2A Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package eventqueue

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/a2aproject/a2a-go/v2/internal/taskupdate"
	"github.com/a2aproject/a2a-go/v2/log"
)

const (
	defaultPollInterval      = 30 * time.Second
	defaultInactivityTimeout = 5 * time.Minute
)

// PullCursor is an opaque type representing a cursor for the puller.
type PullCursor any

// PullResponse represents a response from the puller.
type PullResponse struct {
	Messages []*Message
	Cursor   PullCursor
}

// Puller is an interface for pulling events from the event queue.
type Puller interface {
	// Pull returns a response with messages and a cursor for the next pull.
	Pull(ctx context.Context, taskID a2a.TaskID, cursor PullCursor) (*PullResponse, error)
	// Close closes the puller.
	Close(ctx context.Context) error
}

// PullerProvider is a function that returns a puller for the given task ID.
type PullerProvider func(ctx context.Context, taskID a2a.TaskID) (Puller, error)

// NewStaticPullerProvider returns a PullerProvider that always returns the same Puller.
// The returned Puller's Close method is a no-op to prevent premature closing when
// multiple readers share the same puller.
func NewStaticPullerProvider(es Puller) PullerProvider {
	return func(ctx context.Context, taskID a2a.TaskID) (Puller, error) {
		return es, nil
	}
}

// PullConfig configures the behavior of a pull-based event queue manager.
type PullConfig struct {
	// PollInterval is the interval at which the puller is polled for new events.
	// Defaults to 30 seconds if not specified or <= 0.
	PollInterval time.Duration
	// InactivityTimeout is the duration of inactivity after which the reader will time out.
	// Defaults to 5 minutes. Set to 0 to disable inactivity timeout.
	InactivityTimeout time.Duration
	// AccessCheck is an optional callback executed before emitting the initial snapshot
	// of a task to verify that the calling context has permission to access the task.
	AccessCheck func(context.Context, *a2a.Task) error
	// OnInactivity is an optional callback function that is called when a task has exceeded the
	// InactivityTimeout. It's only triggered from Reader.Read().
	// The returned task is used to update the snapshot.
	OnInactivity func(context.Context, Puller, a2a.TaskID) (*a2a.Task, error)
	// UseInMemory is an optional function that returns true if the manager should bypass the puller
	// and use the in-memory queue instead for a given request context.
	UseInMemory func(context.Context) bool
}

var _ Manager = (*pullQueueManager)(nil)

type pullQueueManager struct {
	inner Manager
	pp    PullerProvider
	cfg   PullConfig
}

// NewPullQueueManager creates a new Manager that manages pull-based event queues.
// It uses the provided PullerProvider to instantiate pullers for tasks, and applies
// the configuration in PullConfig.
func NewPullQueueManager(pp PullerProvider, cfg PullConfig) Manager {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaultPollInterval
	}
	if cfg.InactivityTimeout < 0 {
		cfg.InactivityTimeout = defaultInactivityTimeout
	}
	if cfg.UseInMemory == nil {
		cfg.UseInMemory = func(context.Context) bool {
			return false
		}
	}

	return &pullQueueManager{
		inner: NewInMemoryManager(),
		pp:    pp,
		cfg:   cfg,
	}
}

// CreateReader implements Manager.CreateReader. It creates a new Reader for the given task ID.
// If UseInMemory is configured and returns true, it delegates to the in-memory manager.
// Otherwise, it uses the PullerProvider to get a Puller, fetches the initial task snapshot,
// and returns a pull-based Reader.
func (m *pullQueueManager) CreateReader(ctx context.Context, taskID a2a.TaskID) (Reader, error) {
	if m.cfg.UseInMemory(ctx) {
		return m.inner.CreateReader(ctx, taskID)
	}
	if m.pp == nil {
		return nil, fmt.Errorf("manager is missing puller provider")
	}
	puller, err := m.pp(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get puller: %w", err)
	}

	snapshot, err := getSnapshot(ctx, puller, taskID)
	if err != nil {
		return nil, fmt.Errorf("failed to get snapshot: %w", err)
	}

	return newPullReader(puller, taskID, m, snapshot), nil
}

// CreateWriter implements Manager.CreateWriter. It delegates to the in-memory manager.
func (m *pullQueueManager) CreateWriter(ctx context.Context, taskID a2a.TaskID) (Writer, error) {
	return m.inner.CreateWriter(ctx, taskID)
}

// Destroy implements Manager.Destroy. It delegates to the in-memory manager.
func (m *pullQueueManager) Destroy(ctx context.Context, taskID a2a.TaskID) error {
	return m.inner.Destroy(ctx, taskID)
}

var _ Reader = (*pullReader)(nil)

type pullReader struct {
	taskID      a2a.TaskID
	snapshot    *Message
	puller      Puller
	manager     *pullQueueManager
	eventsChan  chan *Message
	closed      chan struct{}
	closeSignal chan struct{}

	emittedSnapshot bool
}

func newPullReader(p Puller, taskID a2a.TaskID, queueManager *pullQueueManager, snapshot *Message) *pullReader {
	reader := &pullReader{
		taskID:      taskID,
		snapshot:    snapshot,
		puller:      p,
		manager:     queueManager,
		eventsChan:  make(chan *Message),
		closed:      make(chan struct{}),
		closeSignal: make(chan struct{}),
	}
	go reader.poll()
	return reader
}

func (r *pullReader) poll() {
	bgCtx, cancelBg := context.WithCancel(context.Background())
	ticker := time.NewTicker(r.manager.cfg.PollInterval)

	defer func() {
		ticker.Stop()
		cancelBg()
		if err := r.puller.Close(context.Background()); err != nil {
			log.Warn(bgCtx, "Error closing puller: %v", err)
		}
		close(r.eventsChan)
		close(r.closed)
	}()

	var cursor PullCursor
	for {
		resp, err := r.puller.Pull(bgCtx, r.taskID, cursor)
		if err != nil {
			log.Warn(bgCtx, "Error polling for events: %v", err)
		} else {
			cursor = resp.Cursor
			if r.dispatchMessages(resp) {
				return
			}
		}
		select {
		case <-r.closeSignal:
			return
		case <-ticker.C:
		}
	}
}

type stopPolling bool

func (r *pullReader) dispatchMessages(resp *PullResponse) stopPolling {
	if resp == nil {
		return false
	}
	for _, msg := range resp.Messages {
		if msg == nil || msg.Event == nil {
			continue
		}
		// Snapshot is emitted directly from r.snapshot by Read, so filter out to not send duplicate.
		if _, isTask := msg.Event.(*a2a.Task); isTask {
			continue
		}
		select {
		case r.eventsChan <- msg:
		case <-r.closeSignal:
			return true
		}
		if taskupdate.IsFinal(msg.Event) {
			return true
		}
	}
	return false
}

// Read implements Reader.Read. It returns the next message from the puller.
// The first call returns the initial task snapshot (after performing the optional AccessCheck).
// Subsequent calls block and return events polled from the puller.
// If the inactivity timeout is reached, it will trigger the OnInactivity callback if configured,
// and return ErrInactivityTimeout.
func (r *pullReader) Read(ctx context.Context) (*Message, error) {
	if !r.emittedSnapshot {
		if err := r.accessCheck(ctx); err != nil {
			return nil, err
		}
		r.emittedSnapshot = true
		return r.snapshot, nil
	}
	if taskupdate.IsFinal(r.snapshot.Event) {
		return nil, ErrQueueClosed
	}

	var timeout <-chan time.Time
	if r.manager.cfg.InactivityTimeout > 0 {
		timer := time.NewTimer(r.manager.cfg.InactivityTimeout)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case msg, ok := <-r.eventsChan:
		if !ok {
			return nil, ErrQueueClosed
		}
		return msg, nil
	case <-timeout:
		if r.manager.cfg.OnInactivity != nil {
			task, err := r.manager.cfg.OnInactivity(ctx, r.puller, r.taskID)
			if err != nil {
				return nil, fmt.Errorf("%w: failed to call inactivity callback:%w", ErrInactivityTimeout, err)
			}
			if task != nil {
				msg, err := newMessage(task)
				if err != nil {
					return nil, fmt.Errorf("failed to create new snapshot: %w", err)
				}
				r.snapshot = msg
				r.emittedSnapshot = false
			}
		}
		return nil, fmt.Errorf("%w after %v", ErrInactivityTimeout, r.manager.cfg.InactivityTimeout)
	}
}

// Close implements Reader.Close. It stops the polling goroutine and closes the underlying puller.
func (r *pullReader) Close() error {
	select {
	case r.closeSignal <- struct{}{}:
	case <-r.closed:
		return nil
	}

	<-r.closed
	return nil
}

func (r *pullReader) accessCheck(ctx context.Context) error {
	if r.manager.cfg.AccessCheck == nil {
		return nil
	}
	task, ok := r.snapshot.Event.(*a2a.Task)
	if !ok {
		return fmt.Errorf("snapshot event is not a task: %T", r.snapshot.Event)
	}
	return r.manager.cfg.AccessCheck(ctx, task)
}

func getSnapshot(ctx context.Context, puller Puller, taskID a2a.TaskID) (*Message, error) {
	resp, err := puller.Pull(ctx, taskID, nil)
	if err != nil {
		closePullerOnError(ctx, puller)
		return nil, fmt.Errorf("snapshot pull failed for task %v: %w", taskID, err)
	}
	if resp == nil || len(resp.Messages) == 0 {
		closePullerOnError(ctx, puller)
		return nil, fmt.Errorf("puller returned no snapshot for task %v", taskID)
	}
	snapshotMsg := resp.Messages[0]
	if snapshotMsg == nil || snapshotMsg.Event == nil {
		closePullerOnError(ctx, puller)
		return nil, fmt.Errorf("pull queue: puller returned nil snapshot message for task %q", taskID)
	}
	task, ok := snapshotMsg.Event.(*a2a.Task)
	if !ok {
		closePullerOnError(ctx, puller)
		return nil, fmt.Errorf("pull queue: puller's first message for task %q is %T, want *a2a.Task",
			taskID, snapshotMsg.Event)
	}
	if task.ID != taskID {
		closePullerOnError(ctx, puller)
		return nil, fmt.Errorf("pull queue: task ID mismatch in snapshot for task %q: got %q",
			taskID, task.ID)
	}
	return snapshotMsg, nil
}

func closePullerOnError(ctx context.Context, puller Puller) {
	if err := puller.Close(ctx); err != nil {
		log.Warn(ctx, "Error closing puller: %v", err)
	}
}

func newMessage(event a2a.Event) (*Message, error) {
	task, ok := event.(*a2a.Task)
	if !ok {
		return nil, fmt.Errorf("event is not a task: %T", event)
	}
	return &Message{
		Event:       task,
		TaskVersion: taskstore.TaskVersionMissing,
		Protocol:    a2a.Version,
	}, nil
}
