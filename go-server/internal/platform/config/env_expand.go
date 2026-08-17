package config

import (
	"os"
	"reflect"
	"regexp"
)

var configEnvPattern = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*\}`)

func expandEnvString(value string) string {
	return configEnvPattern.ReplaceAllStringFunc(value, func(token string) string {
		key := token[2 : len(token)-1]
		if value, ok := os.LookupEnv(key); ok {
			return value
		}
		return token
	})
}

func expandEnvValues(target any) {
	expandConfigEnvValue(reflect.ValueOf(target))
}

func expandConfigEnvValue(value reflect.Value) {
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		expandConfigEnvValue(value.Elem())
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for idx := 0; idx < value.NumField(); idx++ {
			field := value.Field(idx)
			if !field.CanSet() && field.Kind() != reflect.Struct {
				continue
			}
			expandConfigEnvValue(field)
		}
	case reflect.String:
		if value.CanSet() {
			value.SetString(expandEnvString(value.String()))
		}
	case reflect.Slice, reflect.Array:
		for idx := 0; idx < value.Len(); idx++ {
			expandConfigEnvValue(value.Index(idx))
		}
	case reflect.Map:
		if value.IsNil() || !value.CanSet() {
			return
		}
		replacement := reflect.MakeMapWithSize(value.Type(), value.Len())
		for _, key := range value.MapKeys() {
			outKey := key
			outValue := value.MapIndex(key)
			if key.Kind() == reflect.String {
				outKey = reflect.ValueOf(expandEnvString(key.String())).Convert(key.Type())
			}
			if outValue.Kind() == reflect.String {
				outValue = reflect.ValueOf(expandEnvString(outValue.String())).Convert(outValue.Type())
			}
			replacement.SetMapIndex(outKey, outValue)
		}
		value.Set(replacement)
	}
}
