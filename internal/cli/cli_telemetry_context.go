package cli

import (
	"errors"
	"strings"

	"github.com/mclucy/lucy/internal/telemetry"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/operation"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

type telemetryWorkspace struct {
	Product          string `json:"product,omitempty"`
	ProductVersion   string `json:"product_version,omitempty"`
	MinecraftVersion string `json:"minecraft_version,omitempty"`
	Packages         *int   `json:"packages,omitempty"`
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
			Packages:         new(min(len(document.Packages), maxTelemetryScale)),
		}
	}
	inv.TelemetryContexts = append(inv.TelemetryContexts, context)
}

// recordInitWorkspaceTelemetry projects the runtime selection that
// initialization already observed into one workspace context. It reads only
// the result the operation returned, so an early failure or prompt
// cancellation after selection still describes the chosen runtime without
// another observation or network probe. A run that never selected a runtime
// stays absent instead of claiming an installation.
func recordInitWorkspaceTelemetry(inv *Invocation, result *operation.InitResult) {
	if !inv.TelemetryEnabled {
		return
	}
	if result == nil {
		return
	}
	context := telemetry.Context{
		Kind:   "workspace",
		Status: telemetry.ContextStatus("absent"),
	}
	selection := result.Discovery.Selected()
	if selection != nil {
		product := selection.Product()
		productVersion := selection.Installation.Identity.Primary.Version
		minecraftVersion := selection.MinecraftVersion()
		var packages *int
		if lock := result.Lock; lock != nil {
			product = lock.Server.Product
			productVersion = lock.Server.CoreVersion
			minecraftVersion = lock.Server.Minecraft
			packages = new(min(len(lock.Packages), maxTelemetryScale))
		}
		context.Status = telemetry.ContextStatus("present")
		context.Data = telemetryWorkspace{
			Product:          allowlistedProduct(product),
			ProductVersion:   telemetryVersion(productVersion.String()),
			MinecraftVersion: telemetryVersion(minecraftVersion.String()),
			Packages:         packages,
		}
	}
	inv.TelemetryContexts = append(inv.TelemetryContexts, context)
}

// allowlistedProduct names a product only when it belongs to the catalog the
// telemetry schema allows; any other spelling is omitted.
func allowlistedProduct(product serverpkg.Product) string {
	known, ok := serverpkg.ParseProduct(product.String())
	if !ok || known == serverpkg.ProductUnknown {
		return ""
	}
	return known.String()
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
