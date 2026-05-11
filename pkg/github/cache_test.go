package github

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCacheScopes(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"scp":"Cache.Read:12345 Cache.Write:67890"}`,
	))
	token := "header." + payload + ".sig"

	scopes := parseCacheScopes(token)
	require.NotNil(t, scopes)
	require.Len(t, scopes, 2)
	assert.Equal(t, "Cache.Read", scopes[0].Scope)
	assert.Equal(t, "12345", scopes[0].Permission)
	assert.Equal(t, "Cache.Write", scopes[1].Scope)
	assert.Equal(t, "67890", scopes[1].Permission)
}

func TestParseCacheScopesMultipleActionsResults(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"scp":"Actions.Results:run:job Cache.ReadWrite:11111"}`,
	))
	token := "header." + payload + ".sig"

	scopes := parseCacheScopes(token)
	require.NotNil(t, scopes)
	require.Len(t, scopes, 2)
	assert.Equal(t, "Actions.Results", scopes[0].Scope)
	assert.Equal(t, "run:job", scopes[0].Permission)
	assert.Equal(t, "Cache.ReadWrite", scopes[1].Scope)
	assert.Equal(t, "11111", scopes[1].Permission)
}

func TestParseCacheScopesNoCacheScope(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"scp":"Actions.Results:run:job"}`,
	))
	token := "header." + payload + ".sig"

	scopes := parseCacheScopes(token)
	require.NotNil(t, scopes)
	require.Len(t, scopes, 1)
	assert.Equal(t, "Actions.Results", scopes[0].Scope)
}

func TestParseCacheScopesEmptyScp(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"scp":""}`,
	))
	token := "header." + payload + ".sig"

	scopes := parseCacheScopes(token)
	assert.Nil(t, scopes)
}

func TestParseCacheScopesNoScpClaim(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"other":"value"}`,
	))
	token := "header." + payload + ".sig"

	scopes := parseCacheScopes(token)
	assert.Nil(t, scopes)
}

func TestParseCacheScopesInvalidToken(t *testing.T) {
	scopes := parseCacheScopes("not-a-jwt")
	assert.Nil(t, scopes)
}

func TestParseCacheScopesInvalidPayload(t *testing.T) {
	scopes := parseCacheScopes("header.!!!bad!!!.sig")
	assert.Nil(t, scopes)
}
