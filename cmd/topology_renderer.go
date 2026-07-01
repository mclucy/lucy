package cmd

import (
	mermaidascii "github.com/AlexanderGrooff/mermaid-ascii/cmd"
	"github.com/AlexanderGrooff/mermaid-ascii/pkg/diagram"
	"github.com/mclucy/lucy/internal/fn"

	"github.com/mclucy/lucy/types"
)

func renderTopologyASCII(
	topology *types.ServerTopology,
	direction string,
	noStyle bool,
	longOut bool,
) string {
	if topology == nil || len(topology.Nodes) == 0 {
		return ""
	}

	padding := fn.Ternary(longOut, 1, 0)
	mermaid := buildMermaidTopology(topology, direction, longOut)
	config, err := diagram.NewCLIConfig(
		noStyle,
		false,
		false,
		padding,
		12,
		2,
		direction,
	)
	if err != nil {
		return mermaid
	}

	rendered, err := mermaidascii.RenderDiagram(mermaid, config)
	if err != nil {
		return mermaid
	}

	return rendered
}
