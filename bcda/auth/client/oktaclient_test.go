// +build okta

// To enable this test suite:

// 3. Run "go test -tags=okta -v" from the bcda/auth/client directory

package client

import (
	"crypto/rand"
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/pborman/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type OTestSuite struct {
	suite.Suite
	oc *OktaClient
}

func (s *OTestSuite) SetupTest() {
	s.oc = NewOktaClient()
}

func (s *OTestSuite) TestConfig() {
	originalOktaBaseUrl := os.Getenv("OKTA_CLIENT_ORGURL")
	originalOktaServerID := os.Getenv("OKTA_OAUTH_SERVER_ID")
	originalOktaToken := os.Getenv("OKTA_CLIENT_TOKEN")

	os.Unsetenv("OKTA_CLIENT_ORGURL")
	os.Unsetenv("OKTA_OAUTH_SERVER_ID")
	os.Unsetenv("OKTA_CLIENT_TOKEN")

	err := config()
	require.NotNil(s.T(), err)
	assert.Regexp(s.T(), regexp.MustCompile("(OKTA_[A-Z_]*=, ){2}(OKTA_CLIENT_TOKEN=)"), err)

	os.Setenv("OKTA_CLIENT_TOKEN", originalOktaToken)

	err = config()
	assert.NotNil(s.T(), err)
	assert.Regexp(s.T(), regexp.MustCompile("(OKTA_[A-Z_]*=, ){2}(OKTA_CLIENT_TOKEN=\\[Redacted\\])"), err)

	os.Setenv("OKTA_CLIENT_ORGURL", originalOktaBaseUrl)
	os.Setenv("OKTA_OAUTH_SERVER_ID", originalOktaServerID)
	os.Setenv("OKTA_CLIENT_TOKEN", originalOktaToken)

	err = config()
	assert.Nil(s.T(), err)
}

// visually assert logging side effects for now
// {"level":"info","msg":"1 okta public oauth server public keys cached","time":"2019-02-20T13:30:48-08:00"}
// {"level":"warning","msg":"invalid key id not a real key presented","time":"2019-02-20T13:30:48-08:00"}
func (s *OTestSuite) TestPublicKeyFor() {
	// s.oc = NewOktaClient()
	pk, ok := s.oc.PublicKeyFor("not a real key")
	assert.Nil(s.T(), pk.N)
	assert.False(s.T(), ok)
}

func (s *OTestSuite) TestAddClientApplication() {
	rci := randomClientId(6)
	clientID, secret, clientName, err := s.oc.AddClientApplication(rci)
	assert.Nil(s.T(), err)
	assert.NotEmpty(s.T(), clientID)
	assert.NotEmpty(s.T(), secret)
	assert.Equal(s.T(), "BCDA "+rci, clientName)

	_ = s.oc.RemoveClientApplication(clientID)
}

func (s *OTestSuite) TestRequestAccessToken() {
	clientID := os.Getenv("OKTA_CLIENT_ID")
	clientSecret := os.Getenv("OKTA_CLIENT_SECRET")

	assert.NotEmpty(s.T(), clientID, "Test requires OKTA_CLIENT_ID")
	assert.NotEmpty(s.T(), clientSecret, "Test requires OKTA_CLIENT_SECRET")

	t, err := s.oc.RequestAccessToken(Credentials{ClientID: clientID, ClientSecret: clientSecret})
	assert.IsType(s.T(), OktaToken{}, t)
	assert.Nil(s.T(), err)

	t, err = s.oc.RequestAccessToken(Credentials{ClientID: "", ClientSecret: ""})
	assert.IsType(s.T(), OktaToken{}, t)
	assert.NotNil(s.T(), err)
}

func (s *OTestSuite) TestRevokeAccessToken() {
	clientID := os.Getenv("OKTA_CLIENT_ID")
	clientSecret := os.Getenv("OKTA_CLIENT_SECRET")

	assert.NotEmpty(s.T(), clientID, "Test requires OKTA_CLIENT_ID")
	assert.NotEmpty(s.T(), clientSecret, "Test requires OKTA_CLIENT_SECRET")

	creds := Credentials{ClientID: clientID, ClientSecret: clientSecret}
	t, err := s.oc.RequestAccessToken(creds)
	assert.Nil(s.T(), err, "Must be able to get new token to revoke")

	// Success!
	err = s.oc.RevokeAccessToken(creds, t.AccessToken)
	assert.Nil(s.T(), err)

	// No difference in response is seen for second revokation
	err = s.oc.RevokeAccessToken(creds, t.AccessToken)
	assert.Nil(s.T(), err)

	// Surprisingly, an invalid token does not generate an error either
	badTokenString := "not_a_token"
	err = s.oc.RevokeAccessToken(creds, badTokenString)
	assert.Nil(s.T(), err)

	// Bad credentials fail
	badCreds := Credentials{ClientID: "not_a_client_id", ClientSecret: "not_a_client_secret"}
	err = s.oc.RevokeAccessToken(badCreds, t.AccessToken)
	assert.NotNil(s.T(), err)
}

func (s *OTestSuite) TestGenerateNewClientSecret() {
	validClientID := "0oaj4590j9B5uh8rC0h7"
	newSecret, err := s.oc.GenerateNewClientSecret(validClientID)
	assert.Nil(s.T(), err)
	assert.NotEqual(s.T(), "", newSecret)

	invalidClientID := "IDontexist"
	newSecret, err = s.oc.GenerateNewClientSecret(invalidClientID)
	assert.Equal(s.T(), "404 Not Found", err.Error())
}

func (s *OTestSuite) TestDeactivateApplication() {
	newClientID, _, _, _ := s.oc.AddClientApplication("TestDeactivate" + uuid.NewRandom().String())

	err := s.oc.DeactivateApplication(newClientID)
	assert.Nil(s.T(), err, fmt.Sprintf("failed to deactivate application with ID %s", newClientID))

	err = s.oc.RemoveClientApplication(newClientID)
	assert.Nil(s.T(), err, fmt.Sprintf("failed to remove client application with ID %s", newClientID))

	err = s.oc.RemoveClientApplication(newClientID)
	assert.Equal(s.T(), "401 Unauthorized", err.Error())
}

func (s *OTestSuite) TearDownTest() {
}

func TestOTestSuite(t *testing.T) {
	suite.Run(t, new(OTestSuite))
}

func randomClientId(n int) string {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return "not_random"
	}
	return fmt.Sprintf("%x", b)
}
