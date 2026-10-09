package newsbot

import (
	"errors"
	"time"
)

// jobRetryDelay is shared by persistence and logging; zero means terminal.
func jobRetryDelay(attempt int, err error) time.Duration {
	if err == nil || attempt >= 3 {
		return 0
	}
	var permanent interface{ Permanent() bool }
	if errors.As(err, &permanent) && permanent.Permanent() {
		return 0
	}
	var provider *ProviderHTTPError
	if errors.As(err, &provider) && (provider.HTTPStatus == 429 || provider.HTTPStatus >= 500) {
		if attempt < 1 {
			attempt = 1
		}
		return 2 * time.Minute * time.Duration(1<<(attempt-1))
	}
	return 5 * time.Second
}
