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

package utils

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestDeepCopyPreservesEmptyCollections 验证复制保持空数组、显式 null、嵌套元数据及写入隔离。
func TestDeepCopyPreservesEmptyCollections(t *testing.T) {
	t.Parallel()
	source := map[string]any{"empty": []any{}, "null": nil, "nested": map[string]any{"items": []any{}, "body": []any{"正文"}}, "metadata": []any{map[string]any{"empty": []any{}}}}
	copied, err := DeepCopy(&source)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(source, *copied); diff != "" {
		t.Fatalf("DeepCopy() wrong result (-want +got) diff = %s", diff)
	}
	(*copied)["nested"].(map[string]any)["body"].([]any)[0] = "changed"
	if source["nested"].(map[string]any)["body"].([]any)[0] != "正文" {
		t.Fatal("copy shares source memory")
	}
}

// TestDeepCopyPreservesNonJSONTypes 验证数组、指针、字节和 nil 切片不因 JSON 场景修复改变语义。
func TestDeepCopyPreservesNonJSONTypes(t *testing.T) {
	t.Parallel()
	type sample struct {
		Empty   []byte
		Nil     []byte
		Nested  [1][]int
		Pointer *[]string
		Integer uint64
	}
	empty := []string{"kept"}
	source := sample{Empty: []byte{}, Nested: [1][]int{{}}, Pointer: &empty, Integer: ^uint64(0)}
	copied, err := DeepCopy(&source)
	if err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(source, *copied); diff != "" {
		t.Fatalf("DeepCopy() wrong result (-want +got) diff = %s", diff)
	}
	if source.Pointer == copied.Pointer {
		t.Fatal("copy shares pointer")
	}
}

// emptySliceCodec 验证自定义 gob 编码类型保留自身定义的空值语义。
type emptySliceCodec struct {
	Values []string
}

// GobEncode 使用固定有效载荷模拟拥有私有表示的类型。
func (emptySliceCodec) GobEncode() ([]byte, error) {
	return []byte{1}, nil
}

// GobDecode 有意将空切片归一化为 nil，深复制不能覆盖此约定。
func (v *emptySliceCodec) GobDecode([]byte) error {
	v.Values = nil
	return nil
}

// TestDeepCopyRespectsCustomGobCodec 验证反射恢复不改变自定义编码器的结果。
func TestDeepCopyRespectsCustomGobCodec(t *testing.T) {
	t.Parallel()
	source := emptySliceCodec{Values: []string{}}
	copied, err := DeepCopy(&source)
	if err != nil {
		t.Fatalf("DeepCopy() error = %v, want nil", err)
	}
	if diff := cmp.Diff(emptySliceCodec{}, *copied); diff != "" {
		t.Fatalf("DeepCopy() wrong result (-want +got) diff = %s", diff)
	}
}
