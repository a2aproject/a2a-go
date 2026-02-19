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
	"time"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb/v1"
	"google.golang.org/protobuf/proto"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

func fromProtoMap(meta *structpb.Struct) map[string]any {
	if meta == nil {
		return nil
	}
	return meta.AsMap()
}

func FromProtoSendMessageRequest(req *a2apb.SendMessageRequest) (*a2a.SendMessageRequest, error) {
	if req == nil {
		return nil, nil
	}

	msg, err := FromProtoMessage(req.GetMessage())
	if err != nil {
		return nil, err
	}

	if msg == nil {
		return nil, fmt.Errorf("message cannot be nil")
	}

	config, err := fromProtoSendMessageConfig(req.GetConfiguration())
	if err != nil {
		return nil, err
	}

	params := &a2a.SendMessageRequest{
		Tenant:   req.GetTenant(),
		Message:  msg,
		Config:   config,
		Metadata: fromProtoMap(req.GetMetadata()),
	}
	return params, nil
}

func FromProtoMessage(pMsg *a2apb.Message) (*a2a.Message, error) {
	if pMsg == nil {
		return nil, nil
	}
	contentParts, err := fromProtoParts(pMsg.GetParts())
	if err != nil {
		return nil, err
	}
	if pMsg.GetMessageId() == "" {
		return nil, fmt.Errorf("message id cannot be empty")
	}
	if pMsg.GetRole() == a2apb.Role_ROLE_UNSPECIFIED {
		return nil, fmt.Errorf("message role cannot be unspecified")
	}

	msg := &a2a.Message{
		ID:         pMsg.GetMessageId(),
		ContextID:  pMsg.GetContextId(),
		Extensions: pMsg.GetExtensions(),
		Parts:      contentParts,
		TaskID:     a2a.TaskID(pMsg.GetTaskId()),
		Role:       fromProtoRole(pMsg.GetRole()),
		Metadata:   fromProtoMap(pMsg.GetMetadata()),
	}

	taskIDs := pMsg.GetReferenceTaskIds()
	if taskIDs != nil {
		msg.ReferenceTasks = make([]a2a.TaskID, len(taskIDs))
		for i, tid := range taskIDs {
			msg.ReferenceTasks[i] = a2a.TaskID(tid)
		}
	}

	return msg, nil
}

func fromProtoPart(p *a2apb.Part) (a2a.Part, error) {
	if p == nil {
		return a2a.Part{}, fmt.Errorf("part cannot be nil")
	}
	meta := fromProtoMap(p.Metadata)
	part := a2a.Part{
		Filename:  p.GetFilename(),
		MediaType: p.GetMediaType(),
		Metadata:  meta,
	}
	switch content := p.GetContent().(type) {
	case *a2apb.Part_Text:
		part.Content = a2a.Text(content.Text)
	case *a2apb.Part_Raw:
		part.Content = a2a.Raw(content.Raw)
	case *a2apb.Part_Data:
		part.Content = a2a.Data(content.Data.GetStructValue().AsMap())
	case *a2apb.Part_Url:
		part.Content = a2a.URL(content.Url)
	default:
		return a2a.Part{}, fmt.Errorf("unsupported part type: %T", content)
	}
	return part, nil
}

func fromProtoRole(role a2apb.Role) a2a.MessageRole {
	switch role {
	case a2apb.Role_ROLE_USER:
		return a2a.MessageRoleUser
	case a2apb.Role_ROLE_AGENT:
		return a2a.MessageRoleAgent
	default:
		return a2a.MessageRoleUnspecified
	}
}

func fromProtoPushConfig(pConf *a2apb.PushNotificationConfig) (*a2a.PushConfig, error) {
	if pConf == nil {
		return nil, nil
	}

	auth, err := fromProtoAuthenticationInfo(pConf.GetAuthentication())
	if err != nil {
		return nil, fmt.Errorf("failed to convert authentication info: %w", err)
	}
	url := pConf.GetUrl()
	if url == "" {
		return nil, fmt.Errorf("url cannot be empty")
	}

	result := &a2a.PushConfig{
		ID:    pConf.GetId(),
		URL:   url,
		Token: pConf.GetToken(),
		Auth:  auth,
	}

	return result, nil
}

