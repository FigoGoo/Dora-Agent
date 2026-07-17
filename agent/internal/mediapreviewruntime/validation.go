package mediapreviewruntime

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"unicode/utf8"
)

func strictDecode(encoded []byte, target any) error {
	if len(encoded) == 0 || len(encoded) > 64*1024 || !utf8.Valid(encoded) {
		return ErrOutputContract
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrOutputContract, err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ErrOutputContract
	}
	return nil
}
