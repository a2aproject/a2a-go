package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/a2aproject/a2a-go/internal/jsonrpc"
	"github.com/a2aproject/a2a-go/protocol/jsonprotocol"
)

// TestA2AClient_GetTasks tests the GetTasks client method covering success and error scenarios.
func TestA2AClient_GetTasks(t *testing.T) {
	taskID := "client-get-task-1"
	params := jsonprotocol.TaskQueryParams{
		RPCID: taskID,
		ID:    taskID,
	}
	paramsBytes, err := json.Marshal(params)
	require.NoError(t, err)

	expectedRequest := &jsonrpc.Request{
		Message: jsonrpc.Message{JSONRPC: "2.0", ID: taskID},
		Method:  "tasks/get",
		Params:  paramsBytes,
	}

	t.Run("GetTasks Success", func(t *testing.T) {
		// Prepare mock server response
		respTask := jsonprotocol.Task{
			ID:     taskID,
			Status: jsonprotocol.TaskStatus{State: jsonprotocol.TaskStateCompleted},
			Artifacts: []jsonprotocol.Artifact{
				{
					Name:  stringPtr("test-artifact"),
					Parts: []jsonprotocol.Part{jsonprotocol.NewTextPart("Test result")},
				},
			},
		}
		respResultBytes, err := json.Marshal(respTask)
		require.NoError(t, err)
		respBody := fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","result":%s}`, taskID, string(respResultBytes))

		mockHandler := createMockServerHandler(
			t,
			"tasks/get",
			expectedRequest,
			respBody,
			http.StatusOK,
			nil,
		)
		server := httptest.NewServer(mockHandler)
		defer server.Close()

		client, err := NewA2AClient(server.URL)
		require.NoError(t, err)

		// Call the client method
		result, err := client.GetTasks(context.Background(), params)

		// Assertions
		require.NoError(t, err, "GetTasks should not return an error on success")
		require.NotNil(t, result, "Result should not be nil on success")

		assert.Equal(t, taskID, result.ID)
		assert.Equal(t, jsonprotocol.TaskStateCompleted, result.Status.State)
		assert.Len(t, result.Artifacts, 1)
		assert.Equal(t, "test-artifact", *result.Artifacts[0].Name)
	})

	t.Run("GetTasks JSON-RPC Error", func(t *testing.T) {
		// Prepare mock server error response
		errorResp := fmt.Sprintf(`{
			"jsonrpc":"2.0",
			"id":"%s",
			"error":{
				"code":-32001,
				"message":"Task not found",
				"data":"The requested task ID does not exist"
			}
		}`, taskID)

		mockHandler := createMockServerHandler(
			t,
			"tasks/get",
			expectedRequest,
			errorResp,
			http.StatusOK,
			nil,
		)
		server := httptest.NewServer(mockHandler)
		defer server.Close()

		client, err := NewA2AClient(server.URL)
		require.NoError(t, err)

		// Call the client method
		result, err := client.GetTasks(context.Background(), params)

		// Assertions
		require.Error(t, err, "GetTasks should return an error on JSON-RPC error")
		assert.Nil(t, result, "Result should be nil on error")
		assert.Contains(t, err.Error(), "Task not found")
	})
}

// TestA2AClient_CancelTasks tests the CancelTasks client method.
func TestA2AClient_CancelTasks(t *testing.T) {
	taskID := "client-cancel-task-1"
	params := jsonprotocol.TaskIDParams{
		RPCID: taskID,
		ID:    taskID,
	}
	paramsBytes, err := json.Marshal(params)
	require.NoError(t, err)

	expectedRequest := &jsonrpc.Request{
		Message: jsonrpc.Message{JSONRPC: "2.0", ID: taskID},
		Method:  "tasks/cancel",
		Params:  paramsBytes,
	}

	t.Run("CancelTasks Success", func(t *testing.T) {
		// Prepare mock server response
		respTask := jsonprotocol.Task{
			ID:     taskID,
			Status: jsonprotocol.TaskStatus{State: jsonprotocol.TaskStateCanceled},
		}
		respResultBytes, err := json.Marshal(respTask)
		require.NoError(t, err)
		respBody := fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","result":%s}`, taskID, string(respResultBytes))

		mockHandler := createMockServerHandler(
			t,
			"tasks/cancel",
			expectedRequest,
			respBody,
			http.StatusOK,
			nil,
		)
		server := httptest.NewServer(mockHandler)
		defer server.Close()

		client, err := NewA2AClient(server.URL)
		require.NoError(t, err)

		// Call the client method
		result, err := client.CancelTasks(context.Background(), params)

		// Assertions
		require.NoError(t, err, "CancelTasks should not return an error on success")
		require.NotNil(t, result, "Result should not be nil on success")

		assert.Equal(t, taskID, result.ID)
		assert.Equal(t, jsonprotocol.TaskStateCanceled, result.Status.State)
	})

	t.Run("CancelTasks Non-Existent Task", func(t *testing.T) {
		// Prepare mock server error response
		errorResp := fmt.Sprintf(`{
			"jsonrpc":"2.0",
			"id":"%s",
			"error":{
				"code":-32001,
				"message":"Task not found",
				"data":"Cannot cancel non-existent task"
			}
		}`, taskID)

		mockHandler := createMockServerHandler(
			t,
			"tasks/cancel",
			expectedRequest,
			errorResp,
			http.StatusOK,
			nil,
		)
		server := httptest.NewServer(mockHandler)
		defer server.Close()

		client, err := NewA2AClient(server.URL)
		require.NoError(t, err)

		// Call the client method
		result, err := client.CancelTasks(context.Background(), params)

		// Assertions
		require.Error(t, err, "CancelTasks should return an error for non-existent task")
		assert.Nil(t, result, "Result should be nil on error")
		assert.Contains(t, err.Error(), "Task not found")
	})
}

