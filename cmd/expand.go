package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/kustomize/kyaml/kio"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/vladlosev/hrval/pkg/repository"
	yamlutil "github.com/vladlosev/hrval/pkg/yaml"
)

type ExpandCommandOptions struct {
	credentialsFileName string
}

const ExpandCommandName = "expand"

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

	repoNamespace, err := yamlutil.GetStringOr(
		helmRelease,
		"spec.chart.spec.sourceRef.namespace",
		helmRelease.GetNamespace(),
	)
	if err != nil {
		return nil, err
	}

	repoApiVersion, err := yamlutil.GetStringOr(
		helmRelease,
		"spec.chart.spec.sourceRef.apiVersion",
		"",
	)
	if err != nil {
		return nil, err
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

type releaseRepo struct {
	release *yaml.RNode
	repo    *yaml.RNode
}

type releaseRepoFilter struct {
	pairs *[]releaseRepo
}

func newReleaseRepoFilter(pairs *[]releaseRepo) *releaseRepoFilter {
	return &releaseRepoFilter{pairs: pairs}
}

func (filter *releaseRepoFilter) Filter(
	nodes []*yaml.RNode,
) ([]*yaml.RNode, error) {
	helmReleases := []*yaml.RNode{}

	for _, node := range nodes {
		if yamlutil.GetGroup(node) == "helm.toolkit.fluxcd.io" &&
			node.GetKind() == "HelmRelease" {
			helmReleases = append(helmReleases, node)
		}
	}

	for _, helmRelease := range helmReleases {
		repository, err := getRepositoryForHelmRelease(nodes, helmRelease)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to find repository for HelmRelease %s/%s: %w",
				helmRelease.GetNamespace(),
				helmRelease.GetName(),
				err)
		}
		*filter.pairs = append(
			*filter.pairs,
			releaseRepo{release: helmRelease, repo: repository},
		)
	}
	return nodes, nil
}

type releaseRepoRenderer struct {
	ctx         context.Context
	logger      *slog.Logger
	credentials repository.Credentials
	pairs       *[]releaseRepo
}

func newReleaseRepoRenderer(
	ctx context.Context,
	logger *slog.Logger,
	credentials repository.Credentials,
	pairs *[]releaseRepo,
) *releaseRepoRenderer {
	return &releaseRepoRenderer{
		ctx:         ctx,
		logger:      logger,
		credentials: credentials,
		pairs:       pairs,
	}
}

func (renderer *releaseRepoRenderer) Filter(
	nodes []*yaml.RNode,
) ([]*yaml.RNode, error) {
	result := []*yaml.RNode{}

	for _, pair := range *renderer.pairs {
		expanded, err := repository.ExpandHelmRelease(
			renderer.ctx,
			renderer.logger,
			renderer.credentials,
			pair.release,
			pair.repo,
		)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to expand Helm release %s/%s: %w",
				pair.release.GetNamespace(),
				pair.release.GetName(),
				err,
			)
		}
		result = append(result, expanded...)
	}
	return append(nodes, result...), nil
}

func appendDocSeparator(inputs []io.Reader) []io.Reader {
	if len(inputs) > 0 {
		inputs = append(inputs, bytes.NewBufferString("\n---\n"))
	}
	return inputs
}

func NewExpandCommand(options *ExpandCommandOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   ExpandCommandName,
		Short: "Expands HelmRelease objects into generated templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, logger := getContextAndLogger(cmd)
			logger.Info("Staring expand command")
			var inputs []io.Reader
			var err error
			for _, arg := range args {
				if arg == "-" {
					inputs = append(inputs, os.Stdin)
				} else {
					inputs = appendDocSeparator(inputs)
					file, err := os.Open(arg)
					if err != nil {
						return fmt.Errorf("unable to open input file %s: %w", arg, err)
					}
					defer file.Close()
					inputs = appendDocSeparator(inputs)
					inputs = append(inputs, file)
				}
			}

			stringCreds := map[string]map[string]string{}
			credentials := repository.Credentials{}

			if options.credentialsFileName != "" {
				creds, err := os.ReadFile(options.credentialsFileName)
				if err != nil {
					return fmt.Errorf(
						"unable to open credentials file %s: %w",
						options.credentialsFileName,
						err,
					)
				}
				err = yaml.Unmarshal(creds, stringCreds)
				if err != nil {
					return fmt.Errorf(
						"unable to parse credentials file %s as YAML: %w",
						options.credentialsFileName,
						err,
					)
				}

				for repo, items := range stringCreds {
					credsItem := map[string][]byte{}
					for name, content := range items {
						credsItem[name] = []byte(content)
					}
					credentials[repo] = credsItem
				}
			}

			var pairs []releaseRepo
			filter1 := newReleaseRepoFilter(&pairs)
			filter2 := newReleaseRepoRenderer(ctx, logger, credentials, &pairs)
			err = kio.Pipeline{
				Inputs: []kio.Reader{&kio.ByteReader{
					Reader: io.MultiReader(inputs...),
				}},
				Filters: []kio.Filter{filter1, filter2},
				Outputs: []kio.Writer{kio.ByteWriter{Writer: os.Stdout}},
			}.Execute()
			return err
		},
		SilenceUsage: true,
	}
	command.PersistentFlags().StringVarP(
		&options.credentialsFileName,
		"credentials-file",
		"",
		"",
		"Name of the repository credentials file",
	)

	return command
}
