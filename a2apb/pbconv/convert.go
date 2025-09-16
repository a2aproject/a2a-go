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

package pbconv

import (
	"fmt"
	"regexp"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	taskIDRegex   = regexp.MustCompile(`tasks/([^/]+)`)
	configIDRegex = regexp.MustCompile(`pushConfigs/([^/]+)`)
)

func ExtractTaskID(name string) (a2a.TaskID, error) {
	matches := taskIDRegex.FindStringSubmatch(name)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid or missing task ID in name: %q", name)
	}
	return a2a.TaskID(matches[1]), nil
}

func extractConfigID(name string) (string, error) {
	matches := configIDRegex.FindStringSubmatch(name)
	if len(matches) < 2 {
		return "", fmt.Errorf("invalid or missing config ID in name: %q", name)
	}
	return matches[1], nil
}

func FromProtoSendMessageRequest(req *a2apb.SendMessageRequest) (*a2a.MessageSendParams, error) {
	msg, err := fromProtoMessage(req.GetRequest())
	if err != nil {
		return &a2a.MessageSendParams{}, err
	}
	config, err := fromProtoSendMessageConfig(req.GetConfiguration())
	if err != nil {
		return &a2a.MessageSendParams{}, err
	}
	params := &a2a.MessageSendParams{
		Message: msg,
		Config:  config,
	}
	if req.GetMetadata() != nil {
		params.Metadata = req.GetMetadata().AsMap()
	}
	return params, nil
}

func fromProtoMessage(pMsg *a2apb.Message) (*a2a.Message, error) {
	if pMsg == nil {
		return nil, fmt.Errorf("proto message is nil")
	}
	parts := make([]a2a.Part, len(pMsg.GetContent()))
	for i, p := range pMsg.GetContent() {
		part, err := fromProtoPart(p)
		if err != nil {
			return nil, fmt.Errorf("failed to convert part: %w", err)
		}
		parts[i] = part
	}
	msg := &a2a.Message{
		ID:         pMsg.GetMessageId(),
		ContextID:  pMsg.GetContextId(),
		Extensions: pMsg.GetExtensions(),
		Parts:      parts,
		Role:       fromProtoRole(pMsg.GetRole()),
		TaskID:     a2a.TaskID(pMsg.GetTaskId()),
	}
	if pMsg.GetMetadata() != nil {
		msg.Metadata = pMsg.GetMetadata().AsMap()
	}
	return msg, nil
}

func fromProtoFilePart(pPart *a2apb.FilePart) (a2a.FilePart, error) {
	switch f := pPart.GetFile().(type) {
	case *a2apb.FilePart_FileWithBytes:
		return a2a.FilePart{
			File: a2a.FileBytes{
				FileMeta: a2a.FileMeta{
					MimeType: pPart.GetMimeType(),
				},
				Bytes: string(f.FileWithBytes),
			},
		}, nil
	case *a2apb.FilePart_FileWithUri:
		return a2a.FilePart{
			File: a2a.FileURI{
				FileMeta: a2a.FileMeta{
					MimeType: pPart.GetMimeType(),
				},
				URI: f.FileWithUri,
			},
		}, nil
	default:
		return a2a.FilePart{}, fmt.Errorf("unsupported FilePart type: %T", f)
	}
}

func fromProtoPart(p *a2apb.Part) (a2a.Part, error) {
	switch part := p.GetPart().(type) {
	case *a2apb.Part_Text:
		return a2a.TextPart{Text: part.Text}, nil
	case *a2apb.Part_Data:
		return a2a.DataPart{Data: part.Data.GetData().AsMap()}, nil
	case *a2apb.Part_File:
		return fromProtoFilePart(part.File)
	default:
		return nil, fmt.Errorf("unsupported part type: %T", part)
	}
}

func fromProtoRole(role a2apb.Role) a2a.MessageRole {
	switch role {
	case a2apb.Role_ROLE_USER:
		return a2a.MessageRoleUser
	case a2apb.Role_ROLE_AGENT:
		return a2a.MessageRoleAgent
	default:
		return "" // Corresponds to ROLE_UNSPECIFIED
	}
}