// TestA2AClient_SetPushNotification tests the SetPushNotification client method.
func TestA2AClient_SetPushNotification(t *testing.T) {
	taskID := "client-push-task-1"
	params := jsonprotocol.TaskPushNotificationConfig{
		RPCID:  taskID,
		TaskID: taskID,
		PushNotificationConfig: jsonprotocol.PushNotificationConfig{
			URL: "https://example.com/webhook",
			Authentication: &jsonprotocol.PushNotificationAuthenticationInfo{
				Schemes: []string{"bearer"},
			},
		},
	}
	paramsBytes, err := json.Marshal(params)
	require.NoError(t, err)

	expectedRequest := &jsonrpc.Request{
		Message: jsonrpc.Message{JSONRPC: "2.0", ID: taskID},
		Method:  "tasks/pushNotificationConfig/set",
		Params:  paramsBytes,
	}

	t.Run("SetPushNotification Success", func(t *testing.T) {
		// Prepare mock server response
		respConfig := jsonprotocol.TaskPushNotificationConfig{
			TaskID: taskID,
			PushNotificationConfig: jsonprotocol.PushNotificationConfig{
				URL: "https://example.com/webhook",
			},
		}
		respResultBytes, err := json.Marshal(respConfig)
		require.NoError(t, err)
		respBody := fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","result":%s}`, taskID, string(respResultBytes))

		mockHandler := createMockServerHandler(
			t,
			"tasks/pushNotificationConfig/set",
			expectedRequest,
			respBody,
			http.StatusOK,
			nil,
		)
		server := httptest.NewServer(mockHandler)
		defer server.Close()

		client, err := NewA2AClient(server.URL)
		require.NoError(t, err)

		// Call the client method
		result, err := client.SetPushNotification(context.Background(), params)

		// Assertions
		require.NoError(t, err, "SetPushNotification should not return an error on success")
		require.NotNil(t, result, "Result should not be nil on success")

		assert.Equal(t, taskID, result.TaskID)
		assert.Equal(t, "https://example.com/webhook", result.PushNotificationConfig.URL)
	})

	t.Run("SetPushNotification Invalid URL", func(t *testing.T) {
		// Prepare mock server error response
		errorResp := fmt.Sprintf(`{
			"jsonrpc":"2.0",
			"id":"%s",
			"error":{
				"code":-32602,
				"message":"Invalid params",
				"data":"Invalid webhook URL"
			}
		}`, taskID)

		mockHandler := createMockServerHandler(
			t,
			"tasks/pushNotificationConfig/set",
			expectedRequest,
			errorResp,
			http.StatusOK,
			nil,
		)
		server := httptest.NewServer(mockHandler)
		defer server.Close()

		client, err := NewA2AClient(server.URL)
		require.NoError(t, err)

		// Call the client method
		result, err := client.SetPushNotification(context.Background(), params)

		// Assertions
		require.Error(t, err, "SetPushNotification should return an error for invalid URL")
		assert.Nil(t, result, "Result should be nil on error")
		assert.Contains(t, err.Error(), "Invalid params")
	})
}

