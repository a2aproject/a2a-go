// Copyright 20\d\d The A2A Authors
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

package main

import (
	// "bytes"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/a2aproject/a2a-go/a2a"
)

var restURL = flag.String("url", "http://127.0.0.1:9001", "Base URL of REST server.")

func main() {
	flag.Parse()
	client := &http.Client{}

	// Test SendMessage endpoint
	testSendMessage(client)

	testGetTask(client)

}

func testGetTask(client *http.Client) {
	taskID := "test-task-123"
	url := fmt.Sprintf("%s/v1/tasks:get?id=%s", *restURL, taskID)

	log.Printf("Testing GET task: %s", url)

	resp, err := client.Get(url)
	if err != nil {
		log.Fatalf("Failed to get task: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}

	log.Printf("Response status: %d", resp.StatusCode)
	log.Printf("Response body: %s", string(body))

	if resp.StatusCode == http.StatusOK {
		var task a2a.Task
		if err := json.Unmarshal(body, &task); err != nil {
			log.Fatalf("Failed to parse task response: %v", err)
		}
		log.Printf("Task ID: %s", task.ID)
		log.Printf("Task Status: %s", task.Status)
	}
}

func testSendMessage(client *http.Client) {
	url := fmt.Sprintf("%s/v1/message:send", *restURL)

	log.Printf("Testing SendMessage: %s", url)

	message := a2a.NewMessage(a2a.MessageRoleUser, a2a.TextPart{Text: "Hello from REST client"})
	params := &a2a.MessageSendParams{
		Message: message,
	}

	jsonBody, err := json.Marshal(params)
	if err != nil {
		log.Fatalf("Failed to marshal message: %v", err)
	}

	resp, err := client.Post(url, "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Failed to read response body: %v", err)
	}

	log.Printf("Response status: %d", resp.StatusCode)
	log.Printf("Response body: %s", string(body))
}
