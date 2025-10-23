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

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
	structpb "google.golang.org/protobuf/types/known/structpb"
)

func fromProtoMetadata(meta *structpb.Struct) map[string]any {
	if meta == nil {
		return nil
	}
	return meta.AsMap()
}

func FromProtoSendMessageRequest(req *a2apb.SendMessageRequest) (*a2a.MessageSendParams, error) {
	if req == nil {
		return nil, nil
	}

	msg, err := FromProtoMessage(req.GetRequest())
	if err != nil {
		return nil, err
	}

	config, err := fromProtoSendMessageConfig(req.GetConfiguration())
	if err != nil {
		return nil, err
	}

	params := &a2a.MessageSendParams{
		Message:  msg,
		Config:   config,
		Metadata: fromProtoMetadata(req.GetMetadata()),
	}
	return params, nil
}

func FromProtoMessage(pMsg *a2apb.Message) (*a2a.Message, error) {
	if pMsg == nil {
		return nil, nil
	}

	parts, err := fromProtoParts(pMsg.GetParts())
	if err != nil {
		return nil, err
	}

	msg := &a2a.Message{
		ID:         pMsg.GetMessageId(),
		ContextID:  pMsg.GetContextId(),
		Extensions: pMsg.GetExtensions(),
		Parts:      parts,
		TaskID:     a2a.TaskID(pMsg.GetTaskId()),
		Role:       fromProtoRole(pMsg.GetRole()),
		Metadata:   fromProtoMetadata(pMsg.GetMetadata()),
	}

	return msg, nil
}

func fromProtoFilePart(pPart *a2apb.FilePart, meta map[string]any) (a2a.FilePart, error) {
	switch f := pPart.GetFile().(type) {
	case *a2apb.FilePart_FileWithBytes:
		return a2a.FilePart{
			File: a2a.FileBytes{
				FileMeta: a2a.FileMeta{MimeType: pPart.GetMimeType(), Name: pPart.GetName()},
				Bytes:    string(f.FileWithBytes),
			},
			Metadata: meta,
		}, nil
	case *a2apb.FilePart_FileWithUri:
		return a2a.FilePart{
			File: a2a.FileURI{
				FileMeta: a2a.FileMeta{MimeType: pPart.GetMimeType(), Name: pPart.GetName()},
				URI:      f.FileWithUri,
			},
			Metadata: meta,
		}, nil
	default:
		return a2a.FilePart{}, fmt.Errorf("unsupported FilePart type: %T", f)
	}
}

func fromProtoPart(p *a2apb.Part) (a2a.Part, error) {
	meta := fromProtoMetadata(p.Metadata)
	switch part := p.GetPart().(type) {
	case *a2apb.Part_Text:
		return a2a.TextPart{Text: part.Text, Metadata: meta}, nil
	case *a2apb.Part_Data:
		return a2a.DataPart{Data: part.Data.GetData().AsMap(), Metadata: meta}, nil
	case *a2apb.Part_File:
		return fromProtoFilePart(part.File, meta)
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
		return a2a.MessageRoleUnspecified
	}
}

func fromProtoPushConfig(pConf *a2apb.PushNotificationConfig) (*a2a.PushConfig, error) {
	if pConf == nil {
		return nil, nil
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
		return nil, nil
	}

	pConf, err := fromProtoPushConfig(conf.GetPushNotification())
	if err != nil {
		return nil, fmt.Errorf("failed to convert push config: %w", err)
	}

	result := &a2a.MessageSendConfig{
		AcceptedOutputModes: conf.GetAcceptedOutputModes(),
		Blocking:            conf.GetBlocking(),
		PushConfig:          pConf,
	}

	// TODO: consider the approach after resolving https://github.com/a2aproject/A2A/issues/1072
	if conf.HistoryLength > 0 {
		hl := int(conf.HistoryLength)
		result.HistoryLength = &hl
	}
	return result, nil
}

func FromProtoGetTaskRequest(req *a2apb.GetTaskRequest) (*a2a.TaskQueryParams, error) {
	if req == nil {
		return nil, nil
	}

	// TODO: consider throwing an error when the path - req.GetName() is unexpected, e.g. tasks/taskID/someExtraText
	taskID, err := ExtractTaskID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract task id: %w", err)
	}

	params := &a2a.TaskQueryParams{ID: taskID}
	if req.GetHistoryLength() > 0 {
		historyLength := int(req.GetHistoryLength())
		params.HistoryLength = &historyLength
	}
	return params, nil
}

