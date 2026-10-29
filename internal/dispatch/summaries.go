// Ported from go/internal/mcp/summaries.go and summaries_families.go at 9922b49.
// query.* value helpers are local ports (see values.go); the map-only
// answer-metadata branch is kept because dispatch only ever sees JSON-decoded
// maps. See docs/parity.md.

package dispatch

import (
	"fmt"
	"strings"
)

// Text summaries are a convenience layer for human readers only. The structured
// content and the embedded resource block remain the canonical source of truth.
// Every summary built here MUST be deterministic given its input and MUST stay
// within maxSummaryLength so that a rich result never expands into an unbounded
// string.

// maxSummaryLength caps every generated text summary. Summaries are a human
// convenience, not a transport, so they are clamped rather than allowed to grow
// with the result payload.
const maxSummaryLength = 600

// maxLimitationLength caps a single embedded limitation/reason fragment so one
// oversized field cannot dominate or blow the summary budget.
const maxLimitationLength = 120

// summarizeToolText returns a deterministic, bounded, human-readable summary for
// an envelope-backed tool response. It reads only from the already-parsed
// envelope (no new queries) and never mutates the structured content. Tools
// without a specific summarizer fall back to the generic envelope summary.
func summarizeToolText(toolName string, envelope *ResponseEnvelope) string {
	if envelope == nil {
		return "Eshu query completed."
	}
	if envelope.Error != nil {
		return clampSummary(summarizeEnvelopeError(envelope.Error))
	}

	data, ok := envelope.Data.(map[string]any)
	if !ok {
		data = nil
	}

	var body string
	switch toolName {
	case "get_service_story", "get_workload_story", "get_repo_story":
		body = summarizeServiceStory(data)
	case "investigate_service":
		body = summarizeServiceInvestigation(data)
	case "get_incident_context":
		body = summarizeIncidentContext(data)
	case "build_evidence_citation_packet":
		body = summarizeCitationPacket(data)
	case "derive_visualization_packet":
		body = summarizeVisualizationPacket(data)
	case "analyze_code_relationships":
		body = summarizeCodeRelationships(data)
	default:
		body = ""
	}

	prefix := summarizeTruthPrefix(envelope.Truth)
	if body == "" {
		// No tool-specific summarizer matched. Preserve the generic envelope
		// summary so unhandled tools still get a reasonable, bounded message,
		// while still surfacing truth + freshness when present.
		generic := summarizeEnvelope(envelope)
		return clampSummary(joinSummary(prefix, generic))
	}
	return clampSummary(joinSummary(prefix, body))
}

// summarizePlainToolText returns a deterministic, bounded summary for a tool
// whose response is a plain JSON payload rather than a canonical envelope (for
// example the status/readiness endpoints). Unhandled tools fall back to the
// generic plain-payload summary.
func summarizePlainToolText(toolName string, value any) string {
	data, ok := value.(map[string]any)
	if !ok {
		data = nil
	}

	var body string
	switch toolName {
	case "get_index_status":
		body = summarizeIndexStatus(data)
	case "get_hosted_readiness":
		body = summarizeHostedReadiness(data)
	case "get_ingester_status":
		body = summarizeIngesterStatus(data)
	case "list_ingesters":
		body = summarizeIngesterList(data)
	case "list_collectors":
		body = summarizeCollectorList(data)
	default:
		body = ""
	}

	if body == "" {
		return clampSummary(summarizePlainPayload(value))
	}
	return clampSummary(body)
}

// summarizeEnvelopeError renders an error envelope as truth, surfacing the
// machine-readable code alongside the human message so partial/failed responses
// never collapse into generic success text.
func summarizeEnvelopeError(errEnv *ErrorEnvelope) string {
	message := strings.TrimSpace(errEnv.Message)
	code := strings.TrimSpace(string(errEnv.Code))
	switch {
	case code != "" && message != "":
		return fmt.Sprintf("error %s: %s", code, message)
	case code != "":
		return fmt.Sprintf("error %s", code)
	case message != "":
		return message
	default:
		return "Eshu query returned an error."
	}
}

