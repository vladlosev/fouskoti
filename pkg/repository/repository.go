package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	helmv2beta1 "github.com/fluxcd/helm-controller/api/v2beta1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	sourcev1beta2 "github.com/fluxcd/source-controller/api/v1beta2"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

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

// loadRepositoryChart downloads the chart and returns it.
func loadRepositoryChart(
	ctx context.Context,
	logger *slog.Logger,
	release *helmv2beta1.HelmRelease,
	repoNode *yaml.RNode,
) (*chart.Chart, error) {
	switch repoNode.GetKind() {
	case "HelmRepository":
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
		return loadHelmRepositoryChart(ctx, logger, release, &repo)
	case "GitRepository":
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
		return loadGitRepositoryChart(ctx, release, &repo)
	case "OCIRepository":
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
		return loadOciRepositoryChart(ctx, release, &repo)
	default:
		return nil, fmt.Errorf(
			"unknown kind %s for repository %s/%s",
			repoNode.GetKind(),
			repoNode.GetNamespace(),
			repoNode.GetName(),
		)
	}
}

func ExpandHelmRelease(
	ctx context.Context,
	logger *slog.Logger,
	releaseNode *yaml.RNode,
	repoNode *yaml.RNode,
) ([]*yaml.RNode, error) {
	var release helmv2beta1.HelmRelease
	var repo sourcev1beta2.HelmRepository

	err := decodeToObject(repoNode, &repo)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to decode HelmRepository: %w",
			err,
		)
	}

	err = decodeToObject(releaseNode, &release)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to decode HelmRelease: %w",
			err,
		)
	}

	chart, err := loadRepositoryChart(ctx, logger, &release, repoNode)
	if err != nil {
		return nil, fmt.Errorf(
			"unable to load chart for %s %s/%s: %w",
			repoNode.GetKind(),
			repoNode.GetNamespace(),
			repoNode.GetName(),
			err,
		)
	}
	// TODO(vlad): Add dependency support.
	//dlManager := downloader.Manager{
	//	ChartPath: "",
	//}

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
