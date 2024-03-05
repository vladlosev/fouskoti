package repository

import (
	"fmt"
	"net/url"
	"path"
	"strings"

	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"helm.sh/helm/v3/pkg/chart"
	helmloader "helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	helmgetter "helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/registry"
	helmrepo "helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/kustomize/kyaml/yaml"
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

type helmRepoChartLoader struct {
	loaderConfig
}

func newHelmRepositoryLoader(config loaderConfig) repositoryLoader {
	return &helmRepoChartLoader{loaderConfig: config}
}

func (loader *helmRepoChartLoader) loadRepositoryChart(
	repoNode *yaml.RNode,
	repoURL string,
	parentContext *chartContext,
	chartName string,
	chartVersionSpec string,
) (*chart.Chart, error) {
	var repo sourcev1beta2.HelmRepository
	err := decodeToObject(repoNode, &repo)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to decode HelmRepository %s/%s: %w",
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}

	normalizedURL, err := normalizeURL(repo.Spec.URL)
	if err != nil {
		return nil, fmt.Errorf(
			"invalid Helm repository URL %s: %w",
			repo.Spec.URL,
			err,
		)
	}

	return loader.loadChartByURL(
		normalizedURL,
		chartName,
		chartVersionSpec,
	)
}

func (loader *helmRepoChartLoader) loadChartByURL(
	repoURL string,
	chartName string,
	chartVersionSpec string,
) (*chart.Chart, error) {
	loader.logger.
		With(
			"repoURL", repoURL,
			"name", chartName,
			"version", chartVersionSpec,
		).
		Debug("Loading chart from Helm repository")

	repoPath, err := getCachePathForRepo(loader.cacheRoot, repoURL)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get cache path for Helm repository %s: %w",
			repoURL,
			err,
		)
	}

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
	version, err := repoIndex.Get(chartName, chartVersionSpec)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get chart %s/%s from Helm repository %s: %w",
			chartName,
			chartVersionSpec,
			repoURL,
			err,
		)
	}

	chartVersion := version.Version
	chartKey := fmt.Sprintf("%s#%s#%s", repoURL, chartName, chartVersion)
	if loader.chartCache != nil {
		if chart, ok := loader.chartCache[chartKey]; ok {
			loader.logger.
				With(
					"repoURL", repoURL,
					"name", chartName,
					"version", chartVersion,
				).
				Debug("Using chart from in-memory cache")
			return chart, nil
		}
	}

	parsedURL, err := url.Parse(version.URLs[0])
	if err != nil {
		return nil, fmt.Errorf(
			"unable to parse chart URL %s: %w",
			version.URLs[0],
			err,
		)
	}

	getter, err := getters.ByScheme(parsedURL.Scheme)
	if err != nil {
		return nil, fmt.Errorf(
			"unknown scheme %s for chart %s: %w",
			parsedURL.Scheme,
			version.URLs[0],
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
			chartName,
			chartVersionSpec,
			repoURL,
			err,
		)
	}

	err = loadChartDependencies(loader.loaderConfig, chart, nil)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load chart dependencies for %s/%s in %s: %w",
			chartName,
			chart.Metadata.Version,
			repoURL,
			err,
		)
	}

	if loader.chartCache != nil {
		loader.chartCache[chartKey] = chart
	}

	loader.logger.
		With(
			"repoURL", repoURL,
			"name", chartName,
			"version", chart.Metadata.Version,
		).
		Debug("Finished loading chart")
	return chart, nil
}
