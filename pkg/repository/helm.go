package repository

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"

	helmv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	helmgetter "helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	helmrepo "helm.sh/helm/v3/pkg/repo"
)

// normalizeURL normalizes a ChartRepository URL by its scheme.
func normalizeURL(repositoryURL string) (string, error) {
	if repositoryURL == "" {
		return "", nil
	}
	u, err := url.Parse(repositoryURL)
	if err != nil {
		return "", err
	}

	if u.Scheme == registry.OCIScheme {
		u.Path = strings.TrimRight(u.Path, "/")
		// we perform the same operation on u.RawPath so that it will be a valid encoding
		// of u.Path. This allows u.EscapedPath() (which is used in computing u.String()) to return
		// the correct value when the path is url encoded.
		// ref: https://pkg.go.dev/net/url#URL.EscapedPath
		u.RawPath = strings.TrimRight(u.RawPath, "/")
		return u.String(), nil
	}

	u.Path = strings.TrimRight(u.Path, "/") + "/"
	u.RawPath = strings.TrimRight(u.RawPath, "/") + "/"
	return u.String(), nil
}

// TODO(vlad): Add caching support.

func loadHelmRepositoryChart(
	ctx context.Context,
	release *helmv2beta1.HelmRelease,
	repo *sourcev1beta2.HelmRepository,
) (*chart.Chart, error) {
	normalizedURL, err := normalizeURL(repo.Spec.URL)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid Helm repository URL %s: %w",
			repo.Spec.URL,
			err,
		)
	}

	tempPath, err := os.MkdirTemp("", "helm-repo-")
	if err != nil {
		return nil, fmt.Errorf("unable to create a temp dir for Helm repo: %w", err)
	}

	repoPath := path.Join(
		tempPath,
		strings.ToLower(repo.Kind),
		release.Namespace,
		repo.Name,
		"repo",
	)

	getters := helmgetter.All(&cli.EnvSettings{})
	chartRepo, err := helmrepo.NewChartRepository(
		&helmrepo.Entry{
			Name: repo.Name,
			URL:  normalizedURL,
			// TODO(vlad): Use chart repository options when provided.
		},
		getters,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create chart repository object: %w", err)
	}
	chartRepo.CachePath = repoPath

	indexFilePath, err := chartRepo.DownloadIndexFile()
	if err != nil {
		return nil, fmt.Errorf(
			"unable to download index file for Helm repository %s: %w",
			normalizedURL,
			err,
		)
	}
	repoIndex, err := helmrepo.LoadIndexFile(indexFilePath)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load index file for Helm repository %s: %w",
			normalizedURL,
			err,
		)
	}
	chartRepo.IndexFile = repoIndex
	chartVersion, err := repoIndex.Get(
		release.Spec.Chart.Spec.Chart,
		release.Spec.Chart.Spec.Version,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get chart %s/%s from Helm repository %s: %w",
			release.Spec.Chart.Spec.Chart,
			release.Spec.Chart.Spec.Version,
			normalizedURL,
			err,
		)
	}

	parsedURL, err := url.Parse(chartVersion.URLs[0])
	if err != nil {
		return nil, fmt.Errorf(
			"unable to parse chart URL %s: %w",
			chartVersion.URLs[0],
			err,
		)
	}

	getter, err := getters.ByScheme(parsedURL.Scheme)
	if err != nil {
		return nil, fmt.Errorf(
			"unknown scheme %s for chart %s: %w",
			parsedURL.Scheme,
			chartVersion.URLs[0],
			err,
		)
	}

	chartData, err := getter.Get(
		parsedURL.String(),
		[]helmgetter.Option{}...) // TODO(vlad): Set options if necessary.
	if err != nil {
		return nil, fmt.Errorf(
			"unable to download chart %s: %w",
			parsedURL.String(),
			err,
		)
	}

	chart, err := loader.LoadArchive(chartData)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load chart %s/%s in %s: %w",
			release.Spec.Chart.Spec.Chart,
			release.Spec.Chart.Spec.Version,
			normalizedURL,
			err,
		)
	}
	return chart, nil
}
