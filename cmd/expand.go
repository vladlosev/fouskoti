package cmd

import (
	"fmt"
	"os"

	"github.com/fluxcd/pkg/git"
	"github.com/fluxcd/pkg/git/gogit"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/vladlosev/fouskoti/pkg/repository"
)

type ExpandCommandOptions struct {
	credentialsFileName string
}

const ExpandCommandName = "expand"

func NewExpandCommand(options *ExpandCommandOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   ExpandCommandName,
		Short: "Expands HelmRelease objects into generated templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, logger := getContextAndLogger(cmd)
			logger.Info("Staring expand command")
			input, err := getYAMLInputReader(args)
			if err != nil {
				return err
			}
			defer input.Close()

			stringCreds := map[string]map[string]string{}
			credentials := repository.Credentials{}

			if options.credentialsFileName != "" {
				creds, err := os.ReadFile(options.credentialsFileName)
				if err != nil {
					return fmt.Errorf(
						"unable to open credentials file %s: %w",
						options.credentialsFileName,
						err,
					)
				}
				err = yaml.Unmarshal(creds, stringCreds)
				if err != nil {
					return fmt.Errorf(
						"unable to parse credentials file %s as YAML: %w",
						options.credentialsFileName,
						err,
					)
				}

				for repo, items := range stringCreds {
					credsItem := map[string][]byte{}
					for name, content := range items {
						credsItem[name] = []byte(content)
					}
					credentials[repo] = credsItem
				}
			}

			expander := repository.NewHelmReleaseExpander(
				ctx,
				logger,
				func(
					path string,
					authOpts *git.AuthOptions,
					clientOpts ...gogit.ClientOption,
				) (repository.GitClientInterface, error) {
					return gogit.NewClient(path, authOpts, clientOpts...)
				},
			)
			return expander.ExpandHelmReleases(
				credentials,
				input,
				os.Stdout,
				true,
			)
		},
		SilenceUsage: true,
	}
	command.PersistentFlags().StringVarP(
		&options.credentialsFileName,
		"credentials-file",
		"",
		"",
		"Name of the repository credentials file",
	)

	return command
}
