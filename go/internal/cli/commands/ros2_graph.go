package commands

import (
	"fmt"
	"sort"
	"strings"

	agentpbv2 "github.com/wendylabsinc/wendy/go/proto/gen/agentpb/v2"
)

// ros2HiddenGraphTopics are infrastructure topics every node touches; they
// are noise in a connectivity graph (rqt_graph hides them too).
var ros2HiddenGraphTopics = map[string]bool{
	"/rosout":           true,
	"/parameter_events": true,
}

// ros2GraphEdges builds topic → publishers and topic → subscribers maps from
// a graph response, skipping hidden infrastructure topics.
func ros2GraphEdges(graph *agentpbv2.GetROS2GraphResponse) (pubs, subs map[string][]string) {
	pubs = make(map[string][]string)
	subs = make(map[string][]string)
	for _, e := range graph.GetPublishes() {
		if !ros2HiddenGraphTopics[e.GetTopic()] {
			pubs[e.GetTopic()] = append(pubs[e.GetTopic()], e.GetNode())
		}
	}
	for _, e := range graph.GetSubscribes() {
		if !ros2HiddenGraphTopics[e.GetTopic()] {
			subs[e.GetTopic()] = append(subs[e.GetTopic()], e.GetNode())
		}
	}
	return pubs, subs
}

// renderROS2GraphASCII renders the node graph as one "publisher ──topic──▶
// subscriber" line per edge, with dangling publications and isolated nodes
// listed afterwards (WDY-1333).
func renderROS2GraphASCII(graph *agentpbv2.GetROS2GraphResponse) string {
	pubs, subs := ros2GraphEdges(graph)

	topics := make([]string, 0, len(pubs))
	for topic := range pubs {
		topics = append(topics, topic)
	}
	sort.Strings(topics)

	var b strings.Builder
	connected := make(map[string]bool)
	for _, topic := range topics {
		for _, pub := range pubs[topic] {
			if len(subs[topic]) == 0 {
				fmt.Fprintf(&b, "[%s] ──%s──▶ (no subscribers)\n", pub, topic)
				connected[pub] = true
				continue
			}
			for _, sub := range subs[topic] {
				fmt.Fprintf(&b, "[%s] ──%s──▶ [%s]\n", pub, topic, sub)
				connected[pub] = true
				connected[sub] = true
			}
		}
	}

	var isolated []string
	for _, node := range graph.GetNodes() {
		fqn := ros2GraphNodeFQN(node)
		if !connected[fqn] {
			isolated = append(isolated, fqn)
		}
	}
	sort.Strings(isolated)
	if len(isolated) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("Isolated nodes (no graph connections):\n")
		for _, node := range isolated {
			fmt.Fprintf(&b, "  [%s]\n", node)
		}
	}
	if b.Len() == 0 {
		return "No ROS 2 nodes found.\n"
	}
	return b.String()
}

// renderROS2GraphDOT renders the node graph as Graphviz DOT, with nodes as
// boxes and topics as edge labels.
func renderROS2GraphDOT(graph *agentpbv2.GetROS2GraphResponse) string {
	pubs, subs := ros2GraphEdges(graph)

	var b strings.Builder
	b.WriteString("digraph ros2 {\n")
	b.WriteString("  rankdir=LR;\n")
	b.WriteString("  node [shape=box];\n")
	for _, node := range graph.GetNodes() {
		fmt.Fprintf(&b, "  %q;\n", ros2GraphNodeFQN(node))
	}
	topics := make([]string, 0, len(pubs))
	for topic := range pubs {
		topics = append(topics, topic)
	}
	sort.Strings(topics)
	for _, topic := range topics {
		for _, pub := range pubs[topic] {
			for _, sub := range subs[topic] {
				fmt.Fprintf(&b, "  %q -> %q [label=%q];\n", pub, sub, topic)
			}
		}
	}
	b.WriteString("}\n")
	return b.String()
}

// ros2GraphNodeFQN joins a node's namespace and name into its
// fully-qualified graph name.
func ros2GraphNodeFQN(n *agentpbv2.ROS2Node) string {
	ns := n.GetNamespace()
	if ns == "" || ns == "/" {
		return "/" + n.GetName()
	}
	return ns + "/" + n.GetName()
}
