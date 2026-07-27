package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestGeneratedBrowserSchemasAreCurrent(t *testing.T) {
	documents, err := Documents()
	if err != nil {
		t.Fatalf("build schema documents: %v", err)
	}
	root := filepath.Join("..", "..", "packages", "contracts", "generated-go")
	for name, document := range documents {
		path := filepath.Join(root, name)
		current, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v", path, err)
			continue
		}
		if !bytes.Equal(current, document) {
			t.Errorf("%s is stale; run go generate ./internal/contracts", path)
		}
	}
	module, err := TypeScriptModule()
	if err != nil {
		t.Fatalf("build TypeScript schema module: %v", err)
	}
	path := filepath.Join(
		"..",
		"..",
		"packages",
		"contracts",
		"src",
		"generated",
		"browser-schemas.ts",
	)
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(current, module) {
		t.Errorf("%s is stale; run go generate ./internal/contracts", path)
	}
}

func TestGeneratedSchemasUseJSONNumbersForNumericKeywords(t *testing.T) {
	documents, err := Documents()
	if err != nil {
		t.Fatalf("build schema documents: %v", err)
	}
	for name, document := range documents {
		var schema any
		if err := json.Unmarshal(document, &schema); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		assertNumericSchemaKeywords(t, name, schema)
	}
}

func TestTypeScriptModuleEndsWithOneNewline(t *testing.T) {
	module, err := TypeScriptModule()
	if err != nil {
		t.Fatalf("build TypeScript schema module: %v", err)
	}
	if !bytes.HasSuffix(module, []byte("\n")) {
		t.Fatal("TypeScript schema module does not end with a newline")
	}
	if bytes.HasSuffix(module, []byte("\n\n")) {
		t.Fatal("TypeScript schema module ends with a blank line")
	}
}

func assertNumericSchemaKeywords(t *testing.T, location string, value any) {
	t.Helper()
	switch typed := value.(type) {
	case []any:
		for index, nested := range typed {
			assertNumericSchemaKeywords(
				t,
				fmt.Sprintf("%s[%d]", location, index),
				nested,
			)
		}
	case map[string]any:
		for key, nested := range typed {
			if numericSchemaKeyword(key) {
				if _, ok := nested.(float64); !ok {
					t.Errorf("%s.%s = %#v, want JSON number", location, key, nested)
				}
			}
			assertNumericSchemaKeywords(t, location+"."+key, nested)
		}
	}
}
