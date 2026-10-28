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

// Package utils provides general utility functions for A2A.
package utils

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/a2aproject/a2a-go/v2/log"
)

func init() {
	gob.Register(map[string]any{})
	gob.Register([]any{})
}

// DeepCopy uses gob encode-decode pass to create a deep-copy of the referenced data.
// 保留显式空切片与 nil 的区别，避免元数据数组被复制为 null。
func DeepCopy[T any](task *T) (*T, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	dec := gob.NewDecoder(&buf)

	if err := enc.Encode(*task); err != nil {
		return nil, err
	}

	var copy T
	if err := dec.Decode(&copy); err != nil {
		return nil, err
	}

	// gob decodes non-nil empty slices as nil; restore their shape without changing nil values.
	restoreEmptySlices(reflect.ValueOf(task).Elem(), reflect.ValueOf(&copy).Elem())
	return &copy, nil
}

// ToStringMap converts a map[string]any to a map[string]string.
// It logs a warning if a value cannot be converted to a string.
func ToStringMap(raw any) (map[string]string, bool) {
	if m, ok := raw.(map[string]string); ok {
		return m, true
	} else if m, ok := raw.(map[string]any); ok {
		converted := make(map[string]string)
		for k, v := range m {
			if s, ok := v.(string); ok {
				converted[k] = s
			} else {
				log.Warn(context.Background(), "failed to convert metadata value to string", "key", k, "value", v)
			}
		}
		return converted, true
	}
	return nil, false
}

// ToMapStruct converts an any struct to map[string]any using json roundtrip.
func ToMapStruct(value any) (map[string]any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var r map[string]any
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	return r, nil
}

// FromMapStruct converts map[string]any to a typed value using json roundtrip.
func FromMapStruct[T any](value map[string]any) (*T, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var zero T
	if err := json.Unmarshal(raw, &zero); err != nil {
		return nil, err
	}
	return &zero, nil
}

// MustPrettyPrint is a debugging utility that prints indented JSON represenation of a value.
// 仅供诊断展示结构化值，编码失败时保留显式失败，避免输出误导性内容。
func MustPrettyPrint(value any) {
	raw, err := json.MarshalIndent(value, "", " ")
	if err != nil {
		panic(err.Error())
	}
	fmt.Println(string(raw))
}

// restoreEmptySlices 仅恢复 gob 丢失的空切片形状，保留原 nil 和 gob 的类型及深复制语义。
func restoreEmptySlices(source, target reflect.Value) {
	if !source.IsValid() || !target.IsValid() || source.Type() != target.Type() {
		return
	}
	// 自定义 gob 编解码器拥有该类型的表示，不覆盖其有意执行的转换。
	encoder := reflect.TypeFor[gob.GobEncoder]()
	if source.Type().Implements(encoder) || (source.Kind() != reflect.Pointer && reflect.PointerTo(source.Type()).Implements(encoder)) {
		return
	}
	switch source.Kind() {
	case reflect.Interface:
		if source.IsNil() || target.IsNil() {
			return
		}
		value := reflect.New(target.Elem().Type()).Elem()
		value.Set(target.Elem())
		restoreEmptySlices(source.Elem(), value)
		if target.CanSet() {
			target.Set(value)
		}
	case reflect.Pointer:
		if !source.IsNil() && !target.IsNil() {
			restoreEmptySlices(source.Elem(), target.Elem())
		}
	case reflect.Struct:
		for i := 0; i < source.NumField(); i++ {
			if target.Field(i).CanSet() {
				restoreEmptySlices(source.Field(i), target.Field(i))
			}
		}
	case reflect.Map:
		if source.IsNil() || target.IsNil() {
			return
		}
		for iter := source.MapRange(); iter.Next(); {
			previous := target.MapIndex(iter.Key())
			if !previous.IsValid() {
				continue
			}
			value := reflect.New(previous.Type()).Elem()
			value.Set(previous)
			restoreEmptySlices(iter.Value(), value)
			target.SetMapIndex(iter.Key(), value)
		}
	case reflect.Slice:
		if !source.IsNil() && source.Len() == 0 && target.IsNil() && target.CanSet() {
			target.Set(reflect.MakeSlice(target.Type(), 0, 0))
		}
		for i := 0; i < min(source.Len(), target.Len()); i++ {
			restoreEmptySlices(source.Index(i), target.Index(i))
		}
	case reflect.Array:
		for i := 0; i < source.Len(); i++ {
			restoreEmptySlices(source.Index(i), target.Index(i))
		}
	}
}