func FromProtoCreateTaskPushConfigRequest(req *a2apb.CreateTaskPushNotificationConfigRequest) (*a2a.TaskPushConfig, error) {
	if req == nil {
		return nil, nil
	}

	config := req.GetConfig()
	if config.GetPushNotificationConfig() == nil {
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

	return &a2a.TaskPushConfig{TaskID: taskID, Config: *pConf}, nil
}

func FromProtoGetTaskPushConfigRequest(req *a2apb.GetTaskPushNotificationConfigRequest) (*a2a.GetTaskPushConfigParams, error) {
	if req == nil {
		return nil, nil
	}

	taskID, err := ExtractTaskID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract task id: %w", err)
	}

	configID, err := ExtractConfigID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract config id: %w", err)
	}

	return &a2a.GetTaskPushConfigParams{TaskID: taskID, ConfigID: configID}, nil
}

func FromProtoDeleteTaskPushConfigRequest(req *a2apb.DeleteTaskPushNotificationConfigRequest) (*a2a.DeleteTaskPushConfigParams, error) {
	if req == nil {
		return nil, nil
	}

	taskID, err := ExtractTaskID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract task id: %w", err)
	}

	configID, err := ExtractConfigID(req.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract config id: %w", err)
	}

	return &a2a.DeleteTaskPushConfigParams{TaskID: taskID, ConfigID: configID}, nil
}

