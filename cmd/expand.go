package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

type ExpandCommandOptions struct {
}

const ExpandCommandName = "expand"

func readInput(input io.Reader) ([]*yaml.Node, error) {
	result := []*yaml.Node{}
	decoder := yaml.NewDecoder(input)
	for {
		node := &yaml.Node{}
		err := decoder.Decode(node)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("unable to parse YAML: %w", err)
		}
		result = append(result, node)
	}
	return result, nil
}

func writeResults(output io.Writer, nodes []*yaml.Node) error {
	for i, node := range nodes {
		if i != 0 {
			_, err := output.Write([]byte("---\n"))
			if err != nil {
				return fmt.Errorf("unable to write output: %w", err)
			}
		}
		bytes, err := yaml.Marshal(node)
		if err != nil {
			return fmt.Errorf("unable to marshal to YAML: %w", err)
		}
		_, err = output.Write(bytes)
		if err != nil {
			return fmt.Errorf("unable to write output: %w", err)
		}
	}
	return nil
}

func MarshalToJSON(nodes []*yaml.Node) []byte {
	bytes, _ := json.MarshalIndent(nodes, "", "  ")
	return bytes
}

func NewExpandCommand(options *ExpandCommandOptions) *cobra.Command {
	command := &cobra.Command{
		Use:   ExpandCommandName,
		Short: "Expands HelmRelease objects into generated templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, logger := getContextAndLogger(cmd)
			logger.Info("Staring expand command")
			nodes, err := readInput(os.Stdin)
			if err != nil {
				return fmt.Errorf("unable to read input: %w", err)
			}
			os.Stdout.Write(MarshalToJSON(nodes))
			os.Stderr.Write([]byte("\n"))
			err = writeResults(os.Stdout, nodes)
			if err != nil {
				return fmt.Errorf("unable to marshal to YAML: %w", err)
			}
			return nil
		},
		SilenceUsage: true,
	}
	return command
}
