package protocol_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/protocol"
	"github.com/rizqyep/rizqyep-elsa-assignment/server/internal/quiz"
)

// serverPayloads maps each server message type to its Go payload type.
var serverPayloads = map[protocol.Type]func() any{
	protocol.TypeSnapshot:       func() any { return new(protocol.Snapshot) },
	protocol.TypeQuestion:       func() any { return new(protocol.QuestionMsg) },
	protocol.TypeQuestionClosed: func() any { return new(protocol.QuestionClosed) },
	protocol.TypeAnswerResult:   func() any { return new(protocol.AnswerResult) },
	protocol.TypeRank:           func() any { return new(protocol.Rank) },
	protocol.TypeLeaderboard:    func() any { return new(protocol.Leaderboard) },
	protocol.TypeQuizFinished:   func() any { return new(protocol.QuizFinished) },
	protocol.TypeQuizState:      func() any { return new(protocol.QuizState) },
	protocol.TypeError:          func() any { return new(protocol.Error) },
	protocol.TypePong:           func() any { return new(protocol.Pong) },
}

func TestTypes_MatchSchemaFiles(t *testing.T) {
	var clientFiles, serverFiles []string
	for rel := range loadSchemas(t) {
		dir, file, ok := strings.Cut(strings.TrimPrefix(rel, "ws/"), "/")
		if !ok {
			continue
		}
		name := strings.TrimSuffix(file, ".json")
		if dir == "client" {
			clientFiles = append(clientFiles, name)
		} else {
			serverFiles = append(serverFiles, name)
		}
	}
	check := func(side string, fromSchemas []string, fromGo []protocol.Type) {
		var goNames []string
		for _, typ := range fromGo {
			goNames = append(goNames, string(typ))
		}
		slices.Sort(fromSchemas)
		slices.Sort(goNames)
		if !slices.Equal(fromSchemas, goNames) {
			t.Errorf("%s types: schemas %v, Go %v", side, fromSchemas, goNames)
		}
	}
	check("client", clientFiles, protocol.ClientTypes())
	check("server", serverFiles, protocol.ServerTypes())
}

func TestErrorCodes_MatchSchemaEnum(t *testing.T) {
	common := loadSchemas(t)["common.json"].raw["definitions"].(map[string]any)
	var fromSchema []string
	for _, v := range common["ErrorCode"].(map[string]any)["enum"].([]any) {
		fromSchema = append(fromSchema, v.(string))
	}
	var fromGo []string
	for _, c := range protocol.ErrorCodes() {
		fromGo = append(fromGo, string(c))
	}
	slices.Sort(fromSchema)
	slices.Sort(fromGo)
	if !slices.Equal(fromSchema, fromGo) {
		t.Errorf("error codes differ:\n  schema %v\n  Go     %v", fromSchema, fromGo)
	}
}

func TestDecodeClient_SchemaExamplesDecode(t *testing.T) {
	want := map[protocol.Type]any{
		protocol.TypeJoin:         protocol.Join{},
		protocol.TypeWatch:        protocol.Watch{},
		protocol.TypeSubmitAnswer: protocol.SubmitAnswer{},
		protocol.TypePing:         protocol.Ping{},
		protocol.TypeLeave:        protocol.Leave{},
	}
	for rel, f := range loadSchemas(t) {
		if !strings.HasPrefix(rel, "ws/client/") {
			continue
		}
		frame, err := json.Marshal(f.raw["examples"].([]any)[0])
		if err != nil {
			t.Fatal(err)
		}
		msg, err := protocol.DecodeClient(frame)
		if err != nil {
			t.Errorf("%s: example rejected: %v", rel, err)
			continue
		}
		if reflect.TypeOf(msg.Payload) != reflect.TypeOf(want[msg.Type]) {
			t.Errorf("%s: payload is %T, want %T", rel, msg.Payload, want[msg.Type])
		}
		// The typed payload must carry every field of the example.
		got, _ := json.Marshal(msg.Payload)
		wantData, _ := json.Marshal(f.raw["examples"].([]any)[0].(map[string]any)["data"])
		if !jsonEqual(t, got, wantData) {
			t.Errorf("%s: decoded payload %s, want %s", rel, got, wantData)
		}
	}
}

