package model

import (
	"reflect"
	"testing"
)

func TestStringListValueAndScanRoundTrip(t *testing.T) {
	want := StringList{"insert", "update", "delete"}
	value, err := want.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	var got StringList
	if err := got.Scan(value); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestStringListNilStoresEmptyJSONArray(t *testing.T) {
	value, err := (StringList)(nil).Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}
	if value != "[]" {
		t.Fatalf("Value() = %#v, want []", value)
	}
}
