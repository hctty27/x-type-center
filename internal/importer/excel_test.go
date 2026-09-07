package importer

import "testing"

func TestParseValue(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		kind    valueKind
		value   int64
		start   int64
		end     int64
		project string
	}{
		{name: "number", raw: " 447 ", kind: valueNumber, value: 447},
		{name: "reserved", raw: "15500-15800 SLG预留", kind: valueReservedRange, start: 15500, end: 15800, project: "SLG"},
		{name: "blocked range", raw: "1475-1514", kind: valueReservedRange, start: 1475, end: 1514},
		{name: "unknown", raw: "todo", kind: valueUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseValue(tt.raw)
			if got.Kind != tt.kind || got.Value != tt.value || got.Start != tt.start || got.End != tt.end || got.Project != tt.project {
				t.Fatalf("parseValue(%q) = %+v", tt.raw, got)
			}
		})
	}
}
