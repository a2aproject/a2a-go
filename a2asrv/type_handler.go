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

import (
	"fmt"
	"net/url"
	"strconv"
	"time"
)

func parseIntOpt(query url.Values, key string) (*int, error) {
	val := query.Get(key)
	if val == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	return &v, nil
}

func parseTimeOpt(query url.Values, key string) (*time.Time, error) {
	val := query.Get(key)
	if val == "" {
		return nil, nil
	}
	parsedTime, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	return &parsedTime, nil
}

func parseInt(query url.Values, key string) (int, error) {
	val := query.Get(key)
	if val == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}

func parseBool(query url.Values, key string) (bool, error) {
	val := query.Get(key)
	if val == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(val)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}
