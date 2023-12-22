package repository

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"helm.sh/helm/v3/pkg/repo"
)

func createFileTree(treeRoot string, files map[string]string) error {
	for filePath, content := range files {
		fullPath := path.Join(treeRoot, filePath)
		if err := os.MkdirAll(path.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf(
				"unable to creare directory for test file %s: %w",
				filePath,
				err,
			)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			return fmt.Errorf(
				"unable to write test file %s: %w",
				filePath,
				err,
			)
		}
	}
	return nil
}

func createChartArchive(
	name string,
	version string,
	files map[string]string,
	dir string,
) error {
	chartDir, err := os.MkdirTemp("", "")
	if err != nil {
		return fmt.Errorf(
			"unable to create temp directory for chart %s-%s: %w",
			name,
			version,
			err,
		)
	}
	defer os.RemoveAll(chartDir)
	if err := createFileTree(path.Join(chartDir, name), files); err != nil {
		return fmt.Errorf(
			"unable to create file for chart %s-%s: %w",
			name,
			version,
			err,
		)
	}

	chartArchivePath := path.Join(dir, fmt.Sprintf("%s-%s.tgz", name, version))
	chartArchive, err := os.Create(chartArchivePath)
	if err != nil {
		return fmt.Errorf(
			"unable to create chart archive file %s: %w",
			chartArchivePath,
			err,
		)
	}
	gzipWriter := gzip.NewWriter(chartArchive)
	defer gzipWriter.Close()
	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	curDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf(
			"unable to get the current directory: %s",
			err,
		)
	}
	defer os.Chdir(curDir)
	os.Chdir(chartDir)
	err = filepath.Walk(
		".",
		func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			file, err := os.Open(path)
			if err != nil {
				return fmt.Errorf(
					"unable to open file %s for copying into archive for chart %s-%s: %w",
					path,
					name,
					version,
					err,
				)
			}
			header, err := tar.FileInfoHeader(info, info.Name())
			if err != nil {
				return fmt.Errorf(
					"unable to create tar header for file %s in chart %s-%s: %w",
					path,
					name,
					version,
					err,
				)
			}
			header.Name = path
			if err := tarWriter.WriteHeader(header); err != nil {
				return fmt.Errorf(
					"unable to create tar header for file %s in chart %s-%s: %w",
					path,
					name,
					version,
					err,
				)
			}
			_, err = io.Copy(tarWriter, file)
			if err != nil {
				return fmt.Errorf(
					"unable to write file %s into archive for chart %s-%s: %w",
					path,
					name,
					version,
					err,
				)
			}
			return nil
		},
	)
	return err
}

func indexRepository(dir string, port int) error {
	indexPath := path.Join(dir, "index.yaml")

	repoUrl := fmt.Sprintf("http://localhost:%d", port)
	index, err := repo.IndexDirectory(dir, repoUrl)
	if err != nil {
		return fmt.Errorf(
			"unable to index charts in %s: %w",
			dir,
			err,
		)
	}
	index.SortEntries()
	if err := index.WriteFile(indexPath, 0644); err != nil {
		return fmt.Errorf(
			"unable to write index file %s: %w",
			indexPath,
			err,
		)
	}
	return nil
}

func createSingleChartRepository(
	chartName string,
	chartVersion string,
	files map[string]string,
	port int,
	dir string,
) error {
	err := createChartArchive(chartName, chartVersion, files, dir)
	if err != nil {
		return fmt.Errorf(
			"unable to create chart archive for %s-%s in %s: %w",
			chartName,
			chartVersion,
			dir,
			err,
		)
	}
	if err = indexRepository(dir, port); err != nil {
		return fmt.Errorf(
			"unable to index repository for chart %s-%s in %s: %w",
			chartName,
			chartVersion,
			dir,
			err,
		)
	}
	return nil
}

