package tasksearchext

import "github.com/a2aproject/a2a-go/a2a"

var URI = "https://v1.tasksearchext.example.com"

var MethodName = "SearchTasks"

var JSONRPCMethodName = "SearchTasks"

type Request struct {
	Query string `json:"query"`
}

type Response struct {
	Tasks []*a2a.Task `json:"tasks"`
}

var Definition = a2a.AgentExtension{
	URI:         URI,
	Description: "Example method extension, helps to search for tasks on the A2A server.",
	Required:    false,
}
