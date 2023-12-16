package repository

import (
	"fmt"

	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"helm.sh/helm/v3/pkg/chart"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

type ociRepoChartLoader struct {
	loaderConfig
}

func newOciRepositoryLoader(config loaderConfig) repositoryLoader {
	return &ociRepoChartLoader{loaderConfig: config}
}

func (loader *ociRepoChartLoader) loadRepositoryChart(
	repoNode *yaml.RNode,
	chartName string,
	chartVersion string,
) (*chart.Chart, error) {
	var repo sourcev1beta2.OCIRepository

	err := decodeToObject(repoNode, &repo)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to decode OCIRepository %s/%s: %w",
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}

	// TODO(vlad): Implement.
	return nil, fmt.Errorf("not implemented")
}

func (loader *ociRepoChartLoader) loadChartByURL(
	repoURL string,
	chartName string,
	chartVersion string,
) (*chart.Chart, error) {
	// TODO(vlad): Implement.
	return nil, fmt.Errorf("not implemented")
}
