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

package pushconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/a2aproject/a2a-go/a2a"
)

// HTTPPushSender sends A2A events to a push notification endpoint over HTTP.
type HTTPPushSender struct {
	client *http.Client
}

// NewHTTPPushSender creates a new HTTPPushSender. If no client is provided,
// it uses a default client with a 30-second timeout.
func NewHTTPPushSender(client *http.Client) *HTTPPushSender {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPPushSender{client: client}
}

// Send serializes the event to JSON and sends it as an HTTP POST request
// to the URL specified in the push configuration.
func (s *HTTPPushSender) Send(ctx context.Context, config *a2a.PushConfig, event a2a.Event) error {
	jsonData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to serialize event to JSON: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, config.URL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if config.Token != "" {
		req.Header.Set("X-A2A-Notification-Token", config.Token)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send push notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push notification endpoint returned non-success status: %s", resp.Status)
	}

	return nil
}
