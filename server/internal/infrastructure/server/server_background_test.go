package server

import (
	"context"
	"testing"
	"time"

	"github.com/jherrma/caldav-server/internal/config"
	"github.com/jherrma/caldav-server/internal/infrastructure/database"
	subscriptionusecase "github.com/jherrma/caldav-server/internal/usecase/subscription"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newBackgroundTestServer(t *testing.T, alarms config.AlarmConfig, subs config.SubscriptionConfig) *Server {
	t.Helper()
	cfg := &config.Config{
		DataDir:       t.TempDir(),
		Database:      config.DatabaseConfig{Driver: "sqlite", Name: "bg"},
		Alarms:        alarms,
		Subscriptions: subs,
	}
	db, err := database.New(cfg)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(database.Models()...))
	t.Cleanup(func() { _ = db.Close() })
	return New(cfg, db)
}

func TestBackgroundServicesWiring(t *testing.T) {
	on := config.AlarmConfig{CleanupInterval: time.Hour, AckRetention: 720 * time.Hour}

	t.Run("interval set builds the job", func(t *testing.T) {
		s := newBackgroundTestServer(t, on, config.SubscriptionConfig{})
		assert.NotNil(t, s.services.AlarmAckCleanup)
		assert.Nil(t, s.services.SubscriptionWorker)
	})
	t.Run("interval 0 builds no job", func(t *testing.T) {
		a := on
		a.CleanupInterval = 0
		s := newBackgroundTestServer(t, a, config.SubscriptionConfig{})
		assert.Nil(t, s.services.AlarmAckCleanup)
	})
}

func TestStartBackgroundRunsCleanupWithSubscriptionsOff(t *testing.T) {
	s := newBackgroundTestServer(t,
		config.AlarmConfig{CleanupInterval: time.Hour, AckRetention: 720 * time.Hour},
		config.SubscriptionConfig{Enabled: false})
	require.Nil(t, s.services.SubscriptionWorker)
	require.NotNil(t, s.services.AlarmAckCleanup)

	s.startBackground()
	require.NotNil(t, s.stopBackground, "the cleanup job alone must start the background context")
	require.NotNil(t, s.bgDone)

	select {
	case <-s.bgDone:
		t.Fatal("the job must keep running until stopped")
	case <-time.After(50 * time.Millisecond):
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.stopBackgroundAndWait(ctx)
	select {
	case <-s.bgDone:
	default:
		t.Fatal("stopBackgroundAndWait returned before the job did")
	}
}

func TestStartBackgroundWaitsForEveryJob(t *testing.T) {
	// Both jobs: the subscription worker (interval > 0) and the cleanup job.
	s := newBackgroundTestServer(t,
		config.AlarmConfig{CleanupInterval: time.Hour, AckRetention: 720 * time.Hour},
		config.SubscriptionConfig{Enabled: true, WorkerInterval: time.Hour, MaxPerUser: 20})
	if s.services.SubscriptionWorker == nil {
		s.services.SubscriptionWorker = subscriptionusecase.NewWorker(nil, nil, time.Hour, nil)
	}

	s.startBackground()
	require.NotNil(t, s.bgDone)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.stopBackgroundAndWait(ctx)
	select {
	case <-s.bgDone:
	default:
		t.Fatal("shutdown did not wait for both jobs")
	}
}

func TestStartBackgroundNothingToStart(t *testing.T) {
	s := newBackgroundTestServer(t,
		config.AlarmConfig{CleanupInterval: 0},
		config.SubscriptionConfig{Enabled: false})
	s.startBackground()
	assert.Nil(t, s.stopBackground, "no goroutine and no context when both jobs are off")
	assert.Nil(t, s.bgDone)
	s.stopBackgroundAndWait(context.Background()) // must be a no-op
}
