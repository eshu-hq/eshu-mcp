package rankwalk

// ReasonMalformed: the token failed to decode or its checksum failed.
// Ported from go/internal/query/search/rank/errors.go at 9922b49.
const ReasonMalformed = "malformed"

// walkError is the local cursor-codec failure. TrimWalk and Advance only use
// it as a refused-page signal: trimToBudgetPage keeps the over-budget error
// envelope when they report false, so the message never crosses the wire in
// this package. The code, reason, and message match the source values, and
// the text keeps the source "<code>: <reason>: <message>" shape.
type walkError struct {
	reason  string
	message string
}

func (e *walkError) Error() string {
	return "rank_cursor_invalid: " + e.reason + ": " + e.message
}

func invalid(reason, message string) *walkError {
	return &walkError{reason: reason, message: message}
}
