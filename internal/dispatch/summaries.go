// Ported from go/internal/mcp/summaries.go and summaries_families.go at 9922b49.
// query.* value helpers are local ports (see values.go); the map-only
// answer-metadata branch is kept because dispatch only ever sees JSON-decoded
// maps. See docs/parity.md.

package dispatch

import (
	"fmt"
	"strings"
)

// Text summaries are a convenience layer for human readers only.

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
