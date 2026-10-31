package router

import (
	"testing"
)

func TestErrorSinkTruncates(t *testing.T) {
	t.Parallel()

	var sink errorSink
	for i := 0; i < MaxFieldErrors+5; i++ {
		sink.add(FieldError{Path: "b", Keyword: "z"})
		sink.add(FieldError{Path: "a", Keyword: "a"})
	}
	errs, truncated := sink.result()
	if !truncated {
		t.Fatal("overflow not reported")
	}
	if len(errs) != MaxFieldErrors {
		t.Fatalf("len = %d, want %d", len(errs), MaxFieldErrors)
	}
	for i := 1; i < len(errs); i++ {
		if errs[i-1].Path > errs[i].Path {
			t.Fatal("errors not sorted by path")
		}
	}
	var exact errorSink
	for i := 0; i < MaxFieldErrors; i++ {
		exact.add(FieldError{Path: "p"})
	}
	if _, truncated := exact.result(); truncated {
		t.Fatal("exact fit reported as truncated")
	}
}
