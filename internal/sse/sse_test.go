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

package sse

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestSSE_Success(t *testing.T) {
	wantEvents := 10
	makeEvent := func(i int) string { return "hello " + strconv.Itoa(i) }
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		sse, err := NewWriter(rw)
		if err != nil {
			t.Fatalf("NewWriter() error = %v", err)
		}
		sse.WriteHeaders()
		for i := range wantEvents {
			if err := sse.WriteData(ctx, []byte(makeEvent(i))); err != nil {
				t.Fatalf("WriteKeepAlive() error = %v", err)
			}
			if i%3 == 0 {
				if err := sse.WriteKeepAlive(ctx); err != nil {
					t.Fatalf("WriteKeepAlive() error = %v", err)
				}
			}
		}
	}))

	ctx := t.Context()
	req, err := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	req.Header.Set("Accept", ContentEventStream)
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	eventCount := 0
	for data, err := range ParseDataStream(resp.Body) {
		if err != nil {
			t.Fatalf("ParseDataStream() error = %v", err)
		}
		want := makeEvent(eventCount)
		if string(data) != want {
			t.Fatalf("ParseDataStream() = %q at %d, want %q", string(data), eventCount, want)
		}
		eventCount++
	}
	if eventCount != wantEvents {
		t.Fatalf("ParseDataStream() emitted %d events, want %d", eventCount, wantEvents)
	}

}

func TestSSE_LargePayload(t *testing.T) {
	// Create a payload larger than the default 64KB bufio.Scanner buffer
	// to verify that the increased buffer size works correctly.
	payloadSize := 100 * 1024 // 100KB
	largePayload := strings.Repeat("x", payloadSize)

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		sse, err := NewWriter(rw)
		if err != nil {
			t.Fatalf("NewWriter() error = %v", err)
		}
		sse.WriteHeaders()
		if err := sse.WriteData(ctx, []byte(largePayload)); err != nil {
			t.Fatalf("WriteData() error = %v", err)
		}
	}))
	defer server.Close()

	ctx := t.Context()
	req, err := http.NewRequestWithContext(ctx, "POST", server.URL, nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error = %v", err)
	}
	req.Header.Set("Accept", ContentEventStream)
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	eventCount := 0
	for data, err := range ParseDataStream(resp.Body) {
		if err != nil {
			t.Fatalf("ParseDataStream() error = %v", err)
		}
		if len(data) != payloadSize {
			t.Fatalf("ParseDataStream() payload size = %d, want %d", len(data), payloadSize)
		}
		if string(data) != largePayload {
			t.Fatalf("ParseDataStream() payload content mismatch")
		}
		eventCount++
	}
	if eventCount != 1 {
		t.Fatalf("ParseDataStream() emitted %d events, want 1", eventCount)
	}
}
