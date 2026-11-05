package storage

import (
	"fmt"
	"strconv"
	"time"
)

func durationMicroseconds(duration time.Duration) int64 {
	microseconds := int64(duration / time.Microsecond)
	if duration%time.Microsecond != 0 {
		microseconds++
	}
	if microseconds < 1 {
		return 1
	}
	return microseconds
}

func durationMilliseconds(duration time.Duration) int64 {
	milliseconds := int64(duration / time.Millisecond)
	if duration%time.Millisecond != 0 {
		milliseconds++
	}
	if milliseconds < 1 {
		return 1
	}
	return milliseconds
}

func redisInt64(value interface{}) (int64, error) {
	switch value := value.(type) {
	case int64:
		return value, nil
	case int:
		return int64(value), nil
	case string:
		return strconv.ParseInt(value, 10, 64)
	case []byte:
		return strconv.ParseInt(string(value), 10, 64)
	default:
		return 0, fmt.Errorf("unexpected Redis integer value: %T", value)
	}
}

func redisResultValues(result interface{}, length int) ([]interface{}, error) {
	values, ok := result.([]interface{})
	if !ok || len(values) != length {
		return nil, fmt.Errorf("unexpected Redis response: %T", result)
	}
	return values, nil
}

func redisDecisionValues(result interface{}, length int) (bool, []int64, error) {
	values, err := redisResultValues(result, length)
	if err != nil {
		return false, nil, err
	}

	allowed, err := redisInt64(values[0])
	if err != nil {
		return false, nil, err
	}
	metrics := make([]int64, length-1)
	for i := 1; i < length; i++ {
		metrics[i-1], err = redisInt64(values[i])
		if err != nil {
			return false, nil, err
		}
	}
	return allowed == 1, metrics, nil
}