func fromProtoPushConfig(pConf *a2apb.PushNotificationConfig) (*a2a.PushConfig, error) {
	if pConf == nil || pConf.GetId() == "" {
		return nil, fmt.Errorf("invalid push config")
	}
	result := &a2a.PushConfig{
		ID:    pConf.GetId(),
		URL:   pConf.GetUrl(),
		Token: pConf.GetToken(),
	}
	if pConf.GetAuthentication() != nil {
		result.Auth = &a2a.PushAuthInfo{
			Schemes:     pConf.GetAuthentication().GetSchemes(),
			Credentials: pConf.GetAuthentication().GetCredentials(),
		}
	}
	return result, nil
}

func fromProtoSendMessageConfig(conf *a2apb.SendMessageConfiguration) (*a2a.MessageSendConfig, error) {
	if conf == nil {
		return nil, nil // config is optional field
	}
	result := &a2a.MessageSendConfig{
		AcceptedOutputModes: conf.GetAcceptedOutputModes(),
		Blocking:            conf.GetBlocking(),
	}

	if conf.GetPushNotification() != nil {
		pConf, err := fromProtoPushConfig(conf.GetPushNotification())
		if err != nil {
			return nil, fmt.Errorf("failed to convert push config: %w", err)
		}
		result.PushConfig = pConf
	}

	// TODO: consider the approach after resolving https://github.com/a2aproject/A2A/issues/1072
	if conf.HistoryLength > 0 {
		hl := int(conf.HistoryLength)
		result.HistoryLength = &hl
	}
	return result, nil
}

func FromProtoGetTaskRequest(req *a2apb.GetTaskRequest) (*a2a.TaskQueryParams, error) {
	// TODO: consider throwing an error when the path - req.GetName() is unexpected, e.g. tasks/taskID/someExtraText
	taskID, err := ExtractTaskID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract task id: %w", err)
	}
	params := &a2a.TaskQueryParams{
		ID: taskID,
	}
	if req.GetHistoryLength() > 0 {
		historyLength := int(req.GetHistoryLength())
		params.HistoryLength = &historyLength
	}
	return params, nil
}

func FromProtoCreateTaskPushConfigRequest(req *a2apb.CreateTaskPushNotificationConfigRequest) (*a2a.TaskPushConfig, error) {
	config := req.GetConfig()
	if config == nil || config.GetPushNotificationConfig() == nil {
		return nil, fmt.Errorf("invalid config")
	}
	pConf, err := fromProtoPushConfig(config.GetPushNotificationConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to convert push config: %w", err)
	}
	taskID, err := ExtractTaskID(req.GetParent())
	if err != nil {
		return nil, fmt.Errorf("failed to extract task id: %w", err)
	}

	return &a2a.TaskPushConfig{
		TaskID: taskID,
		Config: *pConf,
	}, nil
}

func FromProtoGetTaskPushConfigRequest(req *a2apb.GetTaskPushNotificationConfigRequest) (*a2a.GetTaskPushConfigParams, error) {
	taskID, err := ExtractTaskID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract task id: %w", err)
	}
	configID, err := extractConfigID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract config id: %w", err)
	}
	return &a2a.GetTaskPushConfigParams{
		TaskID:   taskID,
		ConfigID: configID,
	}, nil
}

func FromProtoDeleteTaskPushConfigRequest(req *a2apb.DeleteTaskPushNotificationConfigRequest) (*a2a.DeleteTaskPushConfigParams, error) {
	taskID, err := ExtractTaskID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract task id: %w", err)
	}
	configID, err := extractConfigID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract config id: %w", err)
	}
	return &a2a.DeleteTaskPushConfigParams{
		TaskID:   taskID,
		ConfigID: configID,
	}, nil
}

func ToProtoSendMessageResponse(result a2a.SendMessageResult) (*a2apb.SendMessageResponse, error) {
	resp := &a2apb.SendMessageResponse{}
	switch r := result.(type) {
	case *a2a.Message:
		pMsg, err := toProtoMessage(r)
		if err != nil {
			return nil, err
		}
		resp.Payload = &a2apb.SendMessageResponse_Msg{Msg: pMsg}
	case *a2a.Task:
		pTask, err := ToProtoTask(r)
		if err != nil {
			return nil, err
		}
		resp.Payload = &a2apb.SendMessageResponse_Task{Task: pTask}
	default:
		return nil, fmt.Errorf("unsupported SendMessageResult type: %T", result)
	}
	return resp, nil
}

