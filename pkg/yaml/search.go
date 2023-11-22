package yaml

import (
	"strings"

	"gopkg.in/yaml.v3"
)

func discardDocumentNode(node *yaml.Node) *yaml.Node {
	if node.Kind != yaml.DocumentNode {
		return node
	}
	return node.Content[0]
}

func hasChild(
	node *yaml.Node,
	name string,
	childPred func(childNode *yaml.Node) bool,
) bool {
	if node.Kind != yaml.MappingNode {
		return false
	}
	var childName string
	for i, childNode := range node.Content {
		if i%2 == 0 {
			childName = childNode.Value
			continue
		}
		if childName != name {
			continue
		}
		if !childPred(childNode) {
			continue
		}
		return true
	}
	return false
}

func hasScalarChild(
	node *yaml.Node,
	name string,
	childPred func(childNode *yaml.Node) bool,
) bool {
	return hasChild(node, name, func(childNode *yaml.Node) bool {
		return childNode.Kind == yaml.ScalarNode && childPred(childNode)
	})
}

func hasChildAttribute(node *yaml.Node, name string, value string) bool {
	return hasScalarChild(
		node,
		name,
		func(childNode *yaml.Node) bool { return childNode.Value == value },
	)
}

func hasChildApiGroup(node *yaml.Node, group string) bool {
	return hasScalarChild(
		node,
		"apiVersion",
		func(childNode *yaml.Node) bool {
			parts := strings.Split(childNode.Value, "/")
			if len(parts) != 2 {
				return false
			}
			return parts[0] == group
		},
	)
}

func FindDocumentsByGroupKind(
	nodes []*yaml.Node,
	group string,
	kind string,
) []*yaml.Node {
	var result []*yaml.Node

	for _, node := range nodes {
		node = discardDocumentNode(node)

		if !hasChildAttribute(node, "kind", "HelmRelease") {
			continue
		}
		if !hasChildApiGroup(node, group) {
			continue
		}
		result = append(result, node)
	}
	return result
}

func FindDocumentByGroupKindNameRef(
	nodes []*yaml.Node,
	group string,
	kind string,
	namespace string,
	name string,
) *yaml.Node {
	for _, node := range nodes {
		node = discardDocumentNode(node)

		if !hasChildAttribute(node, "kind", kind) {
			continue
		}
		if !hasChildApiGroup(node, group) {
			continue
		}
		if !hasChild(node, "metadata", func(metadataNode *yaml.Node) bool {
			if !hasChildAttribute(metadataNode, "name", name) {
				return false
			}
			if namespace == "" {
				return true
			}
			return hasChildAttribute(metadataNode, "namespace", namespace)
		}) {
			continue
		}

		return node
	}
	return nil
}

func FindDocumentByGroupVersionKindNameRef(
	nodes []*yaml.Node,
	apiVersion string,
	kind string,
	namespace string,
	name string,
) *yaml.Node {
	for _, node := range nodes {
		node = discardDocumentNode(node)

		if !hasChildAttribute(node, "kind", kind) {
			continue
		}
		if apiVersion != "" && !hasChildAttribute(node, "apiVersion", apiVersion) {
			continue
		}
		if !hasChild(node, "metadata", func(metadataNode *yaml.Node) bool {
			if !hasChildAttribute(metadataNode, "name", name) {
				return false
			}
			if namespace == "" {
				return true
			}
			return hasChildAttribute(metadataNode, "namespace", namespace)
		}) {
			continue
		}

		return node
	}
	return nil

}