// summarizeTruthPrefix renders the truth level and freshness state (plus a
// bounded freshness cause or detail when the state is not fresh) so every
// summary leads with how much to trust the result and WHY it lags. Returns ""
// when no truth is present. The cause and detail are convenience text only; the
// structured envelope freshness object remains canonical.
func summarizeTruthPrefix(truth *TruthEnvelope) string {
	if truth == nil {
		return ""
	}
	level := strings.TrimSpace(string(truth.Level))
	state := strings.TrimSpace(string(truth.Freshness.State))
	switch {
	case level != "" && state != "":
		prefix := fmt.Sprintf("%s/%s", level, state)
		if state != string(freshnessFresh) {
			if note := freshnessNote(truth.Freshness); note != "" {
				prefix += " (" + clampField(note) + ")"
			}
		}
		return prefix
	case level != "":
		return level
	case state != "":
		return state
	default:
		return ""
	}
}

// freshnessNote renders the convenience explanation for a non-fresh freshness:
// the proven cause when present (it is the most actionable signal), otherwise
// the bounded detail. It reads only the already-parsed freshness and never
// mutates structured content.
func freshnessNote(freshness TruthFreshness) string {
	cause := strings.TrimSpace(string(freshness.Cause))
	detail := strings.TrimSpace(freshness.Detail)
	switch {
	case cause != "" && detail != "":
		return "cause: " + cause + "; " + detail
	case cause != "":
		return "cause: " + cause
	default:
		return detail
	}
}

// joinSummary combines a truth prefix and a body with a deterministic separator,
// tolerating an empty prefix or body.
func joinSummary(prefix, body string) string {
	prefix = strings.TrimSpace(prefix)
	body = strings.TrimSpace(body)
	switch {
	case prefix == "" && body == "":
		return "Eshu query completed."
	case prefix == "":
		return body
	case body == "":
		return prefix
	default:
		return prefix + " — " + body
	}
}

// clampSummary bounds a full summary to maxSummaryLength, appending an ellipsis
// marker when truncation occurs so the reader knows the text was clipped.
func clampSummary(s string) string {
	if len(s) <= maxSummaryLength {
		return s
	}
	if maxSummaryLength <= 3 {
		return s[:maxSummaryLength]
	}
	return s[:maxSummaryLength-3] + "..."
}

// clampField bounds a single embedded fragment (a limitation, reason, or title)
// so one oversized field cannot dominate the summary.
func clampField(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if len(s) <= maxLimitationLength {
		return s
	}
	if maxLimitationLength <= 3 {
		return s[:maxLimitationLength]
	}
	return s[:maxLimitationLength-3] + "..."
}

// summarizeEnvelope is the generic, tool-agnostic envelope fallback used when no
// tool-specific summarizer matches. It surfaces an error message or a result
// count, defaulting to a neutral completion message.
func summarizeEnvelope(envelope *ResponseEnvelope) string {
	if envelope == nil {
		return "Eshu query completed."
	}
	if envelope.Error != nil {
		return summarizeEnvelopeError(envelope.Error)
	}
	if dataMap, ok := envelope.Data.(map[string]any); ok {
		if count, ok := dataMap["count"]; ok {
			return fmt.Sprintf("Returned %v result(s).", count)
		}
		if count, ok := dataMap["affected_count"]; ok {
			return fmt.Sprintf("Found %v affected result(s).", count)
		}
	}
	return "Eshu query completed."
}

// summarizePlainPayload is the generic, tool-agnostic fallback for plain JSON
// payloads. It reports an array length or a count-like field, defaulting to a
// neutral completion message.
func summarizePlainPayload(value any) string {
	switch typed := value.(type) {
	case []any:
		return fmt.Sprintf("Returned %d result(s).", len(typed))
	case map[string]any:
		for _, key := range []string{"count", "total", "total_findings", "total_reconciliations", "total_correlations", "total_identities", "total_attachments"} {
			if count, ok := typed[key]; ok {
				return fmt.Sprintf("Returned %v result(s).", count)
			}
		}
	}
	return "Eshu query completed."
}

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
func summarizeServiceStory(data map[string]any) string {
	if data == nil {
		return ""
	}
	identity := mcpMapField(data, "service_identity")
	apiSurface := mcpMapField(data, "api_surface")
	limits := mcpMapField(data, "result_limits")
	metadata := mcpAnswerMetadata(data)

	name := stringVal(identity, "service_name")
	if name == "" {
		name = stringVal(identity, "service_id")
	}
	if name == "" {
		name = "service"
	}

	parts := []string{clampField(name)}

	endpointCount := intVal(apiSurface, "endpoint_count")
	apiPart := fmt.Sprintf("API surface %d", endpointCount)
	if boolVal(apiSurface, "truncated") || mcpMetadataTruncated(metadata) {
		apiPart += " (truncated)"
	}
	parts = append(parts, apiPart)

	parts = append(parts, fmt.Sprintf("deps %d", intVal(limits, "upstream_count")))
	parts = append(parts, fmt.Sprintf("consumers %d", intVal(limits, "downstream_count")))

	summary := strings.Join(parts, "; ")

	if limitation, count := mcpFirstMetadataReason(metadata, "limitations"); limitation != "" {
		summary += "; top limitation: " + clampField(limitation)
		if count > 1 {
			summary += fmt.Sprintf(" (+%d more)", count-1)
		}
	} else if limitations := stringSliceVal(identity, "limitations"); len(limitations) > 0 {
		summary += "; top limitation: " + clampField(limitations[0])
		if len(limitations) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(limitations)-1)
		}
	}
	return summary
}

