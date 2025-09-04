package a2agrpc

import (
	"fmt"

	"github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2apb"
	"google.golang.org/protobuf/types/known/structpb"
)

func fromProtoSendMessageRequest(req *a2apb.SendMessageRequest) (a2a.MessageSendParams, error) {
	msg, err := fromProtoMessage(req.GetRequest())
	if err != nil {
		return a2a.MessageSendParams{}, err
	}
	params := a2a.MessageSendParams{
		Message: msg,
	}
	if req.GetConfiguration() != nil {
		params.Config = fromProtoSendMessageConfig(req.GetConfiguration())
	}
	if req.GetMetadata() != nil {
		params.Metadata = req.GetMetadata().AsMap()
	}
	return params, nil
}

func fromProtoMessage(pMsg *a2apb.Message) (a2a.Message, error) {
	if pMsg == nil {
		return a2a.Message{}, fmt.Errorf("proto message is nil")
	}
	parts := make([]a2a.Part, len(pMsg.GetContent()))
	for i, p := range pMsg.GetContent() {
		part, err := fromProtoPart(p)
		if err != nil {
			return a2a.Message{}, fmt.Errorf("failed to convert part: %w", err)
		}
		parts[i] = part
	}
	msg := a2a.Message{
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
			MimeType: pPart.GetMimeType(),
			File:     a2a.FileBytes{Bytes: string(f.FileWithBytes)},
		}, nil
	case *a2apb.FilePart_FileWithUri:
		return a2a.FilePart{
			MimeType: pPart.GetMimeType(),
			File:     a2a.FileURI{URI: f.FileWithUri},
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

func fromProtoSendMessageConfig(conf *a2apb.SendMessageConfiguration) *a2a.MessageSendConfig {
	if conf == nil {
		return nil
	}
	result := &a2a.MessageSendConfig{
		AcceptedOutputModes: conf.GetAcceptedOutputModes(),
		Blocking:            conf.GetBlocking(),
	}
	if conf.HistoryLength > 0 {
		hl := int(conf.HistoryLength)
		result.HistoryLength = &hl
	}

	if conf.GetPushNotification() != nil {
		pConf := conf.GetPushNotification()
		result.PushConfig = &a2a.PushConfig{
			ID:  pConf.GetId(),
			URL: pConf.GetUrl(),
			Auth: &a2a.PushAuthInfo{
				Schemes:     pConf.GetAuthentication().GetSchemes(),
				Credentials: pConf.GetAuthentication().GetCredentials(),
			},
			Token: pConf.GetToken(),
		}
	}

	return result
}

func toProtoSendMessageResponse(result a2a.SendMessageResult) (*a2apb.SendMessageResponse, error) {
	resp := &a2apb.SendMessageResponse{}
	switch r := result.(type) {
	case *a2a.Message:
		pMsg, err := toProtoMessage(r)
		if err != nil {
			return nil, err
		}
		resp.Payload = &a2apb.SendMessageResponse_Msg{Msg: pMsg}
	case *a2a.Task:
		pTask, err := toProtoTask(r)
		if err != nil {
			return nil, err
		}
		resp.Payload = &a2apb.SendMessageResponse_Task{Task: pTask}
	default:
		return nil, fmt.Errorf("unsupported SendMessageResult type: %T", result)
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

func toProtoFilePart(part a2a.FilePart) (*a2apb.Part, error) {
	switch fc := part.File.(type) {
	case a2a.FileBytes:
		return &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
			MimeType: part.MimeType,
			File:     &a2apb.FilePart_FileWithBytes{FileWithBytes: []byte(fc.Bytes)},
		}}}, nil
	case a2a.FileURI:
		return &a2apb.Part{Part: &a2apb.Part_File{File: &a2apb.FilePart{
			MimeType: part.MimeType,
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

func toProtoTask(task *a2a.Task) (*a2apb.Task, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	// This is a partial conversion for demonstration. A full implementation
	// would convert all fields, including Status, Artifacts, History, etc.
	return &a2apb.Task{
		Id:        string(task.ID),
		ContextId: task.ContextID,
	}, nil
}
