package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaintenanceConfigDefaultsAndNormalization(t *testing.T) {
	defaults := defaultConfig().Server.Maintenance
	assert.Equal(t, "30s", defaults.SelfHealing.RetryInterval)
	assert.Zero(t, defaults.SelfHealing.MaxAttempts)
	assert.Equal(t, 90, defaults.Cleanup.DiskThreshold)
	assert.Equal(t, 7, defaults.Cleanup.LogRetentionDays)
	assert.True(t, defaults.Notify.OnEnter)
	assert.True(t, defaults.Notify.OnExit)

	configured, warnings := (MaintenanceConfig{
		SelfHealing: MaintenanceSelfHealingConfig{RetryInterval: "bad", MaxAttempts: -1},
		Cleanup:     MaintenanceCleanupConfig{DiskThreshold: 101, LogRetentionDays: 0},
	}).Normalized()
	assert.Equal(t, "30s", configured.SelfHealing.RetryInterval)
	assert.Zero(t, configured.SelfHealing.MaxAttempts)
	assert.Equal(t, 90, configured.Cleanup.DiskThreshold)
	assert.Equal(t, 7, configured.Cleanup.LogRetentionDays)
	assert.Len(t, warnings, 4)
}