// summarizeServiceInvestigation renders a bounded investigation summary: finding
// count + truncation, coverage state, and the single recommended next call.
func summarizeServiceInvestigation(data map[string]any) string {
	if data == nil {
		return ""
	}
	name := stringVal(data, "service_name")
	if name == "" {
		name = "service"
	}
	findingCount := mcpSliceLen(data, "investigation_findings")
	coverage := mcpMapField(data, "coverage_summary")

	summary := fmt.Sprintf("%s — %d finding(s)", clampField(name), findingCount)
	if boolVal(coverage, "truncated") {
		summary += " (truncated)"
	}
	if state := stringVal(coverage, "state"); state != "" {
		summary += "; coverage " + state
	}
	if next := mcpFirstStringInSlice(data, "recommended_next_calls", "tool"); next != "" {
		summary += "; next: " + clampField(next)
	}
	return summary
}

// summarizeIncidentContext renders a bounded incident-context summary: incident
// title, related-change count, missing/ambiguous evidence counts, and the
// truncation marker. Missing and ambiguous counts are surfaced so a partial
// incident packet never reads as a clean success.
func summarizeIncidentContext(data map[string]any) string {
	if data == nil {
		return ""
	}
	incident := mcpMapField(data, "incident")
	title := stringVal(incident, "title")
	if title == "" {
		title = stringVal(incident, "provider_incident_id")
	}
	if title == "" {
		title = "incident"
	}

	relatedChanges := mcpSliceLen(data, "related_changes")
	missing := mcpSliceLen(data, "missing_evidence")
	ambiguous := mcpSliceLen(data, "ambiguous_evidence")
	metadata := mcpAnswerMetadata(data)
	if metadataMissing := mcpMetadataRowsLen(metadata, "missing_evidence"); metadataMissing > 0 {
		missing = metadataMissing
	}

	summary := fmt.Sprintf("%s — %d related change(s)", clampField(title), relatedChanges)
	summary += fmt.Sprintf("; missing evidence %d", missing)
	summary += fmt.Sprintf("; ambiguous %d", ambiguous)
	if boolVal(data, "truncated") || mcpMetadataTruncated(metadata) {
		summary += "; truncated"
	}
	return summary
}

// summarizeCitationPacket renders a bounded evidence-citation summary: resolved
// vs requested handle counts, missing count, and the truncation marker.
func summarizeCitationPacket(data map[string]any) string {
	if data == nil {
		return ""
	}
	coverage := mcpMapField(data, "coverage")
	if coverage == nil {
		return ""
	}
	resolved := intVal(coverage, "resolved_count")
	missing := intVal(coverage, "missing_count")
	requested := intVal(coverage, "input_handle_count")

	summary := fmt.Sprintf("coverage: resolved %d/requested %d; missing %d", resolved, requested, missing)
	if boolVal(coverage, "truncated") {
		summary += " (truncated)"
	}
	return summary
}

// summarizeIndexStatus renders an actionable index/readiness summary: the health
// state and the leading reason, rather than an opaque object count.
func summarizeIndexStatus(data map[string]any) string {
	if data == nil {
		return ""
	}
	state := stringVal(data, "status")
	if state == "" {
		if boolVal(data, "scoped") {
			return summarizeScopedIndexStatus(data)
		}
		return ""
	}
	summary := "index status: " + clampField(state)
	if reasons := stringSliceVal(data, "reasons"); len(reasons) > 0 {
		summary += " — " + clampField(reasons[0])
		if len(reasons) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(reasons)-1)
		}
	}
	return summary
}