func TestDecodeClient_Rejects(t *testing.T) {
	cases := []struct {
		name  string
		frame string
		code  protocol.ErrorCode
	}{
		{"malformed JSON", `{"v":1,"type":"join"`, protocol.CodeInvalidMessage},
		{"not an object", `[1,2,3]`, protocol.CodeInvalidMessage},
		{"unsupported version", `{"v":2,"type":"ping","data":{"clientTime":1}}`, protocol.CodeUnsupportedVersion},
		{"missing version", `{"type":"ping","data":{"clientTime":1}}`, protocol.CodeUnsupportedVersion},
		{"unknown type", `{"v":1,"type":"dance","data":{}}`, protocol.CodeUnknownType},
		{"server type sent by a client", `{"v":1,"type":"snapshot","id":"x","data":{}}`, protocol.CodeUnknownType},
		{"join without request id", `{"v":1,"type":"join","data":{"quizCode":"K7Q2MX","displayName":"Rina"}}`, protocol.CodeInvalidMessage},
		{"join without quiz code", `{"v":1,"type":"join","id":"r-1","data":{"displayName":"Rina"}}`, protocol.CodeInvalidMessage},
		{"join with look-alike in code", `{"v":1,"type":"join","id":"r-1","data":{"quizCode":"K7Q2M0","displayName":"Rina"}}`, protocol.CodeInvalidMessage},
		{"join with 21-character name", `{"v":1,"type":"join","id":"r-1","data":{"quizCode":"K7Q2MX","displayName":"` + strings.Repeat("x", 21) + `"}}`, protocol.CodeInvalidMessage},
		{"answer without option", `{"v":1,"type":"submit_answer","id":"r-7","data":{"questionId":"syn-03"}}`, protocol.CodeInvalidMessage},
		{"answer with wrong field type", `{"v":1,"type":"submit_answer","id":"r-7","data":{"questionId":3,"optionId":"a"}}`, protocol.CodeInvalidMessage},
		{"unknown field (server is strict on input)", `{"v":1,"type":"ping","data":{"clientTime":1,"extra":true}}`, protocol.CodeInvalidMessage},
		{"negative client time", `{"v":1,"type":"ping","data":{"clientTime":-5}}`, protocol.CodeInvalidMessage},
		{"data missing", `{"v":1,"type":"ping"}`, protocol.CodeInvalidMessage},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := protocol.DecodeClient([]byte(tc.frame))
			var de *protocol.DecodeError
			if !errors.As(err, &de) || de.Code != tc.code {
				t.Errorf("got %v, want a DecodeError with code %q", err, tc.code)
			}
		})
	}
}

