package protocol_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// Contract checks for docs/api (TRD §6.4). They guard the contract files themselves;
// task-09 adds checks that Go message types match them.

const draft07 = "http://json-schema.org/draft-07/schema#"

func apiDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "docs", "api"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

type schemaFile struct {
	rel    string
	raw    map[string]any
	schema *jsonschema.Schema
}

// loadSchemas compiles every JSON Schema under docs/api/schemas.
func loadSchemas(t *testing.T) map[string]schemaFile {
	t.Helper()
	root := filepath.Join(apiDir(t), "schemas")
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	c.UseLoader(jsonschema.SchemeURLLoader{"file": jsonschema.FileLoader{}})

	out := map[string]schemaFile{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		raw := decodeFile(t, path).(map[string]any)
		sch, err := c.Compile("file://" + filepath.ToSlash(path))
		if err != nil {
			t.Errorf("%s does not compile: %v", rel, err)
			return nil
		}
		out[filepath.ToSlash(rel)] = schemaFile{rel: rel, raw: raw, schema: sch}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("no schemas found")
	}
	return out
}

func decodeFile(t *testing.T, path string) any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

// toSchemaValue decodes a frame the way the schema validator expects (numbers as json.Number).
func toSchemaValue(t *testing.T, frame []byte) any {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(frame))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// firstExample returns a deep copy of a schema's first example, safe to mutate.
func firstExample(t *testing.T, f schemaFile) map[string]any {
	t.Helper()
	ex, ok := f.raw["examples"].([]any)
	if !ok || len(ex) == 0 {
		t.Fatalf("%s has no examples", f.rel)
	}
	b, err := json.Marshal(ex[0])
	if err != nil {
		t.Fatal(err)
	}
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

func TestSchemas_DeclareDraft07(t *testing.T) {
	for rel, f := range loadSchemas(t) {
		if f.raw["$schema"] != draft07 {
			t.Errorf("%s: $schema = %v, want %s (AsyncAPI 3.0 supports draft-07 only)", rel, f.raw["$schema"], draft07)
		}
	}
}

func TestSchemas_EveryMessageHasAValidExample(t *testing.T) {
	for rel, f := range loadSchemas(t) {
		if !strings.HasPrefix(rel, "ws/") {
			continue // common.json holds definitions only
		}
		ex, _ := f.raw["examples"].([]any)
		if len(ex) == 0 {
			t.Errorf("%s: no example", rel)
		}
		for i, e := range ex {
			if err := f.schema.Validate(e); err != nil {
				t.Errorf("%s: example %d is invalid: %v", rel, i, err)
			}
		}
	}
}

func TestSchemas_RejectInvalidMessages(t *testing.T) {
	schemas := loadSchemas(t)
	cases := []struct {
		name   string
		file   string
		mutate func(m map[string]any)
	}{
		{"question leaks its correct option (FR-21)", "ws/server/question.json", func(m map[string]any) {
			m["data"].(map[string]any)["question"].(map[string]any)["correctOptionId"] = "syn-03-b"
		}},
		{"quiz code contains a look-alike (D16)", "ws/client/join.json", func(m map[string]any) {
			m["data"].(map[string]any)["quizCode"] = "K7Q2M0"
		}},
		{"display name longer than 20 characters (FR-15)", "ws/client/join.json", func(m map[string]any) {
			m["data"].(map[string]any)["displayName"] = strings.Repeat("x", 21)
		}},
		{"answer without request id (FR-18)", "ws/client/submit_answer.json", func(m map[string]any) {
			delete(m, "id")
		}},
		{"points above the maximum of 200 (FR-19)", "ws/server/answer_result.json", func(m map[string]any) {
			m["data"].(map[string]any)["points"] = json.Number("201")
		}},
		{"unsupported protocol version", "ws/client/join.json", func(m map[string]any) {
			m["v"] = json.Number("2")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, ok := schemas[tc.file]
			if !ok {
				t.Fatalf("schema %s not found", tc.file)
			}
			msg := firstExample(t, f)
			tc.mutate(msg)
			if err := f.schema.Validate(msg); err == nil {
				t.Errorf("%s accepted an invalid message", tc.file)
			}
		})
	}
}

func TestQuizCodePattern_SameInOpenAPIAndSchemas(t *testing.T) {
	common := decodeFile(t, filepath.Join(apiDir(t), "schemas", "common.json")).(map[string]any)
	wsPattern := common["definitions"].(map[string]any)["QuizCode"].(map[string]any)["pattern"]

	b, err := os.ReadFile(filepath.Join(apiDir(t), "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas map[string]struct {
				Pattern string `yaml:"pattern"`
			} `yaml:"schemas"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	restPattern := spec.Components.Schemas["QuizCode"].Pattern
	if restPattern == "" || restPattern != wsPattern {
		t.Errorf("quiz code pattern differs: openapi.yaml %q, common.json %q", restPattern, wsPattern)
	}
}