func FromProtoSendMessageResponse(resp *a2apb.SendMessageResponse) (a2a.SendMessageResult, error) {
	if resp == nil {
		return nil, nil
	}

	switch p := resp.Payload.(type) {
	case *a2apb.SendMessageResponse_Msg:
		return FromProtoMessage(p.Msg)
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
	case *a2apb.StreamResponse_Msg:
		return FromProtoMessage(p.Msg)
	case *a2apb.StreamResponse_Task:
		return FromProtoTask(p.Task)
	case *a2apb.StreamResponse_StatusUpdate:
		status, err := fromProtoTaskStatus(p.StatusUpdate.Status)
		if err != nil {
			return nil, err
		}
		return &a2a.TaskStatusUpdateEvent{
			ContextID: p.StatusUpdate.ContextId,
			Final:     p.StatusUpdate.Final,
			Status:    status,
			TaskID:    a2a.TaskID(p.StatusUpdate.TaskId),
			Metadata:  fromProtoMetadata(p.StatusUpdate.Metadata),
		}, nil
	case *a2apb.StreamResponse_ArtifactUpdate:
		artifact, err := fromProtoArtifact(p.ArtifactUpdate.Artifact)
		if err != nil {
			return nil, err
		}
		return &a2a.TaskArtifactUpdateEvent{
			Append:    p.ArtifactUpdate.Append,
			Artifact:  artifact,
			ContextID: p.ArtifactUpdate.ContextId,
			LastChunk: p.ArtifactUpdate.LastChunk,
			TaskID:    a2a.TaskID(p.ArtifactUpdate.TaskId),
			Metadata:  fromProtoMetadata(p.ArtifactUpdate.Metadata),
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

func fromProtoParts(pParts []*a2apb.Part) ([]a2a.Part, error) {
	parts := make([]a2a.Part, len(pParts))
	for i, pPart := range pParts {
		part, err := fromProtoPart(pPart)
		if err != nil {
			return nil, fmt.Errorf("failed to convert part: %w", err)
		}
		parts[i] = part
	}
	return parts, nil
}

func fromProtoTaskState(state a2apb.TaskState) a2a.TaskState {
	switch state {
	case a2apb.TaskState_TASK_STATE_AUTH_REQUIRED:
		return a2a.TaskStateAuthRequired
	case a2apb.TaskState_TASK_STATE_CANCELLED:
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

	message, err := FromProtoMessage(pStatus.Update)
	if err != nil {
		return a2a.TaskStatus{}, fmt.Errorf("failed to convert message for task status: %w", err)
	}

	status := a2a.TaskStatus{
		State:   fromProtoTaskState(pStatus.State),
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

	parts, err := fromProtoParts(pArtifact.Parts)
	if err != nil {
		return nil, fmt.Errorf("failed to convert from proto parts: %w", err)
	}

	return &a2a.Artifact{
		ID:          a2a.ArtifactID(pArtifact.ArtifactId),
		Name:        pArtifact.Name,
		Description: pArtifact.Description,
		Parts:       parts,
		Extensions:  pArtifact.Extensions,
		Metadata:    fromProtoMetadata(pArtifact.Metadata),
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
		ID:        a2a.TaskID(pTask.Id),
		ContextID: pTask.ContextId,
		Status:    status,
		Artifacts: artifacts,
		History:   history,
		Metadata:  fromProtoMetadata(pTask.Metadata),
	}

	return result, nil
}

func FromProtoTaskPushConfig(pTaskConfig *a2apb.TaskPushNotificationConfig) (*a2a.TaskPushConfig, error) {
	if pTaskConfig == nil {
		return nil, nil
	}

	taskID, err := ExtractTaskID(pTaskConfig.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract task id: %w", err)
	}

	configID, err := ExtractConfigID(pTaskConfig.GetName())
	if err != nil {
		return nil, fmt.Errorf("failed to extract config id: %w", err)
	}

	pConf := pTaskConfig.GetPushNotificationConfig()
	if pConf == nil {
		return nil, fmt.Errorf("push notification config is nil")
	}

	if pConf.GetId() != configID {
		return nil, fmt.Errorf("config id mismatch: %q != %q", pConf.GetId(), configID)
	}

	config, err := fromProtoPushConfig(pConf)
	if err != nil {
		return nil, fmt.Errorf("failed to convert push config: %w", err)
	}

	return &a2a.TaskPushConfig{TaskID: taskID, Config: *config}, nil
}

func FromProtoListTaskPushConfig(resp *a2apb.ListTaskPushNotificationConfigResponse) ([]*a2a.TaskPushConfig, error) {
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}

	configs := make([]*a2a.TaskPushConfig, len(resp.Configs))
	for i, pConfig := range resp.Configs {
		config, err := FromProtoTaskPushConfig(pConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to convert config: %w", err)
		}
		configs[i] = config
	}
	return configs, nil
}

func fromProtoAdditionalInterfaces(pInterfaces []*a2apb.AgentInterface) []a2a.AgentInterface {
	interfaces := make([]a2a.AgentInterface, len(pInterfaces))
	for i, pIface := range pInterfaces {
		interfaces[i] = a2a.AgentInterface{
			Transport: a2a.TransportProtocol(pIface.Transport),
			URL:       pIface.Url,
		}
	}
	return interfaces
}

func fromProtoAgentProvider(pProvider *a2apb.AgentProvider) *a2a.AgentProvider {
	if pProvider == nil {
		return nil
	}
	return &a2a.AgentProvider{Org: pProvider.Organization, URL: pProvider.Url}
}

func fromProtoAgentExtensions(pExtensions []*a2apb.AgentExtension) ([]a2a.AgentExtension, error) {
	extensions := make([]a2a.AgentExtension, len(pExtensions))
	for i, pExt := range pExtensions {
		extensions[i] = a2a.AgentExtension{
			URI:         pExt.Uri,
			Description: pExt.Description,
			Required:    pExt.Required,
			Params:      pExt.Params.AsMap(),
		}
	}
	return extensions, nil
}

func fromProtoCapabilities(pCapabilities *a2apb.AgentCapabilities) (a2a.AgentCapabilities, error) {
	extensions, err := fromProtoAgentExtensions(pCapabilities.Extensions)
	if err != nil {
		return a2a.AgentCapabilities{}, fmt.Errorf("failed to convert extensions: %w", err)
	}

	result := a2a.AgentCapabilities{
		PushNotifications: pCapabilities.PushNotifications,
		Streaming:         pCapabilities.Streaming,
		Extensions:        extensions,
	}
	return result, nil
}

func fromProtoOAuthFlows(pFlows *a2apb.OAuthFlows) (a2a.OAuthFlows, error) {
	flows := a2a.OAuthFlows{}
	if pFlows == nil {
		return flows, fmt.Errorf("oauth flows is nil")
	}
	switch f := pFlows.Flow.(type) {
	case *a2apb.OAuthFlows_AuthorizationCode:
		flows.AuthorizationCode = &a2a.AuthorizationCodeOAuthFlow{
			AuthorizationURL: f.AuthorizationCode.AuthorizationUrl,
			TokenURL:         f.AuthorizationCode.TokenUrl,
			RefreshURL:       f.AuthorizationCode.RefreshUrl,
			Scopes:           f.AuthorizationCode.Scopes,
		}
	case *a2apb.OAuthFlows_ClientCredentials:
		flows.ClientCredentials = &a2a.ClientCredentialsOAuthFlow{
			TokenURL:   f.ClientCredentials.TokenUrl,
			RefreshURL: f.ClientCredentials.RefreshUrl,
			Scopes:     f.ClientCredentials.Scopes,
		}
	case *a2apb.OAuthFlows_Implicit:
		flows.Implicit = &a2a.ImplicitOAuthFlow{
			AuthorizationURL: f.Implicit.AuthorizationUrl,
			RefreshURL:       f.Implicit.RefreshUrl,
			Scopes:           f.Implicit.Scopes,
		}
	case *a2apb.OAuthFlows_Password:
		flows.Password = &a2a.PasswordOAuthFlow{
			TokenURL:   f.Password.TokenUrl,
			RefreshURL: f.Password.RefreshUrl,
			Scopes:     f.Password.Scopes,
		}
	default:
		return flows, fmt.Errorf("unsupported oauth flow type: %T", f)
	}
	return flows, nil
}

func fromProtoSecurityScheme(pScheme *a2apb.SecurityScheme) (a2a.SecurityScheme, error) {
	if pScheme == nil {
		return nil, fmt.Errorf("security scheme is nil")
	}

	switch s := pScheme.Scheme.(type) {
	case *a2apb.SecurityScheme_ApiKeySecurityScheme:
		return a2a.APIKeySecurityScheme{
			Name:        s.ApiKeySecurityScheme.Name,
			In:          a2a.APIKeySecuritySchemeIn(s.ApiKeySecurityScheme.Location),
			Description: s.ApiKeySecurityScheme.Description,
		}, nil
	case *a2apb.SecurityScheme_HttpAuthSecurityScheme:
		return a2a.HTTPAuthSecurityScheme{
			Scheme:       s.HttpAuthSecurityScheme.Scheme,
			Description:  s.HttpAuthSecurityScheme.Description,
			BearerFormat: s.HttpAuthSecurityScheme.BearerFormat,
		}, nil
	case *a2apb.SecurityScheme_OpenIdConnectSecurityScheme:
		return a2a.OpenIDConnectSecurityScheme{
			OpenIDConnectURL: s.OpenIdConnectSecurityScheme.OpenIdConnectUrl,
			Description:      s.OpenIdConnectSecurityScheme.Description,
		}, nil
	case *a2apb.SecurityScheme_Oauth2SecurityScheme:
		flows, err := fromProtoOAuthFlows(s.Oauth2SecurityScheme.Flows)
		if err != nil {
			return nil, fmt.Errorf("failed to convert OAuthFlows: %w", err)
		}
		return a2a.OAuth2SecurityScheme{
			Flows:       flows,
			Description: s.Oauth2SecurityScheme.Description,
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

func fromProtoSecurity(pSecurity []*a2apb.Security) []a2a.SecurityRequirements {
	security := make([]a2a.SecurityRequirements, len(pSecurity))
	for i, pSec := range pSecurity {
		schemes := make(a2a.SecurityRequirements)
		for name, scopes := range pSec.Schemes {
			schemes[a2a.SecuritySchemeName(name)] = scopes.List
		}
		security[i] = schemes
	}
	return security
}

func fromProtoSkills(pSkills []*a2apb.AgentSkill) []a2a.AgentSkill {
	skills := make([]a2a.AgentSkill, len(pSkills))
	for i, pSkill := range pSkills {
		skills[i] = a2a.AgentSkill{
			ID:          pSkill.Id,
			Name:        pSkill.Name,
			Description: pSkill.Description,
			Tags:        pSkill.Tags,
			Examples:    pSkill.Examples,
			InputModes:  pSkill.InputModes,
			OutputModes: pSkill.OutputModes,
		}
	}
	return skills
}

func FromProtoAgentCard(pCard *a2apb.AgentCard) (*a2a.AgentCard, error) {
	if pCard == nil {
		return nil, nil
	}

	capabilities, err := fromProtoCapabilities(pCard.Capabilities)
	if err != nil {
		return nil, fmt.Errorf("failed to convert agent capabilities: %w", err)
	}

	schemes, err := fromProtoSecuritySchemes(pCard.SecuritySchemes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert security schemes: %w", err)
	}

	result := &a2a.AgentCard{
		ProtocolVersion:                   pCard.ProtocolVersion,
		Name:                              pCard.Name,
		Description:                       pCard.Description,
		URL:                               pCard.Url,
		PreferredTransport:                a2a.TransportProtocol(pCard.PreferredTransport),
		Version:                           pCard.Version,
		DocumentationURL:                  pCard.DocumentationUrl,
		Capabilities:                      capabilities,
		DefaultInputModes:                 pCard.DefaultInputModes,
		DefaultOutputModes:                pCard.DefaultOutputModes,
		SupportsAuthenticatedExtendedCard: pCard.SupportsAuthenticatedExtendedCard,
		SecuritySchemes:                   schemes,
		Provider:                          fromProtoAgentProvider(pCard.Provider),
		AdditionalInterfaces:              fromProtoAdditionalInterfaces(pCard.AdditionalInterfaces),
		Security:                          fromProtoSecurity(pCard.Security),
		Skills:                            fromProtoSkills(pCard.Skills),
	}

	return result, nil
}
