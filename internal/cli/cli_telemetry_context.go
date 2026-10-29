package cli

import (
	"errors"
	"strings"

	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/telemetry"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

type telemetryWorkspace struct {
	Product          string `json:"product,omitempty"`
	ProductVersion   string `json:"product_version,omitempty"`
	MinecraftVersion string `json:"minecraft_version,omitempty"`
	Packages         int    `json:"packages"`
}

func recordWorkspaceTelemetryFailure(inv *Invocation) {
	if inv.TelemetryEnabled {
		inv.TelemetryContexts = append(inv.TelemetryContexts, telemetry.Context{Kind: "workspace", Status: telemetry.ContextStatus("failed")})
	}
}

func recordWorkspaceTelemetry(inv *Invocation, observed *workspace.Workspace, document statusDocument) {
	if !inv.TelemetryEnabled {
		return
	}
	context := telemetry.Context{Kind: "workspace", Status: telemetry.ContextStatus("present")}
	if observed == nil || document.Server == nil {
		context.Status = telemetry.ContextStatus("absent")
		if observed != nil {
			if selection, ok := errors.AsType[*discovery.SelectionError](observed.SelectionError()); ok && selection.State != discovery.SelectionNone {
				context.Status = telemetry.ContextStatus("failed")
			}
		}
	} else {
		context.Data = telemetryWorkspace{
			Product:          observed.Runtime().Product().String(),
			ProductVersion:   telemetryVersion(document.Server.Product.Version),
			MinecraftVersion: telemetryVersion(document.Server.Minecraft),
			Packages:         len(document.Packages),
		}
	}
	inv.TelemetryContexts = append(inv.TelemetryContexts, context)
}

type telemetryJava struct {
	Major        int    `json:"major"`
	Version      string `json:"version,omitempty"`
	Distribution string `json:"distribution,omitempty"`
}

func recordJavaTelemetryFailure(inv *Invocation) {
	if inv.TelemetryEnabled {
		inv.TelemetryContexts = append(inv.TelemetryContexts, telemetry.Context{Kind: "java", Status: telemetry.ContextStatus("failed")})
	}
}

func recordJavaTelemetry(inv *Invocation, selection *javaruntime.Check) {
	if !inv.TelemetryEnabled || selection == nil {
		return
	}
	context := telemetry.Context{Kind: "java", Status: telemetry.ContextStatus("absent")}
	if selection.Runtime != nil {
		context.Status = telemetry.ContextStatus("present")
		context.Data = telemetryJava{
			Major:        selection.Runtime.Major,
			Version:      telemetryVersion(selection.Runtime.Version),
			Distribution: string(selection.Runtime.Distribution),
		}
	}
	inv.TelemetryContexts = append(inv.TelemetryContexts, context)
}

func telemetryVersion(value string) string {
	if len(value) > 64 || !strings.ContainsAny(value, "0123456789") ||
		strings.Trim(value, "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ.+-_") != "" {
		return ""
	}
	return value
}