func ToProtoStreamResponse(event a2a.Event) (*a2apb.StreamResponse, error) {
	resp := &a2apb.StreamResponse{}
	switch e := event.(type) {
	case *a2a.Message:
		pMsg, err := toProtoMessage(e)
		if err != nil {
			return nil, err
		}
		resp.Payload = &a2apb.StreamResponse_Msg{Msg: pMsg}
	case *a2a.Task:
		pTask, err := ToProtoTask(e)
		if err != nil {
			return nil, err
		}
		resp.Payload = &a2apb.StreamResponse_Task{Task: pTask}
	case *a2a.TaskStatusUpdateEvent:
		pStatus, err := toProtoTaskStatus(e.Status)
		if err != nil {
			return nil, err
		}
		var metadata *structpb.Struct
		if e.Metadata != nil {
			metadata, err = structpb.NewStruct(e.Metadata)
			if err != nil {
				return nil, err
			}
		}
		resp.Payload = &a2apb.StreamResponse_StatusUpdate{StatusUpdate: &a2apb.TaskStatusUpdateEvent{
			ContextId: e.ContextID,
			Final:     e.Final,
			Status:    pStatus,
			TaskId:    string(e.TaskID),
			Metadata:  metadata,
		}}
	case *a2a.TaskArtifactUpdateEvent:
		pArtifact, err := toProtoArtifact(e.Artifact)
		if err != nil {
			return nil, err
		}
		var metadata *structpb.Struct
		if e.Metadata != nil {
			metadata, err = structpb.NewStruct(e.Metadata)
			if err != nil {
				return nil, err
			}
		}
		resp.Payload = &a2apb.StreamResponse_ArtifactUpdate{
			ArtifactUpdate: &a2apb.TaskArtifactUpdateEvent{
				Append:    e.Append,
				Artifact:  pArtifact,
				ContextId: e.ContextID,
				LastChunk: e.LastChunk,
				TaskId:    string(e.TaskID),
				Metadata:  metadata,
			}}
	default:
		return nil, fmt.Errorf("unsupported Event type: %T", event)
	}
	return resp, nil
}

func toProtoMessage(msg *a2a.Message) (*a2apb.Message, error) {
	if msg == nil {
		return nil, fmt.Errorf("message is nil")
	}
	parts := make([]*a2apb.Part, len(msg.Parts))
	for i, p := range msg.Parts {
		part, err := toProtoPart(p)
		if err != nil {
			return nil, fmt.Errorf("failed to convert part: %w", err)
		}
		parts[i] = part
	}

	var pMetadata *structpb.Struct
	if msg.Metadata != nil {
		s, err := structpb.NewStruct(msg.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to convert metadata to proto struct: %w", err)
		}
		pMetadata = s
	}

	return &a2apb.Message{
		MessageId:  msg.ID,
		ContextId:  msg.ContextID,
		Extensions: msg.Extensions,
		Content:    parts,
		Role:       toProtoRole(msg.Role),
		TaskId:     string(msg.TaskID),
		Metadata:   pMetadata,
	}, nil
}

func toProtoMessages(msgs []*a2a.Message) ([]*a2apb.Message, error) {
	pMsgs := make([]*a2apb.Message, len(msgs))
	for i, msg := range msgs {
		pMsg, err := toProtoMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		pMsgs[i] = pMsg
	}
	return pMsgs, nil
}

func toProtoFilePart(part a2a.FilePart) (*a2apb.Part, error) {
	switch fc := part.File.(type) {
	case a2a.FileBytes:
		return &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
			MimeType: fc.MimeType,
			File:     &a2apb.FilePart_FileWithBytes{FileWithBytes: []byte(fc.Bytes)},
		}}}, nil
	case a2a.FileURI:
		return &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
			MimeType: fc.MimeType,
			File:     &a2apb.FilePart_FileWithUri{FileWithUri: fc.URI},
		}}}, nil
	default:
		return nil, fmt.Errorf("unsupported FilePartContent type: %T", fc)
	}
}

