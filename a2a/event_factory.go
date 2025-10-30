package a2a



// TaskEventFactory is a utility for creating task events without the need to pass taskID and contextID around.
type TaskEventFactory struct {
	taskInfoProvider TaskInfoProvider
}

// NewTaskEventFactory creates a new TaskEventFactory.
func NewTaskEventFactory(provider TaskInfoProvider) *TaskEventFactory {
	return &TaskEventFactory{taskInfoProvider: provider}
}

// NewStatusUpdate create a new TaskStatusUpdateEvent related to the task.
func (w *TaskEventFactory) NewStatusUpdate(state TaskState, msg *Message) *TaskStatusUpdateEvent {
	return NewStatusUpdateEvent(w.taskInfoProvider, state, msg)
}

// NewArtifactEvent creates a new TaskArtifactUpdateEvent which represents a new artifact creation.
func (w *TaskEventFactory) NewArtifactEvent(parts ...Part) *TaskArtifactUpdateEvent {
	return NewArtifactEvent(w.taskInfoProvider, parts...)
}

// NewArtifactUpdateEvent creates a new TaskArtifactUpdateEvent which represents a modification of an existing artifact.
func (w *TaskEventFactory) NewArtifactUpdateEvent(aid ArtifactID, parts ...Part) *TaskArtifactUpdateEvent {
	return NewArtifactUpdateEvent(w.taskInfoProvider, aid, parts...)
}

// NewMessage creates a new Message related to the task.
func (w *TaskEventFactory) NewMessage(parts ...Part) *Message {
	return NewMessageForTask(MessageRoleAgent, w.taskInfoProvider, parts...)
}
