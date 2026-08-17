package bilog

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type FieldType string

const (
	FieldString FieldType = "string"
	FieldNumber FieldType = "number"
	FieldBool   FieldType = "bool"
)

type FieldSpec struct {
	Name        string    `json:"name"`
	Type        FieldType `json:"type"`
	Required    bool      `json:"required,omitempty"`
	Description string    `json:"description,omitempty"`
}

type EventSpec struct {
	Name        string   `json:"name"`
	Fields      []string `json:"fields,omitempty"`
	Description string   `json:"description,omitempty"`
}

type Schema struct {
	Fields map[string]FieldSpec `json:"fields"`
	Events map[string]EventSpec `json:"events"`
}

func LoadSchema(path string) (*Schema, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- Schema 路径由工具/测试传入的受控文件路径提供。
	if err != nil {
		return nil, err
	}
	var schema Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	if err := schema.Validate(); err != nil {
		return nil, err
	}
	return &schema, nil
}

func (s *Schema) Validate() error {
	if s == nil {
		return nil
	}
	for name, field := range s.Fields {
		field.Name = strings.TrimSpace(field.Name)
		if field.Name == "" {
			field.Name = strings.TrimSpace(name)
		}
		if field.Name == "" {
			return fmt.Errorf("bilog field name is required")
		}
		if field.Type == "" {
			return fmt.Errorf("bilog field %s type is required", field.Name)
		}
		switch field.Type {
		case FieldString, FieldNumber, FieldBool:
		default:
			return fmt.Errorf("bilog field %s has unsupported type %s", field.Name, field.Type)
		}
		s.Fields[name] = field
	}
	for name, event := range s.Events {
		event.Name = strings.TrimSpace(event.Name)
		if event.Name == "" {
			event.Name = strings.TrimSpace(name)
		}
		if event.Name == "" {
			return fmt.Errorf("bilog event name is required")
		}
		for _, fieldName := range event.Fields {
			if _, ok := s.Fields[fieldName]; !ok {
				return fmt.Errorf("bilog event %s references unknown field %s", event.Name, fieldName)
			}
		}
		s.Events[name] = event
	}
	return nil
}

func (s *Schema) ValidateEvent(event Event) error {
	if s == nil {
		return nil
	}
	spec, ok := s.Events[event.Name]
	if !ok {
		return fmt.Errorf("unknown bilog event %s", event.Name)
	}
	fields := event.Fields
	for _, fieldName := range spec.Fields {
		fieldSpec := s.Fields[fieldName]
		value, exists := fields[fieldName]
		if fieldSpec.Required && !exists {
			return fmt.Errorf("bilog event %s missing required field %s", event.Name, fieldName)
		}
		if exists && !fieldTypeMatches(fieldSpec.Type, value) {
			return fmt.Errorf("bilog event %s field %s type mismatch", event.Name, fieldName)
		}
	}
	return nil
}

func fieldTypeMatches(fieldType FieldType, value any) bool {
	switch fieldType {
	case FieldString:
		_, ok := value.(string)
		return ok
	case FieldBool:
		_, ok := value.(bool)
		return ok
	case FieldNumber:
		switch value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			return true
		default:
			return false
		}
	default:
		return false
	}
}
