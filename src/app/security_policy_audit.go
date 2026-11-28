package app

import (
	"context"
	"log"
	"sort"

	"github.com/apimgr/api/src/config"
	"github.com/apimgr/api/src/database"
)

type securityPolicyAuditEvent struct {
	name   string
	detail map[string]any
}

func securityPolicyChanges(previous, current *config.ServerConfig) []securityPolicyAuditEvent {
	if previous == nil || current == nil {
		return nil
	}
	events := make([]securityPolicyAuditEvent, 0)
	previousAllowlist := make(map[string]config.AllowlistEntry, len(previous.Server.Security.Allowlist))
	currentAllowlist := make(map[string]config.AllowlistEntry, len(current.Server.Security.Allowlist))
	for _, entry := range previous.Server.Security.Allowlist {
		previousAllowlist[entry.CIDR] = entry
	}
	for _, entry := range current.Server.Security.Allowlist {
		currentAllowlist[entry.CIDR] = entry
	}
	for _, cidr := range sortedPolicyKeys(currentAllowlist) {
		entry := currentAllowlist[cidr]
		if _, exists := previousAllowlist[cidr]; !exists {
			events = append(events, securityPolicyAuditEvent{name: "security.ip_allowlisted", detail: map[string]any{"cidr": cidr, "description": entry.Description}})
		}
	}
	for _, cidr := range sortedPolicyKeys(previousAllowlist) {
		if _, exists := currentAllowlist[cidr]; !exists {
			events = append(events, securityPolicyAuditEvent{name: "security.ip_allowlist_removed", detail: map[string]any{"cidr": cidr}})
		}
	}

	previousBlocks := make(map[string]config.BlockedIPEntry, len(previous.Server.Security.BlockedIPs))
	currentBlocks := make(map[string]config.BlockedIPEntry, len(current.Server.Security.BlockedIPs))
	for _, entry := range previous.Server.Security.BlockedIPs {
		previousBlocks[entry.CIDR] = entry
	}
	for _, entry := range current.Server.Security.BlockedIPs {
		currentBlocks[entry.CIDR] = entry
	}
	for _, cidr := range sortedPolicyKeys(currentBlocks) {
		entry := currentBlocks[cidr]
		if _, exists := previousBlocks[cidr]; !exists {
			reason := entry.Description
			if reason == "" {
				reason = "configured"
			}
			events = append(events, securityPolicyAuditEvent{name: "security.ip_blocked", detail: map[string]any{"ip": cidr, "cidr": cidr, "reason": reason, "duration": "permanent", "auto_blocked": false}})
		}
	}
	for _, cidr := range sortedPolicyKeys(previousBlocks) {
		if _, exists := currentBlocks[cidr]; !exists {
			events = append(events, securityPolicyAuditEvent{name: "security.ip_unblocked", detail: map[string]any{"ip": cidr, "cidr": cidr, "reason": "configuration_removed"}})
		}
	}
	return events
}

func sortedPolicyKeys[T any](entries map[string]T) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func auditSecurityPolicyChanges(ctx context.Context, previous, current *config.ServerConfig) {
	for _, event := range securityPolicyChanges(previous, current) {
		if err := database.WriteAuditEventWithSource(ctx, event.name, "system", "", "config", event.detail); err != nil {
			log.Printf("Could not record security policy change %s: %v", event.name, err)
		}
	}
}
