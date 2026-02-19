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

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	dirs := []string{
		"a2a",
		"a2aclient",
		"a2acompat",
		"a2aext",
		"a2asrv",
		"log",
	}

	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	fset := token.NewFileSet()

	for _, dir := range dirs {
		path := filepath.Join(root, dir)
		pkgs, err := parser.ParseDir(fset, path, nil, parser.ParseComments)
		if err != nil {
			fmt.Printf("Error parsing %s: %v\n", path, err)
			continue
		}

		for _, pkg := range pkgs {
			if strings.HasSuffix(pkg.Name, "_test") {
				continue
			}
			for filename, file := range pkg.Files {
				if strings.HasSuffix(filename, "_test.go") {
					continue
				}
				checkFile(fset, filename, file)
			}
		}
	}
}

func checkFile(fset *token.FileSet, filename string, file *ast.File) {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil {
				// Check if receiver is exported
				var typeName string
				types := d.Recv.List[0].Type
				if star, ok := types.(*ast.StarExpr); ok {
					if ident, ok := star.X.(*ast.Ident); ok {
						typeName = ident.Name
					}
				} else if ident, ok := types.(*ast.Ident); ok {
					typeName = ident.Name
				}
				if typeName != "" && !ast.IsExported(typeName) {
					continue
				}
			}

			if !d.Name.IsExported() {
				continue
			}
			checkDoc(fset, filename, d.Name.Name, d.Doc, "func")
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if !s.Name.IsExported() {
						continue
					}
					doc := s.Doc
					if doc == nil {
						doc = d.Doc // Fallback to GenDecl doc
					}
					checkDoc(fset, filename, s.Name.Name, doc, "type")
				case *ast.ValueSpec:
					for i, name := range s.Names {
						if !name.IsExported() {
							continue
						}
						doc := s.Doc
						if doc == nil {
							doc = d.Doc // Fallback
						}
						checkDoc(fset, filename, name.Name, doc, "var/const")
						// If multiple vars are defined on one line, sharing doc is fine.
						// We check once per spec usually, but here we can check per name.
						// Actually, if it's a list, the doc applies to all.
						if i > 0 {
							break
						}
					}
				}
			}
		}
	}
}

func checkDoc(fset *token.FileSet, filename, name string, doc *ast.CommentGroup, kind string) {
	if doc == nil {
		fmt.Printf("%s: %s %s is missing doc\n", filename, kind, name)
		return
	}

	pos := fset.Position(doc.Pos())
	text := doc.Text()
	text = strings.TrimSpace(text)

	if text == "" {
		fmt.Printf("%s: %s %s has empty doc\n", pos, kind, name)
		return
	}

	if !strings.HasPrefix(text, name+" ") && !strings.HasPrefix(strings.ToLower(text), "deprecated:") {
		fmt.Printf("%s: %s %s doc does not start with symbol name\n", pos, kind, name)
	}

	if len(strings.Fields(text)) < 3 {
		fmt.Printf("%s: %s %s doc is very short: %q\n", pos, kind, name, text)
	}
}
