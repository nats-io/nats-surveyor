package surveyor

import (
	"reflect"
	"testing"
)

func TestRemapIdToIdx(t *testing.T) {
	existingMapping := map[string]map[uint64]int{
		"a": {
			100: 0,
			200: 2,
		},
		"b": {
			100: 0,
		},
	}

	pairs := []nameIDPair{
		{name: "a", id: 200},
		{name: "a", id: 100},
		{name: "a", id: 300},
		{name: "a", id: 400},
		{name: "b", id: 200},
		{name: "c", id: 200},
		{name: "c", id: 100},
	}

	newMapping := remapIdToIdx(pairs, existingMapping)
	expected := map[string]map[uint64]int{
		"a": {
			100: 0,
			200: 2,
			300: 1,
			400: 3,
		},
		"b": {
			200: 0,
		},
		"c": {
			200: 0,
			100: 1,
		},
	}

	if !reflect.DeepEqual(expected, newMapping) {
		t.Fatalf("Invalid mapping config; want: %v; got: %v", expected, newMapping)
	}
}