func toProtoDataPart(part a2a.DataPart) (*a2apb.Part, error) {
	s, err := structpb.NewStruct(part.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to convert data to proto struct: %w", err)
	}
	return &a2apb.Part{Part: &a2apb.Part_Data{Data: &a2apb.DataPart{
		Data: s,
	}}}, nil
}

func toProtoPart(part a2a.Part) (*a2apb.Part, error) {
	switch p := part.(type) {
	case a2a.TextPart:
		return &a2apb.Part{Part: &a2apb.Part_Text{Text: p.Text}}, nil
	case a2a.DataPart:
		return toProtoDataPart(p)
	case a2a.FilePart:
		return toProtoFilePart(p)
	default:
		return nil, fmt.Errorf("unsupported part type: %T", p)
	}
}

func toProtoParts(parts []a2a.Part) ([]*a2apb.Part, error) {
	pParts := make([]*a2apb.Part, len(parts))
	for i, part := range parts {
		pPart, err := toProtoPart(part)
		if err != nil {
			return nil, fmt.Errorf("failed to convert part: %w", err)
		}
		pParts[i] = pPart
	}
	return pParts, nil
}

func toProtoRole(role a2a.MessageRole) a2apb.Role {
	switch role {
	case a2a.MessageRoleUser:
		return a2apb.Role_ROLE_USER
	case a2a.MessageRoleAgent:
		return a2apb.Role_ROLE_AGENT
	default:
		return a2apb.Role_ROLE_UNSPECIFIED
	}
}

func toProtoTaskState(state a2a.TaskState) a2apb.TaskState {
	switch state {
	case a2a.TaskStateAuthRequired:
		return a2apb.TaskState_TASK_STATE_AUTH_REQUIRED
	case a2a.TaskStateCanceled:
		return a2apb.TaskState_TASK_STATE_CANCELLED
	case a2a.TaskStateCompleted:
		return a2apb.TaskState_TASK_STATE_COMPLETED
	case a2a.TaskStateFailed:
		return a2apb.TaskState_TASK_STATE_FAILED
	case a2a.TaskStateInputRequired:
		return a2apb.TaskState_TASK_STATE_INPUT_REQUIRED
	case a2a.TaskStateRejected:
		return a2apb.TaskState_TASK_STATE_REJECTED
	case a2a.TaskStateSubmitted:
		return a2apb.TaskState_TASK_STATE_SUBMITTED
	case a2a.TaskStateWorking:
		return a2apb.TaskState_TASK_STATE_WORKING
	default:
		return a2apb.TaskState_TASK_STATE_UNSPECIFIED
	}
}

func toProtoTaskStatus(status a2a.TaskStatus) (*a2apb.TaskStatus, error) {
	if status == (a2a.TaskStatus{}) {
		return nil, fmt.Errorf("invalid status")
	}

	pStatus := &a2apb.TaskStatus{
		State: toProtoTaskState(status.State),
	}

	if status.Message != nil {
		message, err := toProtoMessage(status.Message)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message for task status: %w", err)
		}
		pStatus.Update = message
	}

	if status.Timestamp != nil {
		pStatus.Timestamp = timestamppb.New(*status.Timestamp)
	}

	return pStatus, nil
}

func toProtoArtifact(artifact *a2a.Artifact) (*a2apb.Artifact, error) {
	if artifact == nil {
		return nil, nil
	}
	var metadata *structpb.Struct
	if artifact.Metadata != nil {
		var err error
		metadata, err = structpb.NewStruct(artifact.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to convert metadata to proto struct: %w", err)
		}
	}
	parts, err := toProtoParts(artifact.Parts)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to proto parts: %w", err)
	}
	return &a2apb.Artifact{
		ArtifactId:  string(artifact.ID),
		Name:        artifact.Name,
		Description: artifact.Description,
		Parts:       parts,
		Metadata:    metadata,
		Extensions:  artifact.Extensions,
	}, nil
}

func toProtoArtifacts(artifacts []*a2a.Artifact) ([]*a2apb.Artifact, error) {
	result := make([]*a2apb.Artifact, len(artifacts))
	for i, artifact := range artifacts {
		pArtifact, err := toProtoArtifact(artifact)
		if err != nil {
			return nil, fmt.Errorf("failed to convert artifact: %w", err)
		}
		if pArtifact != nil {
			result[i] = pArtifact
		}
	}
	return result, nil
}

