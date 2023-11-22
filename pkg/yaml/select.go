package yaml

import "gopkg.in/yaml.v3"

func GetChild(node *yaml.Node, name string) *yaml.Node {
	if node == nil {
		return nil
	}
	node = discardDocumentNode(node)

	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == name {
			return node.Content[i+1]
		}
	}
	return nil
}

func GetChildByPath(node *yaml.Node, path []string) *yaml.Node {
	for _, name := range path {
		node = GetChild(node, name)
		if node == nil {
			return nil
		}
	}
	return node
}

func GetChildStringByPath(
	node *yaml.Node,
	path []string,
	defaultValue string,
) string {
	childNode := GetChildByPath(node, path)
	if childNode == nil {
		return defaultValue
	}
	return childNode.Value
}
