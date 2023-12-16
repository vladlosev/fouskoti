package repository

import (
	"fmt"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"helm.sh/helm/v3/pkg/chart"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

type gitRepoChartLoader struct {
	loaderConfig
}

func newGitRepositoryLoader(config loaderConfig) repositoryLoader {
	return &gitRepoChartLoader{loaderConfig: config}
}

func (loader *gitRepoChartLoader) loadRepositoryChart(
	repoNode *yaml.RNode,
	chartName string,
	chartVersion string,
) (*chart.Chart, error) {
	var repo sourcev1.GitRepository

	err := decodeToObject(repoNode, &repo)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to decode GitRepository %s/%s: %w",
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}

	// TODO(vlad): Implement.
	return nil, fmt.Errorf("not implemented")
}

func (loader *gitRepoChartLoader) loadChartByURL(
	repoURL string,
	chartName string,
	chartVersion string,
) (*chart.Chart, error) {
	// TODO(vlad): Implement.
	return nil, fmt.Errorf("not implemented")
}
