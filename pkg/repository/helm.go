package repository

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"strings"

	helmv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"helm.sh/helm/v3/pkg/chart"
	helmloader "helm.sh/helm/v3/pkg/chart/loader"
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
	logger *slog.Logger,
	release *helmv2beta1.HelmRelease,
	repo *sourcev1beta2.HelmRepository,
) (*chart.Chart, error) {
	cacheRoot, err := os.MkdirTemp("", "helm-repo-")
	if err != nil {
		return nil, fmt.Errorf("unable to create a temp dir for Helm repo: %w", err)
	}
	defer os.RemoveAll(cacheRoot) // TODO(vlad): Find way to persist the cache.

	loader := &helmRepoChartLoader{
		logger:    logger,
		cacheRoot: cacheRoot,
	}
	return loader.loadHelmRepositoryChart(ctx, release, repo)
}

type helmRepoChartLoader struct {
	logger    *slog.Logger
	cacheRoot string
}

func (loader *helmRepoChartLoader) getCachePathForRepo(
	repoURL string,
) (string, error) {
	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return "", fmt.Errorf("unable to parse repository URL %s: %w", repoURL, err)
	}
	urlPath := strings.TrimSuffix(parsedURL.Path, "/")
	var repoPath string
	if urlPath == "" {
		repoPath = parsedURL.Host
	} else {
		repoPath = fmt.Sprintf("%s-%s", parsedURL.Host, urlPath)
	}
	return path.Join(loader.cacheRoot, repoPath), nil
}

func (loader *helmRepoChartLoader) loadHelmRepositoryChart(
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

	repoPath, err := loader.getCachePathForRepo(normalizedURL)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get cache path for Helm repository %s: %w",
			normalizedURL,
			err,
		)
	}

	return loader.loadHelmChartByURL(
		normalizedURL,
		repoPath,
		release.Spec.Chart.Spec.Chart,
		release.Spec.Chart.Spec.Version,
	)
}

func (loader *helmRepoChartLoader) loadHelmChartByURL(
	repoURL string,
	repoPath string,
	name string,
	version string,
) (*chart.Chart, error) {
	loader.logger.
		With(
			"repoURL", repoURL,
			"name", name,
			"version", version,
		).
		Debug("Loading chart from Helm repository")
	getters := helmgetter.All(&cli.EnvSettings{})
	chartRepo, err := helmrepo.NewChartRepository(
		&helmrepo.Entry{
			Name: path.Join(repoPath, "repo"),
			URL:  repoURL,
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
			repoURL,
			err,
		)
	}
	repoIndex, err := helmrepo.LoadIndexFile(indexFilePath)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load index file for Helm repository %s: %w",
			repoURL,
			err,
		)
	}
	chartRepo.IndexFile = repoIndex
	chartVersion, err := repoIndex.Get(name, version)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get chart %s/%s from Helm repository %s: %w",
			name,
			version,
			repoURL,
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

	chart, err := helmloader.LoadArchive(chartData)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load chart %s/%s in %s: %w",
			name,
			version,
			repoURL,
			err,
		)
	}
	err = loader.loadChartDependencies(chart)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load chart dependencies for %s/%s in %s: %w",
			name,
			chart.Metadata.Version,
			repoURL,
			err,
		)
	}

	loader.logger.
		With(
			"repoURL", repoURL,
			"name", name,
			"version", chart.Metadata.Version,
		).
		Debug("Finished loading chart")
	return chart, nil
}

func (loader *helmRepoChartLoader) loadChartDependencies(
	chart *chart.Chart,
) error {
	for _, dependency := range chart.Metadata.Dependencies {
		repoURL, err := normalizeURL(dependency.Repository)
		if err != nil {
			return fmt.Errorf(
				"unable to normalize URL for dependency chart %s/%s: %w",
				dependency.Name,
				dependency.Version,
				err,
			)
		}

		repoPath, err := loader.getCachePathForRepo(repoURL)
		if err != nil {
			return fmt.Errorf(
				"unable to get cache path for Helm repository %s: %w",
				repoURL,
				err,
			)
		}

		dependencyChart, err := loader.loadHelmChartByURL(
			repoURL,
			repoPath,
			dependency.Name,
			dependency.Version,
		)
		if err != nil {
			return fmt.Errorf(
				"unable to load chart %s/%s from %s (a dependency of %s): %w",
				dependency.Name,
				dependency.Version,
				repoURL,
				chart.Name(),
				err,
			)
		}
		chart.AddDependency(dependencyChart)
	}
	return nil
}
