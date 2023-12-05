package repository

import (
	"context"
	"fmt"

	helmv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"helm.sh/helm/v3/pkg/chart"
)

func loadOciRepositoryChart(
	ctx context.Context,
	release *helmv2beta1.HelmRelease,
	repo *sourcev1beta2.OCIRepository,
) (*chart.Chart, error) {
	// TODO(vlad): Implement.
	return nil, fmt.Errorf("not implemented")
}
