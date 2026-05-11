package github

import "os"

const (
	EnvGitHubActions      = "GITHUB_ACTIONS"
	EnvGitHubRepository   = "GITHUB_REPOSITORY"
	EnvGitHubToken        = "GITHUB_TOKEN"
	EnvGitHubActor        = "GITHUB_ACTOR"
	EnvGitHubServerURL    = "GITHUB_SERVER_URL"
	EnvGitHubSHA          = "GITHUB_SHA"
	EnvResultsURL         = "ACTIONS_RESULTS_URL"
	EnvRuntimeToken       = "ACTIONS_RUNTIME_TOKEN"

	DefaultRegistry = "ghcr.io"
)

type Config struct {
	IsGitHubActions bool
	Repository      string
	Token           string
	Username        string
	Registry        string
	ServerURL       string
	SHA             string
}

func GetConfig() Config {
	isEnabled := os.Getenv(EnvGitHubActions) == "true"
	repo := os.Getenv(EnvGitHubRepository)
	token := os.Getenv(EnvGitHubToken)
	username := os.Getenv(EnvGitHubActor)
	serverURL := os.Getenv(EnvGitHubServerURL)
	sha := os.Getenv(EnvGitHubSHA)

	return Config{
		IsGitHubActions: isEnabled,
		Repository:      repo,
		Token:           token,
		Username:        username,
		Registry:        DefaultRegistry,
		ServerURL:       serverURL,
		SHA:             sha,
	}
}

func IsGitHubActions() bool {
	return os.Getenv(EnvGitHubActions) == "true"
}