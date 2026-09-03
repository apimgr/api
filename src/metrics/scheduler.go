package metrics

import "time"

// RecordSchedulerTaskStart marks one scheduled task instance as started.
// Call RecordSchedulerTaskEnd when it completes.
func (m *Metrics) RecordSchedulerTaskStart(task string) {
	m.schedulerTasksRunning.WithLabelValues(task).Inc()
}

// RecordSchedulerTaskEnd marks one scheduled task instance as finished,
// recording its duration and outcome. err is the task's own result - nil
// means success, non-nil means failure; the raw error text is never used as
// a label value.
func (m *Metrics) RecordSchedulerTaskEnd(task string, duration time.Duration, err error) {
	m.schedulerTasksRunning.WithLabelValues(task).Dec()
	m.schedulerTaskDuration.WithLabelValues(task).Observe(duration.Seconds())
	m.schedulerLastRun.WithLabelValues(task).Set(float64(time.Now().Unix()))

	status := "success"
	if err != nil {
		status = "error"
	}
	m.schedulerTasksTotal.WithLabelValues(task, status).Inc()
}
