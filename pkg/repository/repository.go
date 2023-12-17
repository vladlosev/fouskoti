package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	helmv2beta2 "github.com/fluxcd/helm-controller/api/v2beta2"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

type repositoryLoader interface {
	loadRepositoryChart(
		repoNode *yaml.RNode,
		chartName string,
		chartVersion string,
	) (*chart.Chart, error)
	loadChartByURL(
		repoURL string,
		chartName string,
		chartVersion string,
	) (*chart.Chart, error)
}

type Credentials map[string]map[string][]byte

type loaderConfig struct {
	ctx         context.Context
	logger      *slog.Logger
	cacheRoot   string
	credentials Credentials
}

type repositoryLoaderFactory func(config loaderConfig) repositoryLoader

func getRepoFactory(
	repoNode *yaml.RNode,
) (repositoryLoaderFactory, error) {
	var result repositoryLoaderFactory

	switch repoNode.GetKind() {
	case "HelmRepository":
		result = newHelmRepositoryLoader
	case "GitRepository":
		result = newGitRepositoryLoader
	case "OCIRepository":
		result = newOciRepositoryLoader
	default:
		return nil, fmt.Errorf(
			"unknown kind %s for repository %s/%s",
			repoNode.GetKind(),
			repoNode.GetNamespace(),
			repoNode.GetName(),
		)
	}
	return result, nil
}

func getRepoFactoryByURL(repoURL string) (repositoryLoaderFactory, error) {
	var result repositoryLoaderFactory

	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse chart repository URL %s", err)
	}

	switch parsedURL.Scheme {
	case "https", "http":
		if parsedURL.User.Username() == "git" {
			result = newGitRepositoryLoader
		} else {
			result = newHelmRepositoryLoader
		}
	case "ssh":
		result = newGitRepositoryLoader
	case "oci":
		result = newOciRepositoryLoader
	default:
		return nil, fmt.Errorf("unknown type for repository URL %s", repoURL)
	}
	return result, nil
}

func getLoaderForRepo(
	repoNode *yaml.RNode,
	config loaderConfig,
) (repositoryLoader, error) {
	factory, err := getRepoFactory(repoNode)
	if err != nil {
		return nil, err
	}

	return factory(config), nil
}

func getLoaderForRepoURL(
	repoURL string,
	config loaderConfig,
) (repositoryLoader, error) {
	factory, err := getRepoFactoryByURL(repoURL)
	if err != nil {
		return nil, err
	}

	return factory(config), nil
}

func decodeToObject(node *yaml.RNode, out runtime.Object) error {
	bytes, err := node.MarshalJSON()
	if err != nil {
		return fmt.Errorf("unable to encode node to JSON: %w", err)
	}
	err = json.Unmarshal(bytes, out)
	if err != nil {
		return fmt.Errorf("unable to unmarshal JSON to k8s object: %w", err)
	}
	return nil
}

func getCachePathForRepo(cacheRoot string, repoURL string) (string, error) {
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
	return path.Join(cacheRoot, repoPath), nil
}

// loadRepositoryChart downloads the chart and returns it.
func loadRepositoryChart(
	ctx context.Context,
	logger *slog.Logger,
	credentials Credentials,
	release *helmv2beta2.HelmRelease,
	repoNode *yaml.RNode,
) (*chart.Chart, error) {
	cacheRoot, err := os.MkdirTemp("", "chart-repo-cache-")
	if err != nil {
		return nil, fmt.Errorf(
			"unable to create a cache dir for repo %s/%s/%s: %w",
			repoNode.GetKind(),
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}
	defer os.RemoveAll(cacheRoot) // TODO(vlad): Find way to persist the cache.

	loader, err := getLoaderForRepo(
		repoNode,
		loaderConfig{ctx, logger, cacheRoot, credentials},
	)
	if err != nil {
		return nil, err
	}

	return loader.loadRepositoryChart(
		repoNode,
		release.Spec.Chart.Spec.Chart,
		release.Spec.Chart.Spec.Version,
	)
}

func loadChartDependencies(config loaderConfig, chart *chart.Chart) error {
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

		loader, err := getLoaderForRepoURL(repoURL, config)
		if err != nil {
			return fmt.Errorf(
				"unable to get loader for chart %s/%s in %s (a dependency of %s): %w",
				dependency.Name,
				dependency.Version,
				repoURL,
				chart.Name(),
				err,
			)
		}

		dependencyChart, err := loader.loadChartByURL(
			repoURL,
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

func ExpandHelmRelease(
	ctx context.Context,
	logger *slog.Logger,
	credentials Credentials,
	releaseNode *yaml.RNode,
	repoNode *yaml.RNode,
) ([]*yaml.RNode, error) {
	var release helmv2beta2.HelmRelease
	err := decodeToObject(releaseNode, &release)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to decode HelmRelease: %w",
			err,
		)
	}

	if repoNode == nil {
		return nil, fmt.Errorf(
			"missing chart repository for Helm release %s/%s",
			release.Namespace,
			release.Name,
		)
	}

	chart, err := loadRepositoryChart(ctx, logger, credentials, &release, repoNode)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load chart for %s %s/%s: %w",
			repoNode.GetKind(),
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}

	values, err := chartutil.CoalesceValues(chart, release.GetValues())
	if err != nil {
		return nil, fmt.Errorf(
			"unable to coalesce values from the chart for release %s/%s: %w",
			release.Namespace,
			release.Name,
			err,
		)
	}

	capabilities := chartutil.DefaultCapabilities
	// TODO(vlad): Set k8s version in capabilities.

	metaValues := chartutil.Values{
		"Release": chartutil.Values{
			"Name":      release.Spec.ReleaseName,
			"Namespace": release.Namespace,
			"IsUpgrade": false,
			"IsInstall": true,
			"Revision":  1,
			"Service":   "Helm",
		},
		"Capabilities": capabilities,
		"Values":       values,
	}
	manifests, err := engine.Render(chart, metaValues)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to render values for Helm release %s/%s: %w",
			release.Namespace,
			release.Name,
			err,
		)
	}

	var results []*yaml.RNode
	for key, manifest := range manifests {
		if strings.TrimSpace(manifest) == "" {
			continue
		}
		if filepath.Base(key) == "NOTES.txt" {
			continue
		}
		result, err := yaml.Parse(manifest)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to parse manifest %s from Helm release %s/%s: %w",
				key,
				release.Namespace,
				release.Name,
				err,
			)
		}
		result.YNode().HeadComment = fmt.Sprintf("Source: " + key)
		results = append(results, result)
	}

	return results, nil
}
