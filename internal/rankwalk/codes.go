package rankwalk

// WindowSize, tool codes, and lane codes, ported from
// go/internal/query/search/rank/walk.go at 9922b49: the cursor codec and
// TrimWalk prove cursor identity against these, so they must match core.
const WindowSize = 200

var toolCodes = map[string]byte{
	"find_code":             1,
	"search_entity_content": 2,
}

var laneCodes = map[string]byte{
	"graph":   1,
	"content": 2,
}
