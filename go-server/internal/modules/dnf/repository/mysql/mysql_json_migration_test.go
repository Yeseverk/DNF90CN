package mysql

import (
	"reflect"
	"testing"
)

func TestDecodeLegacyItemExtraPreservesPrimitiveValues(t *testing.T) {
	got, err := decodeLegacyItemExtra(`{"text":"value","number":12.5,"enabled":true,"empty":null}`)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"text":    "value",
		"number":  "12.5",
		"enabled": "1",
		"empty":   "",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded extra = %#v, want %#v", got, want)
	}
}

func TestDecodeLegacyAuditPayloadFlattensNestedValues(t *testing.T) {
	got, err := decodeLegacyAuditPayload(`{"item":{"id":700,"flags":[true,false]},"note":"ok"}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []legacyAuditPayloadValue{
		{path: "/item/flags/0", valueType: "boolean", boolValue: true},
		{path: "/item/flags/1", valueType: "boolean", boolValue: false},
		{path: "/item/id", valueType: "number", numberValue: "700"},
		{path: "/note", valueType: "string", stringValue: "ok"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded payload = %#v, want %#v", got, want)
	}
}

func TestDecodeLegacyAuditPayloadEscapesJSONPointerTokens(t *testing.T) {
	got, err := decodeLegacyAuditPayload(`{"a/b":{"c~d":1}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].path != "/a~1b/c~0d" {
		t.Fatalf("decoded payload = %#v", got)
	}
}
