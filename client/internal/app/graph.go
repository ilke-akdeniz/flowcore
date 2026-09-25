package app

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/mike-akdeniz/flowcore"
)

// Mermaid renders a definition as a mermaid flowchart.
//
// A workflow is a directed graph, and a list of steps is a poor way to read one.
// Seeing it drawn is what makes a missing route or an unreachable step obvious at
// a glance, and it costs a string builder.
//
// Everything here comes from the definition FlowCore returned. The library has no
// notion of a diagram, a layout or a colour — this is a projection of the same
// tree the editor is built from, which is why the picture cannot drift from what
// will actually run.
func Mermaid(definition flowcore.WorkflowDefinition) string {
	var graph strings.Builder

	graph.WriteString("flowchart TD\n")

	stepNode := make(map[uuid.UUID]string, len(definition.Steps))
	for i, step := range definition.Steps {
		stepNode[step.ID] = fmt.Sprintf("s%d", i)
	}

	statusNode := make(map[uuid.UUID]string, len(definition.Statuses))
	for i, status := range definition.Statuses {
		statusNode[status.ID] = fmt.Sprintf("t%d", i)
	}

	entry := uuid.Nil
	if definition.InitialStepDefinitionID != nil {
		entry = *definition.InitialStepDefinitionID
	}

	for _, step := range definition.Steps {
		label := escape(step.Name)
		if step.AssigneeID != "" {
			label += "<br/><small>" + escape(step.AssigneeID) + "</small>"
		}

		fmt.Fprintf(&graph, "  %s[\"%s\"]\n", stepNode[step.ID], label)
	}

	// Only statuses something actually terminates in are drawn. A status that is
	// merely shown while a run sits on a step is not a node in the graph — it is a
	// label on one, which is a distinction the library makes and the picture
	// should keep.
	terminal := make(map[uuid.UUID]bool)
	for _, step := range definition.Steps {
		for _, action := range step.Actions {
			if action.TerminalWorkflowStatusDefinitionID != nil {
				terminal[*action.TerminalWorkflowStatusDefinitionID] = true
			}
		}
	}

	for _, status := range definition.Statuses {
		if terminal[status.ID] {
			fmt.Fprintf(&graph, "  %s([\"%s\"])\n", statusNode[status.ID], escape(status.Name))
		}
	}

	for _, step := range definition.Steps {
		for _, action := range step.Actions {
			switch {
			case action.NextStepDefinitionID != nil:
				if target, ok := stepNode[*action.NextStepDefinitionID]; ok {
					fmt.Fprintf(&graph, "  %s -->|%s| %s\n",
						stepNode[step.ID], escape(action.Name), target)
				}
			case action.TerminalWorkflowStatusDefinitionID != nil:
				if target, ok := statusNode[*action.TerminalWorkflowStatusDefinitionID]; ok {
					fmt.Fprintf(&graph, "  %s -->|%s| %s\n",
						stepNode[step.ID], escape(action.Name), target)
				}
			}
		}
	}

	if node, ok := stepNode[entry]; ok {
		fmt.Fprintf(&graph, "  start((start)) --> %s\n", node)
	}

	return graph.String()
}

// escape keeps a name from breaking the diagram. Mermaid labels are quoted, so
// quotes and the pipe used for edge labels are what matter.
func escape(name string) string {
	replacer := strings.NewReplacer(`"`, "'", "|", "/", "\n", " ")

	return replacer.Replace(name)
}