func ToProtoTask(task *a2a.Task) (*a2apb.Task, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}

	status, err := toProtoTaskStatus(task.Status)
	if err != nil {
		return nil, fmt.Errorf("failed to convert status: %w", err)
	}

	result := &a2apb.Task{
		Id:        string(task.ID),
		ContextId: task.ContextID,
		Status:    status,
	}

	if len(task.Artifacts) > 0 {
		artifacts, err := toProtoArtifacts(task.Artifacts)
		if err != nil {
			return nil, fmt.Errorf("failed to convert artifacts: %w", err)
		}
		result.Artifacts = artifacts
	}

	if len(task.History) > 0 {
		history, err := toProtoMessages(task.History)
		if err != nil {
			return nil, fmt.Errorf("failed to convert history: %w", err)
		}
		result.History = history
	}

	if task.Metadata != nil {
		metadata, err := structpb.NewStruct(task.Metadata)
		if err != nil {
			return nil, fmt.Errorf("failed to convert metadata to proto struct: %w", err)
		}
		result.Metadata = metadata
	}

	return result, nil
}

func ToProtoTaskPushConfig(config *a2a.TaskPushConfig) (*a2apb.TaskPushNotificationConfig, error) {
	if config == nil || config.Config.ID == "" {
		return nil, fmt.Errorf("invalid config")
	}

	pConfig := &a2apb.PushNotificationConfig{
		Id:    config.Config.ID,
		Url:   config.Config.URL,
		Token: config.Config.Token,
	}

	if config.Config.Auth != nil {
		pConfig.Authentication = &a2apb.AuthenticationInfo{
			Schemes:     config.Config.Auth.Schemes,
			Credentials: config.Config.Auth.Credentials,
		}
	}

	return &a2apb.TaskPushNotificationConfig{
		Name:                   fmt.Sprintf("tasks/%s/pushConfigs/%s", config.TaskID, pConfig.GetId()),
		PushNotificationConfig: pConfig,
	}, nil
}

func ToProtoListTaskPushConfig(configs []*a2a.TaskPushConfig) (*a2apb.ListTaskPushNotificationConfigResponse, error) {
	pConfigs := make([]*a2apb.TaskPushNotificationConfig, len(configs))
	for i, config := range configs {
		pConfig, err := ToProtoTaskPushConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to convert config: %w", err)
		}
		pConfigs[i] = pConfig
	}
	return &a2apb.ListTaskPushNotificationConfigResponse{
		Configs:       pConfigs,
		NextPageToken: "", // todo: add pagination
	}, nil
}

func toProtoAdditionalInterfaces(interfaces []a2a.AgentInterface) []*a2apb.AgentInterface {
	pInterfaces := make([]*a2apb.AgentInterface, len(interfaces))
	for i, iface := range interfaces {
		pInterfaces[i] = &a2apb.AgentInterface{
			Transport: string(iface.Transport),
			Url:       iface.URL,
		}
	}
	return pInterfaces
}

func toProtoAgentProvider(provider *a2a.AgentProvider) *a2apb.AgentProvider {
	if provider == nil {
		return nil
	}
	return &a2apb.AgentProvider{
		Organization: provider.Org,
		Url:          provider.URL,
	}
}

func toProtoAgentExtensions(extensions []a2a.AgentExtension) ([]*a2apb.AgentExtension, error) {
	pExtensions := make([]*a2apb.AgentExtension, len(extensions))
	for i, ext := range extensions {
		params, err := structpb.NewStruct(ext.Params)
		if err != nil {
			return nil, fmt.Errorf("failed to convert extension params: %w", err)
		}
		pExtensions[i] = &a2apb.AgentExtension{
			Uri:         ext.URI,
			Description: ext.Description,
			Required:    ext.Required,
			Params:      params,
		}
	}
	return pExtensions, nil
}

