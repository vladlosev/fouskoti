package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"sigs.k8s.io/kustomize/kyaml/yaml"

	"github.com/vladlosev/hrval/pkg/repository"
)

type ExpandCommandOptions struct {
	credentialsFileName string
}

const ExpandCommandName = "expand"

func appendDocSeparator(inputs []io.Reader) []io.Reader {
	if len(inputs) > 0 {
		inputs = append(inputs, bytes.NewBufferString("\n---\n"))
	}
	return inputs
}

func NewExpandCommand(options *ExpandCommandOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   ExpandCommandName,
		Short: "Expands HelmRelease objects into generated templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, logger := getContextAndLogger(cmd)
			logger.Info("Staring expand command")
			var inputs []io.Reader
			for _, arg := range args {
				if arg == "-" {
					inputs = append(inputs, os.Stdin)
				} else {
					inputs = appendDocSeparator(inputs)
					file, err := os.Open(arg)
					if err != nil {
						return fmt.Errorf("unable to open input file %s: %w", arg, err)
					}
					defer file.Close()
					inputs = appendDocSeparator(inputs)
					inputs = append(inputs, file)
				}
			}

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

			return repository.ExpandHelmReleases(
				ctx,
				logger,
				credentials,
				io.MultiReader(inputs...),
				os.Stdout,
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
