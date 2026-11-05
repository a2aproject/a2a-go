// Copyright 2026 The A2A Authors
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

package oauth

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestRegistrationValidate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		reg     Registration
		wantErr bool
	}{
		{name: "valid", reg: Registration{Authority: "https://login.example.com", ClientID: "c"}},
		{name: "loopback http", reg: Registration{Authority: "http://127.0.0.1:8080", ClientID: "c"}},
		{name: "no client ID", reg: Registration{Authority: "https://login.example.com"}, wantErr: true},
		{name: "no authority", reg: Registration{ClientID: "c"}, wantErr: true},
		{name: "plain http", reg: Registration{Authority: "http://login.example.com", ClientID: "c"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := tc.reg.Validate(); (err != nil) != tc.wantErr {
				t.Fatalf("Registration.Validate() error = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestSelectClient(t *testing.T) {
	t.Parallel()
	regs := []Registration{
		{Authority: "https://login.example.com/", ClientID: "configured"},
		{Authority: "https://dup.example.com", ClientID: "one"},
		{Authority: "https://dup.example.com", ClientID: "two"},
	}
	tests := []struct {
		name          string
		regs          []Registration
		endpoints     Endpoints
		defaultID     string
		want          ClientChoice
		wantErr       bool
		wantNoClient  bool
		wantAmbiguous *AmbiguousClientError
	}{
		{
			name:      "configured registration, trailing slash ignored",
			endpoints: Endpoints{Issuer: "https://login.example.com", CIMDSupported: true},
			defaultID: testCIMDClientID,
			want:      ClientChoice{Registration: regs[0], Source: ClientSourceConfig},
		},
		{
			name:      "CIMD fallback",
			endpoints: Endpoints{Issuer: "https://other.example.com", CIMDSupported: true},
			defaultID: testCIMDClientID,
			want:      ClientChoice{Registration: Registration{Authority: "https://other.example.com", ClientID: testCIMDClientID}, Source: ClientSourceCIMD},
		},
		{
			name:         "no client",
			endpoints:    Endpoints{Issuer: "https://other.example.com"},
			defaultID:    testCIMDClientID,
			wantErr:      true,
			wantNoClient: true,
		},
		{
			name:         "CIMD fallback disabled",
			endpoints:    Endpoints{Issuer: "https://other.example.com", CIMDSupported: true},
			wantErr:      true,
			wantNoClient: true,
		},
		{
			name:          "ambiguous",
			endpoints:     Endpoints{Issuer: "https://dup.example.com"},
			wantErr:       true,
			wantAmbiguous: &AmbiguousClientError{Issuer: "https://dup.example.com", ClientIDs: []string{"one", "two"}},
		},
		{
			name:      "registration without a client ID",
			regs:      []Registration{{Authority: "https://login.example.com"}},
			endpoints: Endpoints{Issuer: "https://login.example.com"},
			wantErr:   true,
		},
		{
			name:      "registration with a plain http authority",
			regs:      []Registration{{Authority: "http://login.example.com", ClientID: "c"}},
			endpoints: Endpoints{Issuer: "http://login.example.com"},
			wantErr:   true,
		},
		{
			name:      "default client ID is not a URL",
			endpoints: Endpoints{Issuer: "https://other.example.com", CIMDSupported: true},
			defaultID: "a2a-client",
			wantErr:   true,
		},
		{
			name:      "default client ID is a plain http URL",
			endpoints: Endpoints{Issuer: "https://other.example.com", CIMDSupported: true},
			defaultID: "http://client.example.com/metadata.json",
			wantErr:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.regs == nil {
				tc.regs = regs
			}
			got, err := selectClient(tc.regs, tc.defaultID, tc.endpoints)
			if (err != nil) != tc.wantErr {
				t.Fatalf("selectClient() error = %v, want error %v", err, tc.wantErr)
			}
			var nc *NoClientError
			if errors.As(err, &nc) != tc.wantNoClient {
				t.Fatalf("selectClient() error = %v, want NoClientError %v", err, tc.wantNoClient)
			}
			var ae *AmbiguousClientError
			if errors.As(err, &ae) != (tc.wantAmbiguous != nil) {
				t.Fatalf("selectClient() error = %v, want AmbiguousClientError %v", err, tc.wantAmbiguous != nil)
			}
			if diff := cmp.Diff(tc.wantAmbiguous, ae); diff != "" {
				t.Fatalf("selectClient() AmbiguousClientError wrong result (-want +got) diff = %s", diff)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Fatalf("selectClient() wrong result (-want +got) diff = %s", diff)
			}
		})
	}
}

const testCIMDClientID = "https://client.example.com/metadata.json"