func toProtoCapabilities(capabilities a2a.AgentCapabilities) (*a2apb.AgentCapabilities, error) {
	result := &a2apb.AgentCapabilities{
		PushNotifications: capabilities.PushNotifications,
		Streaming:         capabilities.Streaming,
	}

	if len(capabilities.Extensions) > 0 {
		extensions, err := toProtoAgentExtensions(capabilities.Extensions)
		if err != nil {
			return nil, fmt.Errorf("failed to convert extensions: %w", err)
		}
		result.Extensions = extensions
	}

	return result, nil
}

func toProtoOAuthFlows_AuthorizationCode(f *a2a.AuthorizationCodeOAuthFlow) *a2apb.OAuthFlows {
	return &a2apb.OAuthFlows{
		Flow: &a2apb.OAuthFlows_AuthorizationCode{
			AuthorizationCode: &a2apb.AuthorizationCodeOAuthFlow{
				AuthorizationUrl: f.AuthorizationURL,
				TokenUrl:         f.TokenURL,
				RefreshUrl:       f.RefreshURL,
				Scopes:           f.Scopes,
			},
		},
	}
}

func toProtoOAuthFlows_ClientCredentials(f *a2a.ClientCredentialsOAuthFlow) *a2apb.OAuthFlows {
	return &a2apb.OAuthFlows{
		Flow: &a2apb.OAuthFlows_ClientCredentials{
			ClientCredentials: &a2apb.ClientCredentialsOAuthFlow{
				TokenUrl:   f.TokenURL,
				RefreshUrl: f.RefreshURL,
				Scopes:     f.Scopes,
			},
		},
	}
}

func toProtoOAuthFlows_Implicit(f *a2a.ImplicitOAuthFlow) *a2apb.OAuthFlows {
	return &a2apb.OAuthFlows{
		Flow: &a2apb.OAuthFlows_Implicit{
			Implicit: &a2apb.ImplicitOAuthFlow{
				AuthorizationUrl: f.AuthorizationURL,
				RefreshUrl:       f.RefreshURL,
				Scopes:           f.Scopes,
			},
		},
	}
}

func toProtoOAuthFlows_Password(f *a2a.PasswordOAuthFlow) *a2apb.OAuthFlows {
	return &a2apb.OAuthFlows{
		Flow: &a2apb.OAuthFlows_Password{
			Password: &a2apb.PasswordOAuthFlow{
				TokenUrl:   f.TokenURL,
				RefreshUrl: f.RefreshURL,
				Scopes:     f.Scopes,
			},
		},
	}
}

func toProtoOAuthFlows(flows a2a.OAuthFlows) (*a2apb.OAuthFlows, error) {
	var result []*a2apb.OAuthFlows

	if flows.AuthorizationCode != nil {
		result = append(result, toProtoOAuthFlows_AuthorizationCode(flows.AuthorizationCode))
	}
	if flows.ClientCredentials != nil {
		result = append(result, toProtoOAuthFlows_ClientCredentials(flows.ClientCredentials))
	}
	if flows.Implicit != nil {
		result = append(result, toProtoOAuthFlows_Implicit(flows.Implicit))
	}
	if flows.Password != nil {
		result = append(result, toProtoOAuthFlows_Password(flows.Password))
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no OAuthFlows found")
	}

	if len(result) > 1 {
		return nil, fmt.Errorf("only one OAuthFlow is allowed")
	}

	return result[0], nil
}

