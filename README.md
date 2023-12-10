# Helm Release Value Expander



### Plan
- [ ] Implement a CI pipeline linting the source code
- [x] Get input YAML loading and filtered for `HelmRelease` objects.
- [x] Add filtering of corresponding `HelmRepository`/`GitRepository` objects.
- [x] Research possibility of reusing Helm code for expanding `HelmRelease` objects.
- [x] Add expansion of `HelmRelease` objects and appending them to the output.
      - [x] Implement basic chart expansion
- [ ] Add Docker image building and pushing to DockerHub.
- [ ] Write the README content describing the program and its usage.