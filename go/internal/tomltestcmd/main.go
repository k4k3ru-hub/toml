package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	toml "github.com/k4k3ru-hub/toml/go"
)

type taggedValue struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

func main() {
	var document map[string]any
	if err := toml.NewDecoder(os.Stdin).Decode(&document); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(tagDocument(document)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func tagDocument(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = tagDocument(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = tagDocument(item)
		}
		return result
	case string:
		return taggedValue{Type: "string", Value: typed}
	case bool:
		return taggedValue{Type: "bool", Value: strconv.FormatBool(typed)}
	case int64:
		return taggedValue{Type: "integer", Value: strconv.FormatInt(typed, 10)}
	case float64:
		return taggedValue{Type: "float", Value: formatFloat(typed)}
	case time.Time:
		return tagTime(typed)
	case toml.LocalDate:
		return taggedValue{Type: "date-local", Value: typed.Format("2006-01-02")}
	case toml.LocalTime:
		return taggedValue{Type: "time-local", Value: typed.Format("15:04:05.999999999")}
	case toml.LocalDateTime:
		return taggedValue{Type: "datetime-local", Value: typed.Format("2006-01-02T15:04:05.999999999")}
	default:
		panic(fmt.Sprintf("unsupported TOML test value %T", value))
	}
}

func formatFloat(value float64) string {
	if math.IsInf(value, 1) {
		return "+inf"
	}
	if math.IsInf(value, -1) {
		return "-inf"
	}
	if math.IsNaN(value) {
		return "nan"
	}
	formatted := strconv.FormatFloat(value, 'g', -1, 64)
	if !containsFloatMarker(formatted) {
		formatted += ".0"
	}
	return formatted
}

func containsFloatMarker(value string) bool {
	for _, character := range value {
		if character == '.' || character == 'e' || character == 'E' {
			return true
		}
	}
	return false
}

func tagTime(value time.Time) taggedValue {
	if value.Location() != time.Local {
		return taggedValue{Type: "datetime", Value: value.Format(time.RFC3339Nano)}
	}
	if value.Year() == 0 {
		return taggedValue{Type: "time-local", Value: value.Format("15:04:05.999999999")}
	}
	if value.Hour() == 0 && value.Minute() == 0 && value.Second() == 0 && value.Nanosecond() == 0 {
		return taggedValue{Type: "date-local", Value: value.Format("2006-01-02")}
	}
	return taggedValue{Type: "datetime-local", Value: value.Format("2006-01-02T15:04:05.999999999")}
}