func fromProtoAuthenticationInfo(pAuth *a2apb.AuthenticationInfo) (*a2a.PushAuthInfo, error) {
	if pAuth == nil {
		return nil, nil
	}
	scheme := pAuth.GetScheme()
	if scheme == "" {
		return nil, fmt.Errorf("authentication scheme cannot be empty")
	}
	return &a2a.PushAuthInfo{
		Scheme:      scheme,
		Credentials: pAuth.GetCredentials(),
	}, nil
}

func fromProtoSendMessageConfig(conf *a2apb.SendMessageConfiguration) (*a2a.SendMessageConfig, error) {
	if conf == nil {
		return nil, nil
	}

	pConf, err := fromProtoPushConfig(conf.GetPushNotificationConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to convert push config: %w", err)
	}

	result := &a2a.SendMessageConfig{
		AcceptedOutputModes: conf.GetAcceptedOutputModes(),
		Blocking:            proto.Bool(conf.GetBlocking()),
		PushConfig:          pConf,
	}

	// TODO: consider the approach after resolving https://github.com/a2aproject/A2A/issues/1072
	if conf.HistoryLength != nil && *conf.HistoryLength >= 0 {
		hl := int(*conf.HistoryLength)
		result.HistoryLength = &hl
	}
	return result, nil
}

func FromProtoGetTaskRequest(req *a2apb.GetTaskRequest) (*a2a.GetTaskRequest, error) {
	if req == nil {
		return nil, nil
	}

	// TODO: consider throwing an error when the path - req.GetName() is unexpected, e.g. tasks/taskID/someExtraText
	taskID := a2a.TaskID(req.GetId())
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}

	request := &a2a.GetTaskRequest{
		Tenant: req.GetTenant(),
		ID:     taskID,
	}
	if req.HistoryLength != nil && *req.HistoryLength >= 0 {
		historyLength := int(*req.HistoryLength)
		request.HistoryLength = &historyLength
	}
	return request, nil
}

func FromProtoCancelTaskRequest(req *a2apb.CancelTaskRequest) (*a2a.CancelTaskRequest, error) {
	if req == nil {
		return nil, nil
	}

	taskID := a2a.TaskID(req.GetId())
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}

	request := &a2a.CancelTaskRequest{
		Tenant: req.GetTenant(),
		ID:     taskID,
	}
	return request, nil
}

func FromProtoSubscribeToTaskRequest(req *a2apb.SubscribeToTaskRequest) (*a2a.SubscribeToTaskRequest, error) {
	if req == nil {
		return nil, nil
	}

	taskID := a2a.TaskID(req.GetId())
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}

	request := &a2a.SubscribeToTaskRequest{
		Tenant: req.GetTenant(),
		ID:     taskID,
	}
	return request, nil
}

func FromProtoListTasksRequest(req *a2apb.ListTasksRequest) (*a2a.ListTasksRequest, error) {
	if req == nil {
		return nil, nil
	}

	var lastUpdatedAfter *time.Time
	if req.GetStatusTimestampAfter() != nil {
		t := req.GetStatusTimestampAfter().AsTime()
		lastUpdatedAfter = &t
	}

	var status a2a.TaskState
	if req.GetStatus() != 0 {
		status = a2a.TaskState(req.GetStatus().String())
	}

	listTasksRequest := a2a.ListTasksRequest{
		Tenant:               req.GetTenant(),
		ContextID:            req.GetContextId(),
		Status:               status,
		PageSize:             int(req.GetPageSize()),
		PageToken:            req.GetPageToken(),
		StatusTimestampAfter: lastUpdatedAfter,
		IncludeArtifacts:     req.GetIncludeArtifacts(),
	}

	if req.HistoryLength != nil && *req.HistoryLength >= 0 {
		hl := int(*req.HistoryLength)
		listTasksRequest.HistoryLength = hl
	}

	return &listTasksRequest, nil
}