func serveDirectory(
	dir string,
	done chan<- struct{},
	logger *slog.Logger,
) (*http.Server, int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, fmt.Errorf(
			"unable to listen on the loopback interface: %w",
			err,
		)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	server := &http.Server{
		Handler: http.FileServer(http.Dir(dir)),
	}
	go func() {
		if err := server.Serve(listener); err != nil {
			if logger != nil {
				logger.With("error", err, "port", port).Error("unable to serve http")
			}
		}
		close(done)
	}()
	return server, port, nil
}

func stopServing(server *http.Server, done <-chan struct{}) error {
	if err := server.Shutdown(context.Background()); err != nil {
		return fmt.Errorf("unable to shut down the server: %w", err)
	}
	<-done
	return nil
}

func TestAll(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	format.TruncatedDiff = false
	ginkgo.RunSpecs(t, "Repository Test Suite")
}

var _ = ginkgo.Describe("HelmRelease expansion check", func() {
	var g gomega.Gomega
	var ctx context.Context
	var logger *slog.Logger

	ginkgo.BeforeEach(func() {
		g = gomega.NewWithT(ginkgo.GinkgoT())
		ctx = context.Background()
		handler := slog.NewTextHandler(
			ginkgo.GinkgoWriter,
			&slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug},
		)
		logger = slog.New(handler)
	})

	ginkgo.It("expands HelmRelease from a chart in a Helm repository", func() {
		chartFiles := map[string]string{
			"Chart.yaml": strings.Join([]string{
				"apiVersion: v2",
				"name: test-chart",
				"version: 0.1.0",
			}, "\n"),
			"values.yaml": strings.Join([]string{
				"data:",
				"  foo: bar",
			}, "\n"),
			"templates/configmap.yaml": strings.Join([]string{
				"apiVersion: v1",
				"kind: ConfigMap",
				"metadata:",
				"  namespace: {{ .Release.Namespace }}",
				"  name: {{ .Release.Name }}-configmap",
				"data: {{- .Values.data | toYaml | nindent 2 }}",
			}, "\n"),
		}
		repoRoot, err := os.MkdirTemp("", "")
		g.Expect(err).ToNot(gomega.HaveOccurred())
		defer os.RemoveAll(repoRoot)
		serverDone := make(chan struct{})
		server, port, err := serveDirectory(repoRoot, serverDone, logger)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		err = createSingleChartRepository(
			"test-chart",
			"0.1.0",
			chartFiles,
			port,
			repoRoot,
		)
		input := strings.Join([]string{
			"apiVersion: helm.toolkit.fluxcd.io/v2beta2",
			"kind: HelmRelease",
			"metadata:",
			"  namespace: testns",
			"  name: test",
			"spec:",
			"  chart:",
			"    spec:",
			"      chart: test-chart",
			"      version: \">=0.1.0\"",
			"      sourceRef:",
			"        kind: HelmRepository",
			"        name: local",
			"  values:",
			"    data:",
			"      foo: baz",
			"---",
			"apiVersion: source.toolkit.fluxcd.io/v1beta2",
			"kind: HelmRepository",
			"metadata:",
			"  namespace: testns",
			"  name: local",
			"spec:",
			fmt.Sprintf("  url: http://localhost:%d", port),
		}, "\n")
		g.Expect(err).ToNot(gomega.HaveOccurred())
		output := &bytes.Buffer{}
		err = ExpandHelmReleases(
			ctx,
			logger,
			Credentials{},
			bytes.NewBufferString(input),
			output,
		)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		stopServing(server, serverDone)
		g.Expect(err).ToNot(gomega.HaveOccurred())
		g.Expect(output.String()).To(gomega.Equal(strings.Join([]string{
			input,
			"---",
			"# Source: test-chart/templates/configmap.yaml",
			"apiVersion: v1",
			"kind: ConfigMap",
			"metadata:",
			"  namespace: testns",
			"  name: testns-test-configmap",
			"data:",
			"  foo: baz",
			"",
		}, "\n"),
		))
	})
})
