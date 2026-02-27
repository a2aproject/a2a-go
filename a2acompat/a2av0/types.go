// Copyright 2025 The A2A Authors
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

package a2av0

import (
	a2alegacy "github.com/a2aproject/a2a-go/a2a"
)

type sendMessageResult = a2alegacy.SendMessageResult
type event = a2alegacy.Event

func unmarshalEventJSON(data []byte) (event, error) {
	return a2alegacy.UnmarshalEventJSON(data)
}

type message = a2alegacy.Message
type task = a2alegacy.Task
type taskStatus = a2alegacy.TaskStatus
type taskState = a2alegacy.TaskState
type artifact = a2alegacy.Artifact
type taskArtifactUpdateEvent = a2alegacy.TaskArtifactUpdateEvent
type taskStatusUpdateEvent = a2alegacy.TaskStatusUpdateEvent
type contentParts = a2alegacy.ContentParts
type part = a2alegacy.Part
type textPart = a2alegacy.TextPart
type dataPart = a2alegacy.DataPart
type filePart = a2alegacy.FilePart
type fileMeta = a2alegacy.FileMeta
type fileBytes = a2alegacy.FileBytes
type fileURI = a2alegacy.FileURI
type taskIDParams = a2alegacy.TaskIDParams
type taskQueryParams = a2alegacy.TaskQueryParams
type messageSendConfig = a2alegacy.MessageSendConfig
type messageSendParams = a2alegacy.MessageSendParams
type getTaskPushConfigParams = a2alegacy.GetTaskPushConfigParams
type listTaskPushConfigParams = a2alegacy.ListTaskPushConfigParams
type deleteTaskPushConfigParams = a2alegacy.DeleteTaskPushConfigParams
type taskPushConfig = a2alegacy.TaskPushConfig
type pushConfig = a2alegacy.PushConfig
type pushAuthInfo = a2alegacy.PushAuthInfo
