package topology

import (
	"sort"

	"github.com/mclucy/lucy/types"
)

type ConnectionRegistry struct {
	byNodeID     map[types.RuntimeNodeID][]ConnectionDefinition
	byCapability map[types.RuntimeCapability][]ConnectionDefinition
}

var DefaultConnectionRegistry = NewConnectionRegistry(nil)

func NewConnectionRegistry(definitions []ConnectionDefinition) ConnectionRegistry {
	registry := ConnectionRegistry{
		byNodeID:     make(map[types.RuntimeNodeID][]ConnectionDefinition),
		byCapability: make(map[types.RuntimeCapability][]ConnectionDefinition),
	}

	for _, definition := range definitions {
		stored := cloneConnectionDefinition(definition)

		switch stored.Source.Type {
		case ConnectionSourceNode:
			registry.byNodeID[stored.Source.NodeID] = append(
				registry.byNodeID[stored.Source.NodeID],
				stored,
			)
		case ConnectionSourceCapability:
			registry.byCapability[stored.Source.Capability] = append(

				registry.byCapability[stored.Source.Capability],
				stored,
			)
		}
	}

	for nodeID := range registry.byNodeID {
		sortConnectionDefinitions(registry.byNodeID[nodeID])
	}
	for capability := range registry.byCapability {
		sortConnectionDefinitions(registry.byCapability[capability])
	}

	return registry
}

func (r ConnectionRegistry) LookupByNodeID(id types.RuntimeNodeID) []ConnectionDefinition {
	definitions := r.byNodeID[id]
	if len(definitions) == 0 {
		return nil
	}

	return cloneConnectionDefinitions(definitions)
}

func (r ConnectionRegistry) LookupByCapability(capability types.RuntimeCapability) []ConnectionDefinition {
	definitions := r.byCapability[capability]
	if len(definitions) == 0 {
		return nil
	}

	return cloneConnectionDefinitions(definitions)
}

func cloneConnectionDefinitions(definitions []ConnectionDefinition) []ConnectionDefinition {
	cloned := make([]ConnectionDefinition, 0, len(definitions))
	for _, definition := range definitions {
		cloned = append(cloned, cloneConnectionDefinition(definition))
	}

	return cloned
}

func cloneConnectionDefinition(definition ConnectionDefinition) ConnectionDefinition {
	return ConnectionDefinition{
		Source: ConnectionSource{
			Type:       definition.Source.Type,
			NodeID:     definition.Source.NodeID,
			Capability: definition.Source.Capability,
		},
		TargetNodeID: definition.TargetNodeID,
		Kind:         definition.Kind,
		Risk:         definition.Risk,
	}
}

func sortConnectionDefinitions(definitions []ConnectionDefinition) {
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].TargetNodeID != definitions[j].TargetNodeID {
			return string(definitions[i].TargetNodeID) < string(definitions[j].TargetNodeID)
		}
		if definitions[i].Kind != definitions[j].Kind {
			return string(definitions[i].Kind) < string(definitions[j].Kind)
		}
		return definitions[i].Risk < definitions[j].Risk
	})
}