// TestA2AClient_GetPushNotification tests the GetPushNotification client method.
func TestA2AClient_GetPushNotification(t *testing.T) {
	taskID := "client-push-get-1"
	params := jsonprotocol.TaskIDParams{
		RPCID: taskID,
		ID:    taskID,
	}
	paramsBytes, err := json.Marshal(params)
	require.NoError(t, err)

	expectedRequest := &jsonrpc.Request{
		Message: jsonrpc.Message{JSONRPC: "2.0", ID: taskID},
		Method:  "tasks/pushNotificationConfig/get",
		Params:  paramsBytes,
	}

	t.Run("GetPushNotification Success", func(t *testing.T) {
		respConfig := jsonprotocol.TaskPushNotificationConfig{
			TaskID: taskID,
			PushNotificationConfig: jsonprotocol.PushNotificationConfig{
				URL: "https://example.com/webhook",
				Authentication: &jsonprotocol.PushNotificationAuthenticationInfo{
					Schemes: []string{"bearer"},
				},
			},
		}
		respResultBytes, err := json.Marshal(respConfig)
		require.NoError(t, err)
		respBody := fmt.Sprintf(`{"jsonrpc":"2.0","id":"%s","result":%s}`, taskID, string(respResultBytes))

		mockHandler := createMockServerHandler(
			t,
			"tasks/pushNotificationConfig/get",
			expectedRequest,
			respBody,
			http.StatusOK,
			nil,
		)
		server := httptest.NewServer(mockHandler)
		defer server.Close()

		client, err := NewA2AClient(server.URL)
		require.NoError(t, err)

		// Call the client method
		result, err := client.GetPushNotification(context.Background(), params)

		// Assertions
		require.NoError(t, err, "GetPushNotification should not return an error on success")
		require.NotNil(t, result, "Result should not be nil on success")

		assert.Equal(t, taskID, result.TaskID)
		assert.Equal(t, "https://example.com/webhook", result.PushNotificationConfig.URL)
		require.NotNil(t, result.PushNotificationConfig.Authentication)
		assert.Contains(t, result.PushNotificationConfig.Authentication.Schemes, "bearer")
	})

	t.Run("GetPushNotification Not Found", func(t *testing.T) {
		// Prepare mock server error response
		errorResp := fmt.Sprintf(`{
			"jsonrpc":"2.0",
			"id":"%s",
			"error":{
				"code":-32001,
				"message":"Not found",
				"data":"No push notification configuration found for task"
			}
		}`, taskID)

		mockHandler := createMockServerHandler(
			t,
			"tasks/pushNotificationConfig/get",
			expectedRequest,
			errorResp,
			http.StatusOK,
			nil,
		)
		server := httptest.NewServer(mockHandler)
		defer server.Close()

		client, err := NewA2AClient(server.URL)
		require.NoError(t, err)

		// Call the client method
		result, err := client.GetPushNotification(context.Background(), params)

		// Assertions
		require.Error(t, err, "GetPushNotification should return an error for not found")
		assert.Nil(t, result, "Result should be nil on error")
		assert.Contains(t, err.Error(), "Not found")
	})
}

// Helper function to get string pointer for tests
func stringPtr(s string) *string {
	return &s
}

// createMockServerHandler provides a configurable mock HTTP handler for testing
// client interactions. It verifies the incoming request method, headers, and
// body (if expectedReqBody is provided) before sending a configured response.
func createMockServerHandler(
	t *testing.T,
	expectedMethod string,
	expectedReqBody *jsonrpc.Request,
	responseBody string,
	statusCode int,
	responseHeaders map[string]string,
) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		// Check method and content type header.
		assert.Equal(t, http.MethodPost, r.Method, "MockHandler: Expected POST method")
		assert.Equal(t, "application/json; charset=utf-8",
			r.Header.Get("Content-Type"), "MockHandler: Content-Type header mismatch")

		// Read request body.
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err, "MockHandler: Failed to read request body")

		// Verify request body if expectedReqBody is provided.
		if expectedReqBody != nil {
			var receivedReq jsonrpc.Request
			err = json.Unmarshal(bodyBytes, &receivedReq)
			require.NoError(t, err, "MockHandler: Failed to unmarshal request body. Body: %s", string(bodyBytes))

			assert.Equal(t, expectedReqBody.JSONRPC, receivedReq.JSONRPC,
				"MockHandler: Request JSONRPC version mismatch")
			assert.Equal(t, expectedReqBody.ID, receivedReq.ID, "MockHandler: Request ID mismatch")
			assert.Equal(t, expectedMethod, receivedReq.Method, "MockHandler: Request method mismatch")
			assert.JSONEq(t, string(expectedReqBody.Params), string(receivedReq.Params),
				"MockHandler: Request params mismatch")
		}

		// Set response headers.
		if responseHeaders != nil {
			for k, v := range responseHeaders {
				w.Header().Set(k, v)
			}
		} else {
			// Default to JSON response header if none provided.
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}

		// Write status code and response body.
		w.WriteHeader(statusCode)
		_, err = w.Write([]byte(responseBody))
		assert.NoError(t, err, "MockHandler: Failed to write response body")
	}
}
