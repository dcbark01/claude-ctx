package cmd

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/dcbark01/claude-ctx/internal/gateway"
)

func TestRefreshWarningsSkipsNoModelInfo(t *testing.T) {
	err := errors.Join(
		fmt.Errorf("model discovery: %w", errors.New("HTTP 500")),
		fmt.Errorf("model limits: %w", gateway.ErrNoModelInfo),
	)
	got := refreshWarnings("a", err)
	if !reflect.DeepEqual(got, []string{"warning: a: model discovery: HTTP 500"}) {
		t.Errorf("got %q", got)
	}
	if got := refreshWarnings("a", fmt.Errorf("model limits: %w", gateway.ErrNoModelInfo)); len(got) != 0 {
		t.Errorf("expected-absence warned: %q", got)
	}
	if got := refreshWarnings("a", nil); len(got) != 0 {
		t.Errorf("nil: %q", got)
	}
}
