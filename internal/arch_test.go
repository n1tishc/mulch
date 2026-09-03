package internal_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAgentScoringAndInterventionImportBoundaries(t *testing.T) {
	checks := map[string][]string{"agent": {"/internal/score", "/internal/intervene"}, "score": {"/internal/agent"}, "intervene": {"/internal/agent"}}
	for pkg, forbidden := range checks {
		root := filepath.Join(pkg)
		if _, err := os.Stat(root); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				return err
			}
			for _, imp := range file.Imports {
				value, _ := strconv.Unquote(imp.Path.Value)
				for _, suffix := range forbidden {
					if strings.HasSuffix(value, suffix) {
						t.Errorf("%s imports forbidden package %s", path, value)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
