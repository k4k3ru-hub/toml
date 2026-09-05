// Package toml decodes TOML 1.0.0 documents into Go values.
package toml

import (
	"encoding"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
)

var timeType = reflect.TypeFor[time.Time]()
var localDateType = reflect.TypeFor[LocalDate]()
var localTimeType = reflect.TypeFor[LocalTime]()
var localDateTimeType = reflect.TypeFor[LocalDateTime]()
var textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()

// Decoder reads and decodes one TOML document.
type Decoder struct {
	reader io.Reader
}

// NewDecoder creates a TOML decoder.
//
// Parameters:
//   - reader: TOML document reader
//
// Returns:
//   - Configured decoder.
//
// Version:
//   - 2026-08-28: Added.
func NewDecoder(reader io.Reader) *Decoder {
	return &Decoder{reader: reader}
}

// Decode reads and decodes a TOML document.
//
// Parameters:
//   - value: non-nil pointer receiving the decoded document
//
// Returns:
//   - A read, syntax, or type-mapping error.
//
// Version:
//   - 2026-08-28: Added.
func (d *Decoder) Decode(value any) error {
	if d == nil {
		return fmt.Errorf("failed to decode toml: decoder=null")
	}
	if d.reader == nil {
		return fmt.Errorf("failed to decode toml: reader=null")
	}
	data, err := io.ReadAll(d.reader)
	if err != nil {
		return fmt.Errorf("failed to decode toml: failed to read document: %w", err)
	}
	if err := Unmarshal(data, value); err != nil {
		return fmt.Errorf("failed to decode toml: %w", err)
	}
	return nil
}

// Unmarshal decodes a TOML document into a Go value.
//
// Parameters:
//   - data: TOML document bytes
//   - value: non-nil pointer receiving the decoded document
//
// Returns:
//   - A syntax or type-mapping error.
//
// Version:
//   - 2026-08-28: Added.
func Unmarshal(data []byte, value any) error {
	target := reflect.ValueOf(value)
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		return fmt.Errorf("failed to unmarshal toml: destination=invalid")
	}
	document, err := parse(data)
	if err != nil {
		return fmt.Errorf("failed to unmarshal toml: %w", err)
	}
	if err := assignValue(target.Elem(), document, ""); err != nil {
		return fmt.Errorf("failed to unmarshal toml: %w", err)
	}
	return nil
}

func assignValue(target reflect.Value, source any, path string) error {
	if !target.CanSet() {
		return mappingError(path, "destination cannot be set")
	}
	if target.Kind() == reflect.Pointer {
		if target.IsNil() {
			target.Set(reflect.New(target.Type().Elem()))
		}
		return assignValue(target.Elem(), source, path)
	}
	if target.Type() == timeType {
		var value time.Time
		switch typed := source.(type) {
		case time.Time:
			value = typed
		case LocalDate:
			value = typed.Time
		case LocalTime:
			value = typed.Time
		case LocalDateTime:
			value = typed.Time
		default:
			return mappingError(path, "expected date or time")
		}
		target.Set(reflect.ValueOf(value))
		return nil
	}
	if target.Type() == localDateType || target.Type() == localTimeType || target.Type() == localDateTimeType {
		value := reflect.ValueOf(source)
		if value.Type() != target.Type() {
			return mappingError(path, "local date or time kind does not match destination")
		}
		target.Set(value)
		return nil
	}
	if target.CanAddr() && target.Addr().Type().Implements(textUnmarshalerType) {
		text, ok := source.(string)
		if !ok {
			return mappingError(path, "text unmarshaler requires a string")
		}
		if err := target.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(text)); err != nil {
			return mappingError(path, err.Error())
		}
		return nil
	}
	if target.Kind() == reflect.Interface {
		target.Set(reflect.ValueOf(source))
		return nil
	}

	switch target.Kind() {
	case reflect.Struct:
		return assignStruct(target, source, path)
	case reflect.Map:
		return assignMap(target, source, path)
	case reflect.Slice:
		return assignSlice(target, source, path)
	case reflect.Array:
		return assignArray(target, source, path)
	case reflect.String:
		value, ok := source.(string)
		if !ok {
			return mappingError(path, "expected string")
		}
		target.SetString(value)
	case reflect.Bool:
		value, ok := source.(bool)
		if !ok {
			return mappingError(path, "expected boolean")
		}
		target.SetBool(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value, ok := source.(int64)
		if !ok || target.OverflowInt(value) {
			return mappingError(path, "integer is incompatible or out of range")
		}
		target.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value, ok := source.(int64)
		if !ok || value < 0 || target.OverflowUint(uint64(value)) {
			return mappingError(path, "unsigned integer is incompatible or out of range")
		}
		target.SetUint(uint64(value))
	case reflect.Float32, reflect.Float64:
		value, ok := source.(float64)
		if !ok || target.OverflowFloat(value) {
			return mappingError(path, "float is incompatible or out of range")
		}
		target.SetFloat(value)
	default:
		return mappingError(path, "unsupported destination type "+target.Type().String())
	}
	return nil
}

