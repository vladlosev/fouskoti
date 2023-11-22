package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	yamlutil "github.com/vladlosev/hrval/pkg/yaml"
)

type ExpandCommandOptions struct {
}

const ExpandCommandName = "expand"

func readInput(input io.Reader) ([]*yaml.Node, error) {
	result := []*yaml.Node{}
	decoder := yaml.NewDecoder(input)
	for {
		node := &yaml.Node{}
		err := decoder.Decode(node)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("unable to parse YAML: %w", err)
		}
		result = append(result, node)
	}
	return result, nil
}

func writeResults(output io.Writer, nodes []*yaml.Node) error {
	for i, node := range nodes {
		if i != 0 {
			_, err := output.Write([]byte("---\n"))
			if err != nil {
				return fmt.Errorf("unable to write output: %w", err)
			}
		}
		encoder := yaml.NewEncoder(output)
		encoder.SetIndent(2)
		err := encoder.Encode(node)
		if err != nil {
			return fmt.Errorf("unable to marshal to YAML: %w", err)
		}
	}
	return nil
}

func filterHelmReleases(nodes []*yaml.Node) []*yaml.Node {
	return yamlutil.FindDocumentsByGroupKind(
		nodes,
		"helm.toolkit.fluxcd.io",
		"HelmRelease",
	)
}

func MarshalToJSON(nodes []*yaml.Node) []byte {
	bytes, _ := json.MarshalIndent(nodes, "", "  ")
	return bytes
}

func expandHelmRelease(
	releaseNode *yaml.Node,
	nodes []*yaml.Node,
) ([]*yaml.Node, error) {
	releaseNamespace := yamlutil.GetChildStringByPath(
		releaseNode,
		[]string{"metadata", "namespace"},
		"",
	)
	apiVersion := yamlutil.GetChildStringByPath(
		releaseNode,
		[]string{"spec", "chart", "spec", "sourceRef", "apiVersion"},
		"",
	)
	kind := yamlutil.GetChildStringByPath(
		releaseNode,
		[]string{"spec", "chart", "spec", "sourceRef", "kind"},
		"",
	)
	namespace := yamlutil.GetChildStringByPath(
		releaseNode,
		[]string{"spec", "chart", "spec", "sourceRef", "namespace"},
		releaseNamespace,
	)
	name := yamlutil.GetChildStringByPath(
		releaseNode,
		[]string{"spec", "chart", "spec", "sourceRef", "name"},
		releaseNamespace,
	)
	repositoryNode := yamlutil.FindDocumentByGroupVersionKindNameRef(
		nodes,
		apiVersion,
		kind,
		namespace,
		name,
	)
	if repositoryNode == nil {
		return nil, fmt.Errorf(
			"unable to find repository for Helm release %s/%s",
			releaseNamespace,
			yamlutil.GetChildStringByPath(
				releaseNode,
				[]string{"metadata", "name"},
				"",
			),
		)
	}
	return []*yaml.Node{repositoryNode}, nil
}

func NewExpandCommand(options *ExpandCommandOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   ExpandCommandName,
		Short: "Expands HelmRelease objects into generated templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, logger := getContextAndLogger(cmd)
			logger.Info("Staring expand command")
			var input io.Reader
			var err error
			if len(args) > 0 {
				input, err = os.Open(args[0])
				if err != nil {
					return fmt.Errorf("unable to open input file %s: %w", os.Args[1], err)
				}
			} else {
				input = os.Stdin
			}
			nodes, err := readInput(input)
			if err != nil {
				return fmt.Errorf("unable to read input: %w", err)
			}
			releaseNodes := filterHelmReleases(nodes)
			var resultNodes []*yaml.Node
			for _, node := range releaseNodes {
				expandedNodes, err := expandHelmRelease(node, nodes)
				if err != nil {
					return fmt.Errorf("unable to expand release: %w", err)
				}
				resultNodes = append(resultNodes, expandedNodes...)
			}
			os.Stderr.Write([]byte("\n"))
			err = writeResults(os.Stdout, resultNodes)
			if err != nil {
				return fmt.Errorf("unable to marshal to YAML: %w", err)
			}
			return nil
		},
		SilenceUsage: true,
	}
	return command
}
