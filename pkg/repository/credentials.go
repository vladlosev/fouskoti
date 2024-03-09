package repository

import (
	"bytes"
	"fmt"
	"io"
	"net/url"
	"os"

	"golang.org/x/exp/maps"
	"gopkg.in/yaml.v2"
)

type RepositoryCreds map[string][]byte

func (creds RepositoryCreds) ExpandEnvVars() {
	for _, key := range maps.Keys(creds) {
		value := creds[key]
		if rest, found := bytes.CutPrefix(value, []byte{'$'}); found {
			if len(rest) > 0 {
				creds[key] = []byte(os.Getenv(string(rest)))
			}
		}
	}
}

type Credentials map[string]RepositoryCreds

func ReadCredentials(input io.Reader) (Credentials, error) {
	bytes, err := io.ReadAll(input)
	if err != nil {
		return nil, fmt.Errorf("unable to read input: %w", err)
	}

	credentials := Credentials{}
	err = yaml.Unmarshal(bytes, credentials)
	if err != nil {
		return nil, fmt.Errorf("unable to parse credentials YAML: %w", err)
	}
	return credentials, nil
}

func (credentials Credentials) FindForRepo(
	repoURL *url.URL,
) (*RepositoryCreds, error) {
	if creds, ok := credentials[repoURL.String()]; ok {
		return &creds, nil
	}
	for storedRepoURL, creds := range credentials {
		parsedURL, err := url.Parse(storedRepoURL)
		if err != nil {
			return nil, fmt.Errorf(
				"unable to parse configured repository URL %s:%w",
				storedRepoURL,
				err,
			)
		}
		if repoURL.Scheme == parsedURL.Scheme &&
			repoURL.Host == parsedURL.Host &&
			repoURL.User.Username() == parsedURL.User.Username() {
			return &creds, nil
		}
	}
	return nil, nil
}

func (credentials Credentials) ExpandEnvVars() {
	for _, value := range credentials {
		value.ExpandEnvVars()
	}
}
