package a2asrv

import (
	"fmt"
	"net/url"
	"strconv"
	"time"
)

func parseIntOpt(query url.Values, key string) (*int, error) {
	val := query.Get(key)
	if val == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	return &v, nil
}

func parseTimeOpt(query url.Values, key string) (*time.Time, error) {
	val := query.Get(key)
	if val == "" {
		return nil, nil
	}
	parsedTime, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", key, err)
	}
	return &parsedTime, nil
}

func parseInt(query url.Values, key string) (int, error) {
	val := query.Get(key)
	if val == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}

func parseBool(query url.Values, key string) (bool, error) {
	val := query.Get(key)
	if val == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(val)
	if err != nil {
		return false, fmt.Errorf("invalid %s: %w", key, err)
	}
	return v, nil
}
