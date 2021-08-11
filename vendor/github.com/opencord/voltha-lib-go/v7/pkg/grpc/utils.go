package grpc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"sync"
	"time"
)

const (
	incrementalFactor = 1.5
	minBackOff        = 10 * time.Millisecond
)

type Backoff struct {
	attempt          int
	initialInterval  time.Duration
	maxElapsedTime   time.Duration
	maxInterval      time.Duration
	totalElapsedTime time.Duration
	mutex            sync.RWMutex
}

func NewBackoff(initialInterval, maxInterval, maxElapsedTime time.Duration) *Backoff {
	bo := &Backoff{}
	bo.initialInterval = initialInterval
	bo.maxInterval = maxInterval
	bo.maxElapsedTime = maxElapsedTime
	return bo
}

func (bo *Backoff) Backoff(ctx context.Context) error {
	duration, err := bo.getBackOffDuration()
	if err != nil {
		return err
	}

	ticker := time.NewTicker(duration)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticker.C:
	}
	return nil
}

func (bo *Backoff) getBackOffDuration() (duration time.Duration, err error) {
	err = nil
	defer func() {
		bo.mutex.Lock()
		defer bo.mutex.Unlock()
		bo.attempt += 1
		bo.totalElapsedTime += duration
		if bo.maxElapsedTime > 0 && bo.totalElapsedTime > bo.maxElapsedTime {
			err = errors.New("max elapsed backoff time reached")
		}
	}()

	if bo.initialInterval <= minBackOff {
		bo.initialInterval = minBackOff
	}
	if bo.initialInterval > bo.maxInterval {
		duration = bo.initialInterval
		return
	}

	// Calculate incremental duration
	minf := float64(bo.initialInterval)
	durf := minf * math.Pow(incrementalFactor, float64(bo.attempt))

	if durf > math.MaxInt64 {
		duration = bo.maxInterval
		return
	}
	duration = time.Duration(durf)

	//Keep within bounds
	if duration < bo.initialInterval {
		duration = bo.initialInterval
	}
	if duration > bo.maxInterval {
		duration = bo.maxInterval
	}
	return
}

func (bo *Backoff) Reset() {
	bo.mutex.Lock()
	defer bo.mutex.Unlock()
	bo.attempt = 0
	bo.totalElapsedTime = 0
}

func SetFromEnvVariable(key string, variableToSet interface{}) error {
	if _, ok := variableToSet.(*time.Duration); !ok {
		return fmt.Errorf("unsupported type %T", variableToSet)
	}
	if valStr, present := os.LookupEnv(key); present {
		val, err := time.ParseDuration(valStr)
		if err != nil {
			return err
		}
		reflect.ValueOf(variableToSet).Elem().Set(reflect.ValueOf(val))
	}
	return nil
}
