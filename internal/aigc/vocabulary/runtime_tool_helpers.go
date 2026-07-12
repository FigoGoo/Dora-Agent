package vocabulary

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

func isNilDependency(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func cloneJSONMap(value map[string]any) (map[string]any, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("value is not JSON-compatible: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var cloned map[string]any
	if err := decoder.Decode(&cloned); err != nil {
		return nil, fmt.Errorf("decode JSON-compatible value: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode JSON-compatible value: unexpected trailing value")
		}
		return nil, fmt.Errorf("decode JSON-compatible value: %w", err)
	}
	return cloned, nil
}