func TestDecodeClient_KeepsRequestID(t *testing.T) {
	msg, err := protocol.DecodeClient([]byte(`{"v":1,"type":"submit_answer","id":"r-7","data":{"questionId":"syn-03","optionId":"syn-03-b"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != "r-7" || msg.Type != protocol.TypeSubmitAnswer {
		t.Errorf("got id %q type %q", msg.ID, msg.Type)
	}
	if got := msg.Payload.(protocol.SubmitAnswer); got.QuestionID != "syn-03" || got.OptionID != "syn-03-b" {
		t.Errorf("payload %+v", got)
	}
}

func TestEncode_ServerExamplesRoundTripAndValidate(t *testing.T) {
	schemas := loadSchemas(t)
	for typ, newPayload := range serverPayloads {
		f := schemas["ws/server/"+string(typ)+".json"]
		example := f.raw["examples"].([]any)[0].(map[string]any)
		data, _ := json.Marshal(example["data"])
		payload := newPayload()
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(payload); err != nil {
			t.Errorf("%s: example data doesn't fit the Go type: %v", typ, err)
			continue
		}
		id, _ := example["id"].(string)
		frame, err := protocol.Encode(typ, id, reflect.ValueOf(payload).Elem().Interface())
		if err != nil {
			t.Errorf("%s: encode: %v", typ, err)
			continue
		}
		if err := f.schema.Validate(toSchemaValue(t, frame)); err != nil {
			t.Errorf("%s: encoded frame fails its schema: %v", typ, err)
		}
		want, _ := json.Marshal(example)
		if !jsonEqual(t, frame, want) {
			t.Errorf("%s: round trip changed the message\n got %s\nwant %s", typ, frame, want)
		}
	}
}

func TestEncode_EmptyListsAreArrays(t *testing.T) {
	schemas := loadSchemas(t)
	for _, tc := range []struct {
		typ     protocol.Type
		payload any
	}{
		{protocol.TypeLeaderboard, protocol.Leaderboard{Version: 1}},
		{protocol.TypeQuizFinished, protocol.QuizFinished{StateVersion: 9}},
	} {
		frame, err := protocol.Encode(tc.typ, "", tc.payload)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(frame, []byte("null")) {
			t.Errorf("%s: empty list encoded as null: %s", tc.typ, frame)
		}
		if err := schemas["ws/server/"+string(tc.typ)+".json"].schema.Validate(toSchemaValue(t, frame)); err != nil {
			t.Errorf("%s: %v", tc.typ, err)
		}
	}
}

func TestQuestionFrom_NeverCarriesTheAnswerKey(t *testing.T) {
	q := quiz.Question{
		ID:              "syn-03",
		Prompt:          "Choose the synonym of 'rapid'",
		Options:         []quiz.Option{{ID: "syn-03-a", Text: "slow"}, {ID: "syn-03-b", Text: "quick"}},
		CorrectOptionID: "secret-answer-key",
	}
	msg := protocol.QuestionMsg{Question: protocol.QuestionFrom(q.Public(), 2, 10, 1, 15_001, 15_001), StateVersion: 6}
	frame, err := protocol.Encode(protocol.TypeQuestion, "", msg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(frame, []byte("secret-answer-key")) || bytes.Contains(frame, []byte("correct")) {
		t.Errorf("question frame leaks the answer key: %s", frame)
	}
}

// The protocol package must not reference quiz.Question (only quiz.PublicQuestion), so the answer
// key has no path to a client frame (FR-21).
func TestProtocolPackage_NeverReferencesQuizQuestion(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "quiz" && sel.Sel.Name == "Question" {
					t.Errorf("%s: references quiz.Question at %s", name, fset.Position(sel.Pos()))
				}
			}
			return true
		})
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

func TestValidateServer_AcceptsExamplesAndRejectsContractBreaks(t *testing.T) {
	schemas := loadSchemas(t)
	for _, typ := range protocol.ServerTypes() {
		frame, _ := json.Marshal(schemas["ws/server/"+string(typ)+".json"].raw["examples"].([]any)[0])
		if err := protocol.ValidateServer(frame); err != nil {
			t.Errorf("%s example rejected: %v", typ, err)
		}
	}
	bad := map[string]string{
		"answer key in a question": `{"v":1,"type":"question","data":{"stateVersion":2,"question":{"questionId":"q","index":0,"count":1,"prompt":"p","options":[{"id":"a","text":"A"},{"id":"b","text":"B"}],"openedAt":1,"deadline":2,"closeAt":2,"correctOptionId":"a"}}}`,
		"unknown type":             `{"v":1,"type":"surprise","data":{}}`,
		"not json":                 `nope`,
	}
	for name, frame := range bad {
		if err := protocol.ValidateServer([]byte(frame)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
