package repository

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/fluxcd/pkg/git/repository"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"helm.sh/helm/v3/pkg/chart"
	helmloader "helm.sh/helm/v3/pkg/chart/loader"
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
	loader.logger.
		With(
			"repoName", repoNode.GetName(),
			"repoNamespace", repoNode.GetNamespace(),
			"name", chartName,
		).
		Debug("Loading chart from Git repository")

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

	repoURL := repo.Spec.URL
	repoPath, err := getCachePathForRepo(loader.cacheRoot, repoURL)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to get cache path for Git repository %s: %w",
			repoURL,
			err,
		)
	}

	parsedURL, err := url.Parse(repoURL)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to parse URL %s for GitRepository %s/%s: %w",
			repoURL,
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}

	authOpts, err := git.NewAuthOptions(*parsedURL, loader.credentials[repoURL])
	if err != nil {
		return nil, fmt.Errorf(
			"unable to initialize Git auth options for Git repository %s/%s: %w",
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}

	clientOpts := []gogit.ClientOption{
		gogit.WithDiskStorage(),
		gogit.WithSingleBranch(true),
	}

	timeout := 60 * time.Second
	specTimeout := repo.Spec.Timeout
	if specTimeout != nil {
		timeout = specTimeout.Duration
	}

	client, err := gogit.NewClient(repoPath, authOpts, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to create Git client to clone repository %s: %w",
			repoURL,
			err,
		)
	}
	cloneCtx, cancel := context.WithTimeout(loader.ctx, timeout)
	defer cancel()

	cloneOpts := repository.CloneConfig{
		ShallowClone: true,
	}
	if repo.Spec.Reference != nil {
		cloneOpts.CheckoutStrategy = repository.CheckoutStrategy{
			Branch:  repo.Spec.Reference.Branch,
			Tag:     repo.Spec.Reference.Tag,
			SemVer:  repo.Spec.Reference.SemVer,
			RefName: repo.Spec.Reference.Name,
			Commit:  repo.Spec.Reference.Commit,
		}
	}
	_, err = client.Clone(cloneCtx, repoURL, cloneOpts)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to clone Git repository %s: %w",
			repoURL,
			err,
		)
	}

	chart, err := helmloader.LoadDir(path.Join(repoPath, chartName))
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load chart %s from GitRepository %s/%s: %w",
			chartName,
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}

	// TODO(vlad): Handle relative dependency paths here.
	err = loadChartDependencies(loader.loaderConfig, chart)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load chart dependencies for %s/%s in %s: %w",
			chartName,
			chart.Metadata.Version,
			repoURL,
			err,
		)
	}

	loader.logger.
		With(
			"repoName", repoNode.GetName(),
			"repoNamespace", repoNode.GetNamespace(),
			"name", chartName,
			"version", chart.Metadata.Version,
		).
		Debug("Finished loading chart")

	return chart, nil
}

func (loader *gitRepoChartLoader) loadChartByURL(
	repoURL string,
	chartName string,
	chartVersion string,
) (*chart.Chart, error) {
	// TODO(vlad): Implement.
	return nil, fmt.Errorf("not implemented")
}
