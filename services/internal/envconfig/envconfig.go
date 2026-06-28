package envconfig

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Values map[string]string

func Load(paths ...string) (Values, error) {
	values := Values{}
	for _, path := range paths {
		if path == "" {
			continue
		}
		if err := readFile(values, path); err != nil {
			return nil, err
		}
	}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			values[key] = value
		}
	}
	return values, nil
}

func readFile(values Values, path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open env file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("parse env file %s:%d: missing '='", path, lineNo)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("parse env file %s:%d: empty key", path, lineNo)
		}
		values[key] = trimValue(value)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan env file %s: %w", path, err)
	}
	return nil
}

func trimValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
		if value[0] == '"' && value[len(value)-1] == '"' {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func (v Values) String(key, fallback string) string {
	if value, ok := v[key]; ok {
		return value
	}
	return fallback
}

func (v Values) Required(key string) (string, error) {
	value := strings.TrimSpace(v[key])
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func (v Values) Bool(key string, fallback bool) (bool, error) {
	raw, ok := v[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("%s must be boolean: %w", key, err)
	}
	return parsed, nil
}

func (v Values) Int(key string, fallback int) (int, error) {
	raw, ok := v[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be integer: %w", key, err)
	}
	return parsed, nil
}

func (v Values) Duration(key string, fallback time.Duration) (time.Duration, error) {
	raw, ok := v[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s must be duration: %w", key, err)
	}
	return parsed, nil
}

func (v Values) Milliseconds(key string, fallback time.Duration) (time.Duration, error) {
	ms, err := v.Int(key, int(fallback/time.Millisecond))
	if err != nil {
		return 0, err
	}
	return time.Duration(ms) * time.Millisecond, nil
}

func (v Values) CSV(key string) []string {
	raw := strings.TrimSpace(v[key])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			items = append(items, item)
		}
	}
	return items
}