func assignStruct(target reflect.Value, source any, path string) error {
	table, ok := source.(map[string]any)
	if !ok {
		return mappingError(path, "expected table")
	}
	fields := structFields(target.Type())
	for key, value := range table {
		fieldIndex, ok := fields[key]
		if !ok {
			fieldIndex, ok = fields[strings.ToLower(key)]
		}
		if !ok {
			continue
		}
		if err := assignValue(target.FieldByIndex(fieldIndex), value, joinPath(path, key)); err != nil {
			return err
		}
	}
	return nil
}

func structFields(target reflect.Type) map[string][]int {
	fields := make(map[string][]int)
	for index := 0; index < target.NumField(); index++ {
		field := target.Field(index)
		if field.PkgPath != "" {
			continue
		}
		tag := field.Tag.Get("toml")
		name := strings.Split(tag, ",")[0]
		if name == "-" {
			continue
		}
		if field.Anonymous && name == "" && field.Type.Kind() == reflect.Struct {
			for embeddedName, embeddedIndex := range structFields(field.Type) {
				fields[embeddedName] = append([]int{index}, embeddedIndex...)
			}
			continue
		}
		if name == "" {
			name = field.Name
		}
		fields[name] = field.Index
		fields[strings.ToLower(name)] = field.Index
	}
	return fields
}

func assignMap(target reflect.Value, source any, path string) error {
	table, ok := source.(map[string]any)
	if !ok {
		return mappingError(path, "expected table")
	}
	if target.Type().Key().Kind() != reflect.String {
		return mappingError(path, "map key must be a string")
	}
	if target.IsNil() {
		target.Set(reflect.MakeMapWithSize(target.Type(), len(table)))
	}
	for key, value := range table {
		mapped := reflect.New(target.Type().Elem()).Elem()
		if err := assignValue(mapped, value, joinPath(path, key)); err != nil {
			return err
		}
		mapKey := reflect.New(target.Type().Key()).Elem()
		mapKey.SetString(key)
		target.SetMapIndex(mapKey, mapped)
	}
	return nil
}

func assignSlice(target reflect.Value, source any, path string) error {
	values, ok := source.([]any)
	if !ok {
		return mappingError(path, "expected array")
	}
	result := reflect.MakeSlice(target.Type(), len(values), len(values))
	for index, value := range values {
		if err := assignValue(result.Index(index), value, fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
	}
	target.Set(result)
	return nil
}

func assignArray(target reflect.Value, source any, path string) error {
	values, ok := source.([]any)
	if !ok || len(values) != target.Len() {
		return mappingError(path, "array length does not match destination")
	}
	for index, value := range values {
		if err := assignValue(target.Index(index), value, fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func mappingError(path string, reason string) error {
	if path == "" {
		path = "<document>"
	}
	return fmt.Errorf("failed to map toml value: %s: path=%q", reason, path)
}

func joinPath(parent string, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
