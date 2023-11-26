package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

type ExpandCommandOptions struct {
}

const ExpandCommandName = "expand"

func getGroup(node *yaml.RNode) string {
	apiVersion := node.GetApiVersion()
	parts := strings.SplitN(apiVersion, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

func getStringOrDefault(
	node *yaml.RNode,
	fieldSpec string,
	fieldDescription string,
	defaultValue string,
) (string, error) {
	result, err := node.GetString(fieldSpec)
	if err != nil && errors.Is(err, yaml.NoFieldError{Field: fieldSpec}) {
		return defaultValue, nil
	}
	if err != nil {
		return defaultValue, fmt.Errorf("unable to get %s: %w", fieldDescription, err)
	}
	return result, nil
}

func getRepositoryForHelmRelease(
	nodes []*yaml.RNode,
	helmRelease *yaml.RNode,
) (*yaml.RNode, error) {
	repoKind, err := helmRelease.GetString("spec.chart.spec.sourceRef.kind")
	if err != nil {
		return nil, fmt.Errorf("unable to get kind for the repository: %w", err)
	}

	repoName, err := helmRelease.GetString("spec.chart.spec.sourceRef.name")
	if err != nil {
		return nil, fmt.Errorf("unable to get name for the repository: %w", err)
	}

	repoNamespace, err := getStringOrDefault(
		helmRelease,
		"spec.chart.spec.sourceRef.namespace",
		"namespace",
		helmRelease.GetNamespace(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get namespace for the repository: %w",
			err,
		)
	}

	repoApiVersion, err := getStringOrDefault(
		helmRelease,
		"spec.chart.spec.sourceRef.apiVersion",
		"apiVersion",
		"",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get apiVersion for the repository: %w",
			err,
		)
	}

	for _, node := range nodes {
		if node.GetKind() == repoKind &&
			node.GetName() == repoName &&
			node.GetNamespace() == repoNamespace &&
			(repoApiVersion == "" || node.GetApiVersion() == repoApiVersion) {
			return node, nil
		}
	}
	return nil, nil
}

func filterHelmReleases(nodes []*yaml.RNode) ([]*yaml.RNode, error) {
	helmReleases := []*yaml.RNode{}

	for _, node := range nodes {
		if getGroup(node) == "helm.toolkit.fluxcd.io" &&
			node.GetKind() == "HelmRelease" {
			helmReleases = append(helmReleases, node)
		}
	}

	var repositories []*yaml.RNode
	for _, helmRelease := range helmReleases {
		repository, err := getRepositoryForHelmRelease(nodes, helmRelease)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to find repository for HelmRelease %s/%s: %w",
				helmRelease.GetNamespace(),
				helmRelease.GetName(),
				err)
		}
		repositories = append(repositories, repository)
	}
	return repositories, nil
}

func MarshalToJSON(nodes []*yaml.Node) []byte {
	bytes, _ := json.MarshalIndent(nodes, "", "  ")
	return bytes
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
			err = kio.Pipeline{
				Inputs:  []kio.Reader{&kio.ByteReader{Reader: input}},
				Filters: []kio.Filter{kio.FilterFunc(filterHelmReleases)},
				Outputs: []kio.Writer{kio.ByteWriter{Writer: os.Stdout}},
			}.Execute()
			return err
		},
		SilenceUsage: true,
	}
	return command
}