// summarizeScopedIndexStatus renders the scoped index-status shape (#5167): a
// grant-bound repository count and the disclosure that the deployment-wide
// sections were withheld, so a caller does not read the absent status as an
// unknown health.
func summarizeScopedIndexStatus(data map[string]any) string {
	count := intVal(data, "repository_count")
	noun := "repositories"
	if count == 1 {
		noun = "repository"
	}
	return fmt.Sprintf(
		"index status (scoped): %d %s in your grant; deployment-wide health, queue, and coordinator sections are withheld",
		count, noun,
	)
}

// summarizeHostedReadiness renders the hosted readiness result and first
// blocking class so MCP clients do not need to inspect the full check list.
func summarizeHostedReadiness(data map[string]any) string {
	if data == nil {
		return ""
	}
	state := stringVal(data, "state")
	if state == "" {
		return ""
	}
	summary := "hosted readiness: " + clampField(state)
	if failures := stringSliceVal(data, "failure_classes"); len(failures) > 0 {
		summary += " — " + clampField(failures[0])
		if len(failures) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(failures)-1)
		}
	}
	return summary
}

// summarizeIngesterStatus renders an actionable ingester readiness summary: the
// ingester name, its health state, and the leading health reason.
func summarizeIngesterStatus(data map[string]any) string {
	if data == nil {
		return ""
	}
	name := stringVal(data, "ingester")
	if name == "" {
		return ""
	}
	health := mcpMapField(data, "health")
	state := stringVal(health, "state")
	if state == "" {
		state = "unknown"
	}
	summary := fmt.Sprintf("ingester %s: %s", clampField(name), clampField(state))
	if reasons := stringSliceVal(health, "reasons"); len(reasons) > 0 {
		summary += " — " + clampField(reasons[0])
		if len(reasons) > 1 {
			summary += fmt.Sprintf(" (+%d more)", len(reasons)-1)
		}
	}
	return summary
}

// summarizeIngesterList renders a bounded ingester-list summary: how many
// ingesters are registered.
func summarizeIngesterList(data map[string]any) string {
	if data == nil {
		return ""
	}
	if _, ok := data["ingesters"]; !ok {
		return ""
	}
	return fmt.Sprintf("%d ingester(s) registered", mcpSliceLen(data, "ingesters"))
}

// summarizeCollectorList renders a bounded collector-list summary: how many
// collectors are registered.
func summarizeCollectorList(data map[string]any) string {
	if data == nil {
		return ""
	}
	if _, ok := data["collectors"]; !ok {
		return ""
	}
	return fmt.Sprintf("%d collector(s) registered", mcpSliceLen(data, "collectors"))
}

// summarizeCodeRelationships renders a bounded relationship-story summary that
// makes code-relationship uncertainty explicit (issue #3158): how many edges are
// derived (canonical code truth) versus heuristic/ambiguous or unsupported, and
// why the result is empty or short. It reads the structured content only and
// never reinterprets confidence or truth — it surfaces what the answer already
// labels per edge so a reader sees the ambiguity instead of trusting raw counts.
func summarizeCodeRelationships(data map[string]any) string {
	if data == nil {
		return ""
	}
	relationships, ok := data["relationships"].([]any)
	if !ok {
		return ""
	}
	var derived, heuristic, unsupported int
	for _, raw := range relationships {
		row, ok := raw.(map[string]any)
		if !ok {
			row = nil
		}
		provenance := mcpMapField(row, "provenance")
		switch stringVal(provenance, "truth_state") {
		case "derived":
			derived++
		case "heuristic":
			heuristic++
		default:
			unsupported++
		}
	}

	parts := []string{fmt.Sprintf("%d relationship(s): %d derived, %d heuristic/ambiguous, %d unsupported",
		len(relationships), derived, heuristic, unsupported)}
	if heuristic > 0 {
		parts = append(parts, "heuristic edges are correlation evidence, not canonical code truth")
	}
	if unsupported > 0 {
		parts = append(parts, "unsupported edges carry no recorded confidence")
	}

	coverage := mcpMapField(data, "coverage")
	if explanation := stringVal(coverage, "evidence_explanation"); explanation != "" {
		reason := stringVal(coverage, "missing_edge_reason")
		if reason != "" && reason != "complete" {
			parts = append(parts, fmt.Sprintf("%s: %s", reason, explanation))
		}
	}
	return strings.Join(parts, "; ")
}
