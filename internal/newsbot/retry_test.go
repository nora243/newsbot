package newsbot

import (
	"fmt"
	"testing"
	"time"
)

func TestJobRetryDelay(t *testing.T) {
	for _, status := range []int{429, 500, 502, 503, 504} {
		err := fmt.Errorf("generate: %w", &ProviderHTTPError{HTTPStatus: status})
		for attempt, want := range map[int]time.Duration{1: 2 * time.Minute, 2: 4 * time.Minute, 3: 0} {
			if got := jobRetryDelay(attempt, err); got != want {
				t.Fatalf("status=%d attempt=%d got=%v want=%v", status, attempt, got, want)
			}
		}
	}
	for _, err := range []error{nil, &ProviderHTTPError{HTTPStatus: 400}, &InsufficientSourceError{}, &FactCheckError{message: "unsupported"}} {
		if jobRetryDelay(1, err) != 0 {
			t.Fatalf("terminal error retried: %v", err)
		}
	}
	if jobRetryDelay(1, fmt.Errorf("temporary")) != 5*time.Second {
		t.Fatal("ordinary retry changed")
	}
}
