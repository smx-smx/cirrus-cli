package github

import "os"

const (
	EnvGitHubActions    = "GITHUB_ACTIONS"
	EnvGitHubRepository = "GITHUB_REPOSITORY"
	EnvGitHubToken      = "GITHUB_TOKEN"
	EnvGitHubActor      = "GITHUB_ACTOR"

	DefaultRegistry = "ghcr.io"
)

type Config struct {
	IsGitHubActions bool
	Repository      string
	Token           string
	Username        string
	Registry        string
}

func GetConfig() Config {
	isEnabled := os.Getenv(EnvGitHubActions) == "true"
	repo := os.Getenv(EnvGitHubRepository)
	token := os.Getenv(EnvGitHubToken)
	username := os.Getenv(EnvGitHubActor)

	return Config{
		IsGitHubActions: isEnabled,
		Repository:      repo,
		Token:           token,
		Username:        username,
		Registry:        DefaultRegistry,
	}
}

func IsGitHubActions() bool {
	return os.Getenv(EnvGitHubActions) == "true"
}