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

// A script used to process results of `jsonrpc_gen.sh`.
// Reads the generated output into a map[typeName][]field where fields contain tags.
// Updates core types in a2a package with tags based on type&field name match.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"slices"
	"strings"
)

type field struct {
	Name string `json:"name"`
	Tags string `json:"tags"`
}

type typeMap map[string][]field

func (t typeMap) types() []string {
	result := make([]string, 0, len(t))
	for k := range t {
		result = append(result, k)
	}
	return result
}

func main() {
	genOutput := "./internal/jsonrpc/spec.go"
	spec, err := os.Open(genOutput)
	if err != nil {
		log.Fatalf("failed to open %s file: %v", genOutput, err)
	}

	files, err := os.ReadDir("./a2a")
	if err != nil {
		log.Fatalf("failed to read dir files: %v", err)
	}

	tMap := readTypes(spec)
	deleteJsonrpcSpecific(tMap)
	fixRenamedTypes(tMap)

	for _, fi := range files {
		p := path.Join("./a2a", fi.Name())

		f, err := os.Open(p)
		if err != nil {
			log.Fatalf("failed to open %s file: %v", f.Name(), err)
		}

		tags := writeFieldTags(f, tMap)

		if err := os.WriteFile(p, []byte(tags), fi.Type().Perm()); err != nil {
			log.Fatalf("failed to write to %s file: %v", f.Name(), err)
		}
	}

	if len(tMap) > 0 {
		wrapper := map[string]any{"types": tMap.types(), "fields": tMap}
		json, err := json.MarshalIndent(wrapper, "", "  ")
		if err != nil {
			log.Fatalf("failed to marshal remaining tags: %v", err)
		}
		if err := os.WriteFile("./internal/jsonrpc/remaining.json", json, 0644); err != nil {
			log.Fatalf("failed to write remaining types: %v", err)
		}
	}
}

func writeFieldTags(file *os.File, tMap typeMap) string {
	var fields []field
	var sb strings.Builder
	reader := bufio.NewReader(file)
	for {
		line, _, err := reader.ReadLine()
		if err == io.EOF {
			break
		}
		if name, ok := getTypeName(line); ok {
			fields = tMap[name]
			delete(tMap, name)
		}
		if bytes.Equal(line, []byte("}")) {
			writeUnrecognizedFields(&sb, fields)
			fields = nil
		}
		sb.Write(line)
		if fields == nil {
			sb.WriteRune('\n')
			continue
		}
		trimmed := bytes.Trim(line, " \t")
		if bytes.HasPrefix(trimmed, []byte("//")) || len(trimmed) == 0 {
			sb.WriteRune('\n')
			continue
		}
		fieldI := findFieldIndex(fields, trimmed)
		if fieldI < 0 {
			sb.WriteRune('\n')
			continue
		}
		sb.WriteString(" `")
		sb.WriteString(fields[fieldI].Tags)
		sb.WriteString("`\n")

		fields = append(fields[:fieldI], fields[fieldI+1:]...)
	}
	return sb.String()
}

func findFieldIndex(fields []field, fieldLine []byte) int {
	name := string(bytes.Split(fieldLine, []byte(" "))[0])
	return slices.IndexFunc(fields, func(f field) bool {
		return f.Name == name
	})
}

func getIdiomaticName(name string) string {
	common := map[string]string{
		"ContextId": "ContextID",
		"Id":        "ID",
		"TaskId":    "TaskID",
		"Url":       "URL",
		"Uri":       "URI",
	}
	if renamed, ok := common[name]; ok {
		return renamed
	}
	return strings.ReplaceAll(name, "Url", "URL")
}

func writeUnrecognizedFields(sb *strings.Builder, fields []field) {
	if len(fields) == 0 {
		return
	}
	sb.WriteString("\n//Unrecognized fields:\n")
	for _, f := range fields {
		fmt.Fprintf(sb, "//%s: %s\n", f.Name, f.Tags)
	}
}

// removes types represeting jsonrpc request-response wrappers and protocol errors
func deleteJsonrpcSpecific(tMap typeMap) {
	var toDelete []string
	for typeName, fields := range tMap {
		if strings.HasSuffix(typeName, "Error") {
			toDelete = append(toDelete, typeName)
			continue
		}
		for _, f := range fields {
			if f.Name == "Jsonrpc" {
				toDelete = append(toDelete, typeName)
				continue
			}
		}
	}
	for _, k := range toDelete {
		delete(tMap, k)
	}
}

func fixRenamedTypes(tMap typeMap) {
	renamed := map[string]string{
		"GetTaskPushNotificationConfigParams":        "GetTaskPushConfigParams",
		"MessageSendConfiguration":                   "MessageSendConfig",
		"PushNotificationAuthenticationInfo":         "PushAuthInfo",
		"TaskIdParams":                               "TaskIDParams",
		"ListTaskPushNotificationConfigParams":       "ListTaskPushConfigParams",
		"GetTaskPushNotificationConfigRequestParams": "GetTaskPushConfigParams",
		"PushNotificationConfig":                     "PushConfig",
		"OpenIdConnectSecurityScheme":                "OpenIDConnectSecurityScheme",
		"DeleteTaskPushNotificationConfigParams":     "DeleteTaskPushConfigParams",
		"TaskPushNotificationConfig":                 "TaskPushConfig",
		"SecuritySchemeBase":                         "",
		"SendMessageSuccessResponseResult":           "",
		"SendStreamingMessageSuccessResponseResult":  "",
	}
	for k, v := range renamed {
		if len(v) > 0 {
			tMap[v] = tMap[k]
		}
		delete(tMap, k)
	}
}

func readTypes(file *os.File) typeMap {
	var currType string
	typeToFields := map[string][]field{}
	reader := bufio.NewReader(file)
	for {
		line, _, err := reader.ReadLine()
		if err == io.EOF {
			break
		}
		if name, ok := getTypeName(line); ok {
			currType = name
			typeToFields[currType] = []field{}
		}
		if bytes.Count(line, []byte("`")) == 2 {
			oti, cti := bytes.Index(line, []byte("`")), bytes.LastIndex(line, []byte("`"))
			tags := line[oti+1 : cti]
			name := bytes.Split(bytes.Trim(line, " \t"), []byte(" "))[0]
			f := field{Name: getIdiomaticName(string(name)), Tags: string(tags)}
			typeToFields[currType] = append(typeToFields[currType], f)
		}
	}
	return typeToFields
}

func getTypeName(line []byte) (string, bool) {
	nameEnd := bytes.Index(line, []byte(" struct {"))
	if nameEnd < 0 {
		return "", false
	}
	return string(line[len("type "):nameEnd]), true
}
