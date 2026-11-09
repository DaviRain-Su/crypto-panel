package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

var rawJSONType = reflect.TypeFor[json.RawMessage]()

// DecodeJSON 严格解码一个 JSON 值，递归检查重复键和精确字段名。
// 原始解码异常可能含输入片段，因此只向调用方返回固定安全文案。
func DecodeJSON(data []byte, dst any) error {
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer || reflect.ValueOf(dst).IsNil() {
		return errors.New("JSON destination must be a non-nil pointer")
	}
	if !utf8.Valid(data) {
		return errors.New("JSON must be UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := scanJSON(dec, t.Elem(), 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("JSON: trailing data after the value")
	}
	dec = json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("JSON: invalid value or type")
	}
	return nil
}

func scanJSON(dec *json.Decoder, t reflect.Type, depth int) error {
	if depth > 128 {
		return errors.New("JSON: nesting is too deep")
	}
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == rawJSONType || (t != nil && t.Kind() == reflect.Interface) {
		t = nil
	}
	token, err := dec.Token()
	if err != nil {
		return errors.New("JSON: invalid syntax")
	}
	if token == nil {
		return errors.New("JSON: null is not a valid field value")
	}
	if delim, ok := token.(json.Delim); ok {
		switch delim {
		case '{':
			if t != nil && t.Kind() != reflect.Struct && t.Kind() != reflect.Map {
				return errors.New("JSON: expected a different field type")
			}
			fields := jsonFields(t)
			seen := map[string]bool{}
			for dec.More() {
				raw, err := dec.Token()
				if err != nil {
					return errors.New("JSON: invalid object key")
				}
				key, ok := raw.(string)
				if !ok {
					return errors.New("JSON: invalid object key")
				}
				if seen[key] {
					return errors.New("JSON: duplicate key")
				}
				seen[key] = true
				var child reflect.Type
				if t != nil {
					if t.Kind() == reflect.Map {
						child = t.Elem()
					} else {
						var found bool
						child, found = fields[key]
						if !found {
							return errors.New("JSON: unknown field")
						}
					}
				}
				if err := scanJSON(dec, child, depth+1); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim('}') {
				return errors.New("JSON: invalid object")
			}
			return nil
		case '[':
			if t != nil && t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
				return errors.New("JSON: expected a different field type")
			}
			var child reflect.Type
			if t != nil {
				child = t.Elem()
			}
			for dec.More() {
				if err := scanJSON(dec, child, depth+1); err != nil {
					return err
				}
			}
			end, err := dec.Token()
			if err != nil || end != json.Delim(']') {
				return errors.New("JSON: invalid array")
			}
			return nil
		default:
			return errors.New("JSON: unexpected delimiter")
		}
	}
	if t != nil {
		kind := t.Kind()
		switch token.(type) {
		case string:
			if kind != reflect.String {
				return errors.New("JSON: expected a different field type")
			}
		case bool:
			if kind != reflect.Bool {
				return errors.New("JSON: expected a different field type")
			}
		case json.Number:
			if kind < reflect.Int || kind > reflect.Float64 {
				return errors.New("JSON: expected a different field type")
			}
		}
	}
	return nil
}

func jsonFields(t reflect.Type) map[string]reflect.Type {
	fields := map[string]reflect.Type{}
	if t == nil || t.Kind() != reflect.Struct {
		return fields
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if tag == "-" {
			continue
		}
		if field.Anonymous && tag == "" {
			for key, ft := range jsonFields(field.Type) {
				fields[key] = ft
			}
			continue
		}
		if tag == "" {
			tag = field.Name
		}
		fields[tag] = field.Type
	}
	return fields
}
