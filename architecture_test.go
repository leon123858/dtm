package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestBusinessLayersDoNotDependOnPersistenceModels(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate repository root")
	}
	root := filepath.Dir(currentFile)
	targets := []string{
		filepath.Join(root, "chain"),
		filepath.Join(root, "domain"),
		filepath.Join(root, "tx"),
		filepath.Join(root, "mq", "mq"),
		filepath.Join(root, "cmd", "share.go"),
	}

	for _, target := range targets {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if info.IsDir() {
			err = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() || filepath.Ext(path) != ".go" {
					return nil
				}
				checkLayerImports(t, root, path)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			continue
		}
		checkLayerImports(t, root, target)
	}
}

func checkLayerImports(t *testing.T, root, path string) {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Errorf("parse %s: %v", path, err)
		return
	}
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Errorf("parse import in %s: %v", path, err)
			continue
		}
		if importPath == "dtm/db/db" {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				relative = path
			}
			t.Errorf("%s imports persistence contract %q", relative, importPath)
		}
		if strings.HasPrefix(importPath, "dtm/") && filepath.Base(filepath.Dir(path)) == "domain" {
			t.Errorf("domain must remain a leaf package, but %s imports %q", path, importPath)
		}
	}
}
