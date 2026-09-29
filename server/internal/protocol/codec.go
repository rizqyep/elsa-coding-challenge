package protocol

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// schemas is a copy of docs/api/schemas made by `make generate` (Go can't embed outside the module).
//
//go:embed schemas
var schemas embed.FS

const schemaBase = "embed:///"

type embedLoader struct{}

func (embedLoader) Load(url string) (any, error) {
	f, err := schemas.Open("schemas/" + strings.TrimPrefix(url, schemaBase))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return jsonschema.UnmarshalJSON(f)
}

var (
	clientSchemas = sync.OnceValues(func() (map[Type]*jsonschema.Schema, error) { return compile("client", ClientTypes()) })
	serverSchemas = sync.OnceValues(func() (map[Type]*jsonschema.Schema, error) { return compile("server", ServerTypes()) })
)

func compile(side string, types []Type) (map[Type]*jsonschema.Schema, error) {
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	c.UseLoader(jsonschema.SchemeURLLoader{"embed": embedLoader{}})
	out := make(map[Type]*jsonschema.Schema)
	for _, t := range types {
		s, err := c.Compile(schemaBase + "ws/" + side + "/" + string(t) + ".json")
		if err != nil {
			return nil, fmt.Errorf("compile %s schema: %w", t, err)
		}
		out[t] = s
	}
	return out, nil
}

// ValidateServer checks a server frame against its JSON Schema; for tests and the test kit.
func ValidateServer(frame []byte) error {
	var head struct {
		Type Type `json:"type"`
	}
	if err := json.Unmarshal(frame, &head); err != nil {
		return err
	}
	all, err := serverSchemas()
	if err != nil {
		return err
	}
	schema, ok := all[head.Type]
	if !ok {
		return fmt.Errorf("unknown server message type %q", head.Type)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(frame))
	if err != nil {
		return err
	}
	return schema.Validate(doc)
}

// ClientMessage is a decoded, schema-valid client frame.
type ClientMessage struct {
	ID      string
	Type    Type
	Payload any // Join, Watch, SubmitAnswer, or Ping
}

// DecodeClient validates a client frame against its JSON Schema and decodes its payload (NFR-20).
func DecodeClient(frame []byte) (ClientMessage, error) {
	var head struct {
		V    *int            `json:"v"`
		Type Type            `json:"type"`
		ID   string          `json:"id"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(frame, &head); err != nil {
		return ClientMessage{}, &DecodeError{CodeInvalidMessage, err}
	}
	if head.V == nil || *head.V != Version {
		return ClientMessage{}, &DecodeError{CodeUnsupportedVersion, fmt.Errorf("protocol version must be %d", Version)}
	}
	all, err := clientSchemas()
	if err != nil {
		return ClientMessage{}, &DecodeError{CodeInternal, err}
	}
	schema, ok := all[head.Type]
	if !ok {
		return ClientMessage{}, &DecodeError{CodeUnknownType, fmt.Errorf("unknown message type %q", head.Type)}
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(frame))
	if err != nil {
		return ClientMessage{}, &DecodeError{CodeInvalidMessage, err}
	}
	if err := schema.Validate(doc); err != nil {
		return ClientMessage{}, &DecodeError{CodeInvalidMessage, err}
	}
	payload, err := decodePayload(head.Type, head.Data)
	if err != nil {
		return ClientMessage{}, &DecodeError{CodeInvalidMessage, err}
	}
	return ClientMessage{ID: head.ID, Type: head.Type, Payload: payload}, nil
}

func decodePayload(t Type, data json.RawMessage) (any, error) {
	switch t {
	case TypeJoin:
		return decodeAs[Join](data)
	case TypeWatch:
		return decodeAs[Watch](data)
	case TypeSubmitAnswer:
		return decodeAs[SubmitAnswer](data)
	case TypePing:
		return decodeAs[Ping](data)
	case TypeLeave:
		return decodeAs[Leave](data)
	}
	return nil, errors.New("no payload type for " + string(t))
}

func decodeAs[T any](data json.RawMessage) (T, error) {
	var v T
	err := json.Unmarshal(data, &v)
	return v, err
}

// Encode wraps a server payload in the protocol envelope.
func Encode(t Type, id string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode %s payload: %w", t, err)
	}
	return json.Marshal(struct {
		V    int             `json:"v"`
		Type Type            `json:"type"`
		ID   string          `json:"id,omitempty"`
		Data json.RawMessage `json:"data"`
	}{Version, t, id, data})
}
