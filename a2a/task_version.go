package a2a

// TaskVersion is a version of the task stored on the server.
type TaskVersion interface {
	After(another TaskVersion) bool
}

type taskVersionMissingType struct{}

// TaskVersionMissing is a special value used to denote that task version is not being tracked.
var TaskVersionMissing TaskVersion = taskVersionMissingType{}

func (taskVersionMissingType) After(another TaskVersion) bool {
	// Consider every state "latest" if the version is not tracked.
	return true
}

// TaskVersionInt is a version of the task stored on the server represented as an int64.
type TaskVersionInt int64

var _ TaskVersion = TaskVersionInt(0)

func (i TaskVersionInt) After(another TaskVersion) bool {
	if ai, ok := another.(TaskVersionInt); ok {
		return i > ai
	}
	return false
}
