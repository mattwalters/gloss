package main

import (
	"encoding/json"
	"io"

	"github.com/writtendev/writ/cmd/writ/internal/wire"
)

// emitJSON formats data into the versioned envelope and writes it as a single
// JSON document with a trailing newline.
func emitJSON(w io.Writer, kind string, data any) error {
	env := wire.Envelope{
		SchemaVersion: wire.CurrentSchemaVersion,
		Kind:          kind,
		Data:          data,
	}
	enc := json.NewEncoder(w)
	return enc.Encode(env)
}
