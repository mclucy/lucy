package curseforge

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
)

var (
	availabilityOnce sync.Once
	availabilityErr  error
)

func AvailabilityError() error {
	availabilityOnce.Do(
		func() {
			availabilityErr = validateAvailability()
		},
	)

	return availabilityErr
}

func Enabled() bool {
	return AvailabilityError() == nil
}

func validateAvailability() error {
	var resp struct {
		Data []struct{} `json:"data"`
	}

	err := get(baseUrl+"/v1/games", &resp)
	if err == nil {
		return nil
	}

	if errors.Is(err, ErrNoApiKey) {
		return err
	}

	if apiErr, ok := errors.AsType[ApiResponseError](err); ok {
		if apiErr.StatusCode == http.StatusBadRequest ||
			apiErr.StatusCode == http.StatusForbidden {
			return fmt.Errorf("%w: %w", ErrInvalidApiKey, err)
		}
	}

	return nil
}
