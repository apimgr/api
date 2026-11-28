package app

import (
	"fmt"
	"net"
	"strings"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
	"github.com/apimgr/api/src/scheduler"
	"github.com/apimgr/api/src/server"
	"github.com/apimgr/api/src/server/handler"
)

func dumpRuntimeStatus(cfg *config.ServerConfig, sched *scheduler.Scheduler, listeners ...net.Listener) {
	logger := server.GetLogger()
	if logger == nil {
		return
	}
	addresses := make([]string, 0, len(listeners))
	for _, listener := range listeners {
		if listener != nil {
			addresses = append(addresses, listener.Addr().String())
		}
	}
	schedulerRunning := sched != nil && sched.Running()
	logger.LogServer("info", fmt.Sprintf(
		"Status dump: version=%s mode=%s uptime_seconds=%d database_connected=%t scheduler_running=%t listeners=%s",
		Version, cfg.Server.Mode, handler.UptimeSeconds(), database.GetServerDB() != nil, schedulerRunning, strings.Join(addresses, ","),
	))
}
