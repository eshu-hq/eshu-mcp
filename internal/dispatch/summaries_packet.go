// Ported from go/internal/mcp/summaries.go and summaries_families.go at 9922b49.
// query.* value helpers are local ports (see values.go); the map-only
// answer-metadata branch is kept because dispatch only ever sees JSON-decoded
// maps. See docs/parity.md.

package dispatch

import (
	"fmt"
	"strings"
)

// Packet and field helpers shared by the visualization, citation, and
// status summarizers.

func summarizeVisualizationPacket(data map[string]any) string {
	packet := mcpMapField(data, "visualization_packet")
	if packet == nil {
		return ""
	}
	view := strings.TrimSpace(stringVal(packet, "view"))
	nodes := mcpSliceLen(packet, "nodes")
	edges := mcpSliceLen(packet, "edges")
	supported, ok := packet["supported"].(bool)
	if !ok {
		supported = false
	}
	if !supported {
		limitation := firstStringField(packet, "limitations")
		if limitation != "" {
			return fmt.Sprintf("visualization %s unsupported: %s", view, clampField(limitation))
		}
		return fmt.Sprintf("visualization %s unsupported", view)
	}
	return fmt.Sprintf("visualization %s: %d node(s), %d edge(s)", view, nodes, edges)
}

func firstStringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch values := m[key].(type) {
	case []any:
		if len(values) == 0 {
			return ""
		}
		value, ok := values[0].(string)
		if !ok {
			return ""
		}
		return strings.TrimSpace(value)
	case []string:
		if len(values) == 0 {
			return ""
		}
		return strings.TrimSpace(values[0])
	default:
		return ""
	}
}

// mcpMapField extracts a nested map[string]any from a JSON-parsed map, returning
// nil when the key is absent or not a map.
func mcpMapField(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	if nested, ok := m[key].(map[string]any); ok {
		return nested
	}
	return nil
}

// mcpSliceLen returns the length of a JSON-parsed slice value, tolerating both
// []any and []map[string]any. Non-slice values report length 0.
func mcpSliceLen(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	switch typed := m[key].(type) {
	case []any:
		return len(typed)
	case []map[string]any:
		return len(typed)
	default:
		return 0
	}
}

func mcpAnswerMetadata(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	// The source also accepts a typed answer-metadata struct; dispatch only
	// ever sees JSON-decoded maps, so the map branch is the whole port.
	metadata, ok := data["answer_metadata"].(map[string]any)
	if !ok {
		return nil
	}
	return metadata
}

func mcpMetadataTruncated(metadata map[string]any) bool {
	return boolVal(metadata, "truncated")
}

func mcpMetadataRowsLen(metadata map[string]any, key string) int {
	if metadata == nil {
		return 0
	}
	return mcpValueSliceLen(metadata[key])
}

func mcpFirstMetadataReason(metadata map[string]any, key string) (string, int) {
	rows := mcpMetadataRows(metadata, key)
	for _, row := range rows {
		for _, reasonKey := range []string{"reason", "kind", "slot"} {
			if reason := stringVal(row, reasonKey); reason != "" {
				return reason, len(rows)
			}
		}
	}
	return "", len(rows)
}

func mcpMetadataRows(metadata map[string]any, key string) []map[string]any {
	if metadata == nil {
		return nil
	}
	switch typed := metadata[key].(type) {
	case []map[string]any:
		return typed
	case []any:
		rows := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				rows = append(rows, row)
			}
		}
		return rows
	default:
		return nil
	}
}

func mcpValueSliceLen(value any) int {
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case []map[string]any:
		return len(typed)
	default:
		return 0
	}
}

// mcpFirstStringInSlice returns the named string field of the first row in a
// slice value, or "" when the slice is empty or the field is absent. It is used
// to surface the single recommended next call deterministically.
func mcpFirstStringInSlice(m map[string]any, sliceKey, field string) string {
	if m == nil {
		return ""
	}
	switch typed := m[sliceKey].(type) {
	case []any:
		if len(typed) == 0 {
			return ""
		}
		if row, ok := typed[0].(map[string]any); ok {
			return stringVal(row, field)
		}
	case []map[string]any:
		if len(typed) == 0 {
			return ""
		}
		return stringVal(typed[0], field)
	}
	return ""
}

// summarizeServiceStory renders a bounded service-story dossier summary:
// service name, API surface size + truncation, dependency/consumer counts, and
// the top limitation. It reads only from the already-parsed dossier data.
