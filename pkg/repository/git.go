package repository

import (
	"context"
	"fmt"

	helmv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"helm.sh/helm/v3/pkg/chart"
)

func loadGitRepositoryChart(
	ctx context.Context,
	release *helmv2beta1.HelmRelease,
	repo *sourcev1.GitRepository,
) (*chart.Chart, error) {
	// TODO(vlad): Implement.
	return nil, fmt.Errorf("not implemented")
}
