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

package a2asrv

// User can be attached to [CallContext] by authentication middleware.
type User struct {
	// Name is a username.
	Name string
	// Authenticated is true if the request was authenticated.
	Authenticated bool
	// Attributes is a map of attributes associated with the user.
	Attributes map[string]any
}

// NewAuthenticatedUser returns a new [User] instance with the specified username and attributes.
func NewAuthenticatedUser(username string, attrs map[string]any) *User {
	return &User{
		Name:          username,
		Attributes:    attrs,
		Authenticated: true,
	}
}