func toProtoSecurityScheme(scheme a2a.SecurityScheme) (*a2apb.SecurityScheme, error) {
	switch s := scheme.(type) {
	case a2a.APIKeySecurityScheme:
		return &a2apb.SecurityScheme{
			Scheme: &a2apb.SecurityScheme_ApiKeySecurityScheme{
				ApiKeySecurityScheme: &a2apb.APIKeySecurityScheme{
					Name:        s.Name,
					Location:    string(s.In),
					Description: s.Description},
			},
		}, nil
	case a2a.HTTPAuthSecurityScheme:
		return &a2apb.SecurityScheme{
			Scheme: &a2apb.SecurityScheme_HttpAuthSecurityScheme{
				HttpAuthSecurityScheme: &a2apb.HTTPAuthSecurityScheme{
					Scheme:       string(s.Scheme),
					Description:  s.Description,
					BearerFormat: s.BearerFormat,
				},
			},
		}, nil
	case a2a.OpenIDConnectSecurityScheme:
		return &a2apb.SecurityScheme{
			Scheme: &a2apb.SecurityScheme_OpenIdConnectSecurityScheme{
				OpenIdConnectSecurityScheme: &a2apb.OpenIdConnectSecurityScheme{
					OpenIdConnectUrl: s.OpenIDConnectURL,
					Description:      s.Description,
				},
			},
		}, nil
	case a2a.MutualTLSSecurityScheme:
		return nil, nil
	case a2a.OAuth2SecurityScheme:
		flows, err := toProtoOAuthFlows(s.Flows)
		if err != nil {
			return nil, fmt.Errorf("failed to convert OAuthFlows: %w", err)
		}
		return &a2apb.SecurityScheme{
			Scheme: &a2apb.SecurityScheme_Oauth2SecurityScheme{
				Oauth2SecurityScheme: &a2apb.OAuth2SecurityScheme{
					Flows:       flows,
					Description: s.Description,
				},
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported security scheme type: %T", s)
	}
}

func totoProtoSecuritySchemes(schemes a2a.NamedSecuritySchemes) (map[string]*a2apb.SecurityScheme, error) {
	pSchemes := make(map[string]*a2apb.SecurityScheme, len(schemes))
	for name, scheme := range schemes {
		pScheme, err := toProtoSecurityScheme(scheme)
		if err != nil {
			return nil, fmt.Errorf("failed to convert security scheme: %w", err)
		}
		if pScheme != nil {
			pSchemes[string(name)] = pScheme
		}
	}
	return pSchemes, nil
}

func toProtoSecurity(security []a2a.SecurityRequirements) []*a2apb.Security {
	pSecurity := make([]*a2apb.Security, len(security))
	for i, sec := range security {
		pSchemes := make(map[string]*a2apb.StringList)
		for name, scopes := range sec {
			pSchemes[string(name)] = &a2apb.StringList{
				List: scopes,
			}
		}
		pSecurity[i] = &a2apb.Security{
			Schemes: pSchemes,
		}
	}
	return pSecurity
}

func toProtoSkills(skills []a2a.AgentSkill) []*a2apb.AgentSkill {
	pSkills := make([]*a2apb.AgentSkill, len(skills))
	for i, skill := range skills {
		pSkills[i] = &a2apb.AgentSkill{
			Id:          skill.ID,
			Name:        skill.Name,
			Description: skill.Description,
			Tags:        skill.Tags,
			Examples:    skill.Examples,
			InputModes:  skill.InputModes,
			OutputModes: skill.OutputModes,
		}
	}
	return pSkills
}

func ToProtoAgentCard(card *a2a.AgentCard) (*a2apb.AgentCard, error) {
	if card == nil {
		return nil, fmt.Errorf("agent card not found")
	}

	capabilities, err := toProtoCapabilities(card.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to convert agent capabilities: %w", err)
	}

	result := &a2apb.AgentCard{
		ProtocolVersion:                   card.ProtocolVersion,
		Name:                              card.Name,
		Description:                       card.Description,
		Url:                               card.URL,
		PreferredTransport:                string(card.PreferredTransport),
		Provider:                          toProtoAgentProvider(card.Provider),
		Version:                           card.Version,
		DocumentationUrl:                  card.DocumentationURL,
		Capabilities:                      capabilities,
		DefaultInputModes:                 card.DefaultInputModes,
		DefaultOutputModes:                card.DefaultOutputModes,
		SupportsAuthenticatedExtendedCard: card.SupportsAuthenticatedExtendedCard,
	}

	if card.SecuritySchemes != nil {
		schemes, err := totoProtoSecuritySchemes(card.SecuritySchemes)
		if err != nil {
			return nil, fmt.Errorf("failed to convert security schemes: %w", err)
		}
		result.SecuritySchemes = schemes
	}

	if len(card.AdditionalInterfaces) > 0 {
		result.AdditionalInterfaces = toProtoAdditionalInterfaces(card.AdditionalInterfaces)
	}

	if len(card.Security) > 0 {
		result.Security = toProtoSecurity(card.Security)
	}

	if len(card.Skills) > 0 {
		result.Skills = toProtoSkills(card.Skills)
	}

	return result, nil
}