func FromProtoListTasksResponse(resp *a2apb.ListTasksResponse) (*a2a.ListTasksResponse, error) {
	if resp == nil {
		return nil, nil
	}

	var tasks []*a2a.Task
	for _, task := range resp.GetTasks() {
		t, err := FromProtoTask(task)
		if err != nil {
			return nil, fmt.Errorf("failed to convert task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return &a2a.ListTasksResponse{
		Tasks:         tasks,
		TotalSize:     int(resp.GetTotalSize()),
		PageSize:      int(resp.GetPageSize()),
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

func FromProtoCreateTaskPushConfigRequest(req *a2apb.CreateTaskPushNotificationConfigRequest) (*a2a.CreateTaskPushConfigRequest, error) {
	if req == nil {
		return nil, nil
	}

	config := req.GetConfig()
	if config == nil {
		return nil, fmt.Errorf("config is required")
	}

	pConf, err := fromProtoPushConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to convert push config: %w", err)
	}
	taskID := a2a.TaskID(req.GetTaskId())
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}

	return &a2a.CreateTaskPushConfigRequest{
		Tenant: req.GetTenant(),
		TaskID: taskID,
		Config: *pConf,
	}, nil
}

func FromProtoGetTaskPushConfigRequest(req *a2apb.GetTaskPushNotificationConfigRequest) (*a2a.GetTaskPushConfigRequest, error) {
	if req == nil {
		return nil, nil
	}

	taskID := a2a.TaskID(req.GetTaskId())
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	id := req.GetId()
	if id == "" {
		return nil, fmt.Errorf("config id cannot be empty")
	}

	return &a2a.GetTaskPushConfigRequest{
		Tenant: req.GetTenant(),
		TaskID: taskID,
		ID:     id,
	}, nil
}

func FromProtoDeleteTaskPushConfigRequest(req *a2apb.DeleteTaskPushNotificationConfigRequest) (*a2a.DeleteTaskPushConfigRequest, error) {
	if req == nil {
		return nil, nil
	}

	taskID := a2a.TaskID(req.GetTaskId())
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	id := req.GetId()
	if id == "" {
		return nil, fmt.Errorf("config id cannot be empty")
	}

	return &a2a.DeleteTaskPushConfigRequest{
		Tenant: req.GetTenant(),
		TaskID: taskID,
		ID:     id,
	}, nil
}

func FromProtoSendMessageResponse(resp *a2apb.SendMessageResponse) (a2a.SendMessageResult, error) {
	if resp == nil {
		return nil, nil
	}

	switch p := resp.Payload.(type) {
	case *a2apb.SendMessageResponse_Message:
		return FromProtoMessage(p.Message)
	case *a2apb.SendMessageResponse_Task:
		return FromProtoTask(p.Task)
	default:
		return nil, fmt.Errorf("unsupported SendMessageResponse payload type: %T", resp.Payload)
	}
}

func FromProtoStreamResponse(resp *a2apb.StreamResponse) (a2a.Event, error) {
	if resp == nil {
		return nil, nil
	}

	switch p := resp.Payload.(type) {
	case *a2apb.StreamResponse_Message:
		msg, err := FromProtoMessage(p.Message)
		if err != nil {
			return nil, err
		}
		return msg, nil
	case *a2apb.StreamResponse_Task:
		task, err := FromProtoTask(p.Task)
		if err != nil {
			return nil, err
		}
		return task, nil
	case *a2apb.StreamResponse_StatusUpdate:
		status, err := fromProtoTaskStatus(p.StatusUpdate.GetStatus())
		if err != nil {
			return nil, err
		}
		taskID := a2a.TaskID(p.StatusUpdate.GetTaskId())
		return &a2a.TaskStatusUpdateEvent{
			ContextID: p.StatusUpdate.GetContextId(),
			Status:    status,
			TaskID:    taskID,
			Metadata:  fromProtoMap(p.StatusUpdate.GetMetadata()),
		}, nil
	case *a2apb.StreamResponse_ArtifactUpdate:
		artifact, err := fromProtoArtifact(p.ArtifactUpdate.GetArtifact())
		if err != nil {
			return nil, err
		}
		taskID := a2a.TaskID(p.ArtifactUpdate.GetTaskId())
		return &a2a.TaskArtifactUpdateEvent{
			Append:    p.ArtifactUpdate.GetAppend(),
			Artifact:  artifact,
			ContextID: p.ArtifactUpdate.GetContextId(),
			LastChunk: p.ArtifactUpdate.GetLastChunk(),
			TaskID:    taskID,
			Metadata:  fromProtoMap(p.ArtifactUpdate.GetMetadata()),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported StreamResponse payload type: %T", resp.Payload)
	}
}

func fromProtoMessages(pMsgs []*a2apb.Message) ([]*a2a.Message, error) {
	msgs := make([]*a2a.Message, len(pMsgs))
	for i, pMsg := range pMsgs {
		msg, err := FromProtoMessage(pMsg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		msgs[i] = msg
	}
	return msgs, nil
}

func fromProtoParts(pParts []*a2apb.Part) (a2a.ContentParts, error) {
	contentParts := make([]*a2a.Part, len(pParts))
	for i, pPart := range pParts {
		part, err := fromProtoPart(pPart)
		if err != nil {
			return nil, fmt.Errorf("failed to convert part: %w", err)
		}
		contentParts[i] = &part
	}
	return contentParts, nil
}

func fromProtoTaskState(state a2apb.TaskState) a2a.TaskState {
	switch state {
	case a2apb.TaskState_TASK_STATE_AUTH_REQUIRED:
		return a2a.TaskStateAuthRequired
	case a2apb.TaskState_TASK_STATE_CANCELED:
		return a2a.TaskStateCanceled
	case a2apb.TaskState_TASK_STATE_COMPLETED:
		return a2a.TaskStateCompleted
	case a2apb.TaskState_TASK_STATE_FAILED:
		return a2a.TaskStateFailed
	case a2apb.TaskState_TASK_STATE_INPUT_REQUIRED:
		return a2a.TaskStateInputRequired
	case a2apb.TaskState_TASK_STATE_REJECTED:
		return a2a.TaskStateRejected
	case a2apb.TaskState_TASK_STATE_SUBMITTED:
		return a2a.TaskStateSubmitted
	case a2apb.TaskState_TASK_STATE_WORKING:
		return a2a.TaskStateWorking
	default:
		return a2a.TaskStateUnspecified
	}
}

func fromProtoTaskStatus(pStatus *a2apb.TaskStatus) (a2a.TaskStatus, error) {
	if pStatus == nil {
		return a2a.TaskStatus{}, fmt.Errorf("invalid status")
	}

	message, err := FromProtoMessage(pStatus.GetMessage())
	if err != nil {
		return a2a.TaskStatus{}, fmt.Errorf("failed to convert message for task status: %w", err)
	}

	status := a2a.TaskStatus{
		State:   fromProtoTaskState(pStatus.GetState()),
		Message: message,
	}

	if pStatus.Timestamp != nil {
		t := pStatus.Timestamp.AsTime()
		status.Timestamp = &t
	}

	return status, nil
}

func fromProtoArtifact(pArtifact *a2apb.Artifact) (*a2a.Artifact, error) {
	if pArtifact == nil {
		return nil, nil
	}

	contentParts, err := fromProtoParts(pArtifact.GetParts())
	if err != nil {
		return nil, fmt.Errorf("failed to convert from proto parts: %w", err)
	}
	id := a2a.ArtifactID(pArtifact.GetArtifactId())
	if id == "" {
		return nil, fmt.Errorf("artifact id cannot be empty")
	}

	return &a2a.Artifact{
		ID:          id,
		Name:        pArtifact.GetName(),
		Description: pArtifact.GetDescription(),
		Parts:       contentParts,
		Extensions:  pArtifact.GetExtensions(),
		Metadata:    fromProtoMap(pArtifact.GetMetadata()),
	}, nil
}

func fromProtoArtifacts(pArtifacts []*a2apb.Artifact) ([]*a2a.Artifact, error) {
	result := make([]*a2a.Artifact, len(pArtifacts))
	for i, pArtifact := range pArtifacts {
		artifact, err := fromProtoArtifact(pArtifact)
		if err != nil {
			return nil, fmt.Errorf("failed to convert artifact: %w", err)
		}
		result[i] = artifact
	}
	return result, nil
}

func FromProtoTask(pTask *a2apb.Task) (*a2a.Task, error) {
	if pTask == nil {
		return nil, nil
	}
	id := a2a.TaskID(pTask.GetId())
	if id == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	contextID := pTask.GetContextId()
	if contextID == "" {
		return nil, fmt.Errorf("context id cannot be empty")
	}
	status, err := fromProtoTaskStatus(pTask.Status)
	if err != nil {
		return nil, fmt.Errorf("failed to convert status: %w", err)
	}

	artifacts, err := fromProtoArtifacts(pTask.Artifacts)
	if err != nil {
		return nil, fmt.Errorf("failed to convert artifacts: %w", err)
	}

	history, err := fromProtoMessages(pTask.History)
	if err != nil {
		return nil, fmt.Errorf("failed to convert history: %w", err)
	}

	result := &a2a.Task{
		ID:        id,
		ContextID: contextID,
		Status:    status,
		Artifacts: artifacts,
		History:   history,
		Metadata:  fromProtoMap(pTask.GetMetadata()),
	}

	return result, nil
}

func FromProtoTaskPushConfig(pTaskConfig *a2apb.TaskPushNotificationConfig) (*a2a.TaskPushConfig, error) {
	if pTaskConfig == nil {
		return nil, nil
	}

	taskID := a2a.TaskID(pTaskConfig.GetTaskId())
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	id := pTaskConfig.GetId()
	if id == "" {
		return nil, fmt.Errorf("config id cannot be empty")
	}

	pConf := pTaskConfig.GetPushNotificationConfig()
	if pConf == nil {
		return nil, fmt.Errorf("push notification config cannot be empty")
	}

	if pConf.GetId() != id {
		return nil, fmt.Errorf("config id mismatch: %q != %q", pConf.GetId(), id)
	}

	config, err := fromProtoPushConfig(pConf)
	if err != nil {
		return nil, fmt.Errorf("failed to convert push config: %w", err)
	}

	return &a2a.TaskPushConfig{
		Tenant: pTaskConfig.GetTenant(),
		Config: *config,
		TaskID: taskID,
		ID:     id,
	}, nil
}

func FromProtoListTaskPushConfigResponse(resp *a2apb.ListTaskPushNotificationConfigResponse) (*a2a.ListTaskPushConfigResponse, error) {
	configs := make([]a2a.TaskPushConfig, len(resp.GetConfigs()))
	for i, pConfig := range resp.GetConfigs() {
		config, err := FromProtoTaskPushConfig(pConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to convert config: %w", err)
		}
		configs[i] = *config
	}
	return &a2a.ListTaskPushConfigResponse{
		Configs:       configs,
		NextPageToken: resp.GetNextPageToken(),
	}, nil
}

func FromProtoListTaskPushConfigRequest(req *a2apb.ListTaskPushNotificationConfigRequest) (*a2a.ListTaskPushConfigRequest, error) {
	if req == nil {
		return nil, nil
	}
	taskID := a2a.TaskID(req.GetTaskId())
	if taskID == "" {
		return nil, fmt.Errorf("task id cannot be empty")
	}
	return &a2a.ListTaskPushConfigRequest{
		Tenant:    req.GetTenant(),
		TaskID:    taskID,
		PageSize:  int(req.GetPageSize()),
		PageToken: req.GetPageToken(),
	}, nil
}

func fromProtoSupportedInterfaces(pInterfaces []*a2apb.AgentInterface) ([]*a2a.AgentInterface, error) {
	if pInterfaces == nil {
		return nil, nil
	}
	interfaces := make([]*a2a.AgentInterface, len(pInterfaces))
	for i, pIface := range pInterfaces {
		url := pIface.GetUrl()
		if url == "" {
			return nil, fmt.Errorf("url cannot be empty")
		}
		pb := pIface.GetProtocolBinding()
		if pb == "" {
			return nil, fmt.Errorf("protocol binding cannot be empty")
		}
		interfaces[i] = &a2a.AgentInterface{
			URL:             url,
			ProtocolBinding: a2a.TransportProtocol(pb),
			Tenant:          pIface.GetTenant(),
			ProtocolVersion: a2a.ProtocolVersion(pIface.GetProtocolVersion()),
		}
	}
	return interfaces, nil
}

func fromProtoAgentProvider(pProvider *a2apb.AgentProvider) (*a2a.AgentProvider, error) {
	if pProvider == nil {
		return nil, nil
	}
	org := pProvider.GetOrganization()
	if org == "" {
		return nil, fmt.Errorf("organization cannot be empty")
	}
	url := pProvider.GetUrl()
	if url == "" {
		return nil, fmt.Errorf("url cannot be empty")
	}
	return &a2a.AgentProvider{Org: org, URL: url}, nil
}

func fromProtoAgentExtensions(pExtensions []*a2apb.AgentExtension) ([]a2a.AgentExtension, error) {
	extensions := make([]a2a.AgentExtension, len(pExtensions))
	for i, pExt := range pExtensions {
		extensions[i] = a2a.AgentExtension{
			URI:         pExt.GetUri(),
			Description: pExt.GetDescription(),
			Required:    pExt.GetRequired(),
			Params:      pExt.GetParams().AsMap(),
		}
	}
	return extensions, nil
}

func fromProtoCapabilities(pCapabilities *a2apb.AgentCapabilities) (a2a.AgentCapabilities, error) {
	if pCapabilities == nil {
		return a2a.AgentCapabilities{}, fmt.Errorf("capabilities is nil")
	}
	extensions, err := fromProtoAgentExtensions(pCapabilities.GetExtensions())
	if err != nil {
		return a2a.AgentCapabilities{}, fmt.Errorf("failed to convert extensions: %w", err)
	}

	result := a2a.AgentCapabilities{
		PushNotifications: pCapabilities.GetPushNotifications(),
		Streaming:         pCapabilities.GetStreaming(),
		Extensions:        extensions,
		ExtendedAgentCard: pCapabilities.GetExtendedAgentCard(),
	}
	return result, nil
}

func fromProtoOAuthFlows(pFlows *a2apb.OAuthFlows) (a2a.OAuthFlows, error) {
	if pFlows == nil {
		return nil, fmt.Errorf("oauth flows is nil")
	}
	switch f := pFlows.Flow.(type) {
	case *a2apb.OAuthFlows_AuthorizationCode:
		return &a2a.AuthorizationCodeOAuthFlow{
			AuthorizationURL: f.AuthorizationCode.GetAuthorizationUrl(),
			TokenURL:         f.AuthorizationCode.GetTokenUrl(),
			RefreshURL:       f.AuthorizationCode.GetRefreshUrl(),
			Scopes:           f.AuthorizationCode.GetScopes(),
			PKCERequired:     f.AuthorizationCode.GetPkceRequired(),
		}, nil
	case *a2apb.OAuthFlows_ClientCredentials:
		return &a2a.ClientCredentialsOAuthFlow{
			TokenURL:   f.ClientCredentials.GetTokenUrl(),
			RefreshURL: f.ClientCredentials.GetRefreshUrl(),
			Scopes:     f.ClientCredentials.GetScopes(),
		}, nil
	case *a2apb.OAuthFlows_Implicit:
		return &a2a.ImplicitOAuthFlow{
			AuthorizationURL: f.Implicit.GetAuthorizationUrl(),
			RefreshURL:       f.Implicit.GetRefreshUrl(),
			Scopes:           f.Implicit.GetScopes(),
		}, nil
	case *a2apb.OAuthFlows_Password:
		return &a2a.PasswordOAuthFlow{
			TokenURL:   f.Password.GetTokenUrl(),
			RefreshURL: f.Password.GetRefreshUrl(),
			Scopes:     f.Password.GetScopes(),
		}, nil
	case *a2apb.OAuthFlows_DeviceCode:
		return &a2a.DeviceCodeOAuthFlow{
			DeviceAuthorizationURL: f.DeviceCode.GetDeviceAuthorizationUrl(),
			TokenURL:               f.DeviceCode.GetTokenUrl(),
			RefreshURL:             f.DeviceCode.GetRefreshUrl(),
			Scopes:                 f.DeviceCode.GetScopes(),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported oauth flow type: %T", f)
	}
}

func fromProtoSecurityScheme(pScheme *a2apb.SecurityScheme) (a2a.SecurityScheme, error) {
	if pScheme == nil {
		return nil, fmt.Errorf("security scheme is nil")
	}

	switch s := pScheme.Scheme.(type) {
	case *a2apb.SecurityScheme_ApiKeySecurityScheme:
		return a2a.APIKeySecurityScheme{
			Name:        s.ApiKeySecurityScheme.GetName(),
			Location:    a2a.APIKeySecuritySchemeLocation(s.ApiKeySecurityScheme.GetLocation()),
			Description: s.ApiKeySecurityScheme.GetDescription(),
		}, nil
	case *a2apb.SecurityScheme_HttpAuthSecurityScheme:
		return a2a.HTTPAuthSecurityScheme{
			Scheme:       s.HttpAuthSecurityScheme.GetScheme(),
			Description:  s.HttpAuthSecurityScheme.GetDescription(),
			BearerFormat: s.HttpAuthSecurityScheme.GetBearerFormat(),
		}, nil
	case *a2apb.SecurityScheme_OpenIdConnectSecurityScheme:
		return a2a.OpenIDConnectSecurityScheme{
			OpenIDConnectURL: s.OpenIdConnectSecurityScheme.GetOpenIdConnectUrl(),
			Description:      s.OpenIdConnectSecurityScheme.GetDescription(),
		}, nil
	case *a2apb.SecurityScheme_Oauth2SecurityScheme:
		flows, err := fromProtoOAuthFlows(s.Oauth2SecurityScheme.GetFlows())
		if err != nil {
			return nil, fmt.Errorf("failed to convert OAuthFlows: %w", err)
		}
		return a2a.OAuth2SecurityScheme{
			Flows:             flows,
			Oauth2MetadataURL: s.Oauth2SecurityScheme.GetOauth2MetadataUrl(),
			Description:       s.Oauth2SecurityScheme.GetDescription(),
		}, nil
	case *a2apb.SecurityScheme_MtlsSecurityScheme:
		return a2a.MutualTLSSecurityScheme{
			Description: s.MtlsSecurityScheme.GetDescription(),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported security scheme type: %T", s)
	}
}

func fromProtoSecuritySchemes(pSchemes map[string]*a2apb.SecurityScheme) (a2a.NamedSecuritySchemes, error) {
	schemes := make(a2a.NamedSecuritySchemes, len(pSchemes))
	for name, pScheme := range pSchemes {
		scheme, err := fromProtoSecurityScheme(pScheme)
		if err != nil {
			return nil, fmt.Errorf("failed to convert security scheme: %w", err)
		}
		schemes[a2a.SecuritySchemeName(name)] = scheme
	}
	return schemes, nil
}

func fromProtoSecurity(pSecurity []*a2apb.SecurityRequirement) a2a.SecurityRequirementsOptions {
	security := make(a2a.SecurityRequirementsOptions, len(pSecurity))
	for i, pSec := range pSecurity {
		schemes := make(map[a2a.SecuritySchemeName]a2a.SecuritySchemeScopes)
		for name, scopes := range pSec.Schemes {
			schemes[a2a.SecuritySchemeName(name)] = scopes.GetList()
		}
		security[i] = schemes
	}
	return security
}

func fromProtoSkills(pSkills []*a2apb.AgentSkill) ([]a2a.AgentSkill, error) {
	if pSkills == nil {
		return nil, nil
	}
	skills := make([]a2a.AgentSkill, len(pSkills))
	for i, pSkill := range pSkills {
		id := pSkill.GetId()
		if id == "" {
			return nil, fmt.Errorf("skill ID cannot be empty")
		}
		name := pSkill.GetName()
		if name == "" {
			return nil, fmt.Errorf("skill name cannot be empty")
		}
		description := pSkill.GetDescription()
		if description == "" {
			return nil, fmt.Errorf("skill description cannot be empty")
		}
		tags := pSkill.GetTags()
		if tags == nil {
			return nil, fmt.Errorf("skill tags cannot be empty")
		}

		skills[i] = a2a.AgentSkill{
			ID:                   id,
			Name:                 name,
			Description:          description,
			Tags:                 tags,
			Examples:             pSkill.GetExamples(),
			InputModes:           pSkill.GetInputModes(),
			OutputModes:          pSkill.GetOutputModes(),
			SecurityRequirements: fromProtoSecurity(pSkill.GetSecurityRequirements()),
		}
	}
	return skills, nil
}

func fromProtoAgentCardSignatures(in []*a2apb.AgentCardSignature) ([]a2a.AgentCardSignature, error) {
	if in == nil {
		return nil, nil
	}
	out := make([]a2a.AgentCardSignature, len(in))
	for i, v := range in {
		protected := v.GetProtected()
		if protected == "" {
			return nil, fmt.Errorf("signature protected cannot be empty")
		}
		signature := v.GetSignature()
		if signature == "" {
			return nil, fmt.Errorf("signature cannot be empty")
		}
		out[i] = a2a.AgentCardSignature{
			Protected: protected,
			Signature: signature,
			Header:    fromProtoMap(v.GetHeader()),
		}
	}
	return out, nil
}

func FromProtoAgentCard(pCard *a2apb.AgentCard) (*a2a.AgentCard, error) {
	if pCard == nil {
		return nil, nil
	}
	name := pCard.GetName()
	if name == "" {
		return nil, fmt.Errorf("agent card name cannot be empty")
	}
	description := pCard.GetDescription()
	if description == "" {
		return nil, fmt.Errorf("agent card description cannot be empty")
	}
	interfaces, err := fromProtoSupportedInterfaces(pCard.GetSupportedInterfaces())
	if err != nil {
		return nil, fmt.Errorf("failed to convert supported interfaces: %w", err)
	}
	if interfaces == nil {
		return nil, fmt.Errorf("supported interfaces cannot be empty")
	}
	provider, err := fromProtoAgentProvider(pCard.GetProvider())
	if err != nil {
		return nil, fmt.Errorf("failed to convert agent provider: %w", err)
	}
	version := pCard.GetVersion()
	if version == "" {
		return nil, fmt.Errorf("agent card version cannot be empty")
	}
	capabilities, err := fromProtoCapabilities(pCard.GetCapabilities())
	if err != nil {
		return nil, fmt.Errorf("failed to convert agent capabilities: %w", err)
	}
	defaultInputModes := pCard.GetDefaultInputModes()
	if defaultInputModes == nil {
		return nil, fmt.Errorf("default input modes cannot be empty")
	}
	defaultOutputModes := pCard.GetDefaultOutputModes()
	if defaultOutputModes == nil {
		return nil, fmt.Errorf("default output modes cannot be empty")
	}
	schemes, err := fromProtoSecuritySchemes(pCard.GetSecuritySchemes())
	if err != nil {
		return nil, fmt.Errorf("failed to convert security schemes: %w", err)
	}
	skills, err := fromProtoSkills(pCard.GetSkills())
	if err != nil {
		return nil, fmt.Errorf("failed to convert skills: %w", err)
	}
	if skills == nil {
		return nil, fmt.Errorf("skills cannot be empty")
	}
	signatures, err := fromProtoAgentCardSignatures(pCard.GetSignatures())
	if err != nil {
		return nil, fmt.Errorf("failed to convert signatures: %w", err)
	}

	result := &a2a.AgentCard{
		Name:                 name,
		Description:          description,
		SupportedInterfaces:  interfaces,
		Version:              version,
		DocumentationURL:     pCard.GetDocumentationUrl(),
		Capabilities:         capabilities,
		DefaultInputModes:    defaultInputModes,
		DefaultOutputModes:   defaultOutputModes,
		SecuritySchemes:      schemes,
		Provider:             provider,
		SecurityRequirements: fromProtoSecurity(pCard.GetSecurityRequirements()),
		Skills:               skills,
		IconURL:              pCard.GetIconUrl(),
		Signatures:           signatures,
	}

	return result, nil
}

func FromProtoGetExtendedAgentCardRequest(req *a2apb.GetExtendedAgentCardRequest) (*a2a.GetExtendedAgentCardRequest, error) {
	if req == nil {
		return nil, nil
	}

	return &a2a.GetExtendedAgentCardRequest{
		Tenant: req.GetTenant(),
	}, nil
}
