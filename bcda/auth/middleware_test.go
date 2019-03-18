package auth_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"

	"github.com/dgrijalva/jwt-go"
	"github.com/go-chi/chi"
	"github.com/pborman/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/CMSgov/bcda-app/bcda/auth"
	"github.com/CMSgov/bcda-app/bcda/database"
	"github.com/CMSgov/bcda-app/bcda/models"
	"github.com/CMSgov/bcda-app/bcda/testUtils"
)

var mockHandler http.HandlerFunc = func(w http.ResponseWriter, r *http.Request) {}

type MiddlewareTestSuite struct {
	testUtils.AuthTestSuite
	server *httptest.Server
	rr     *httptest.ResponseRecorder
	token  string
	ad     auth.AuthData
}

func (s *MiddlewareTestSuite) CreateRouter() http.Handler {
	router := chi.NewRouter()
	router.Use(auth.ParseToken)
	router.With(auth.RequireTokenAuth).Get("/", func(w http.ResponseWriter, r *http.Request) {
		_, err := w.Write([]byte("Test router"))
		if err != nil {
			log.Fatal(err)
		}
	})

	return router
}

func (s *MiddlewareTestSuite) SetupTest() {
	s.SetupAuthBackend()
	userID := "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"
	acoID := "DBBD1CE1-AE24-435C-807D-ED45953077D3"
	tokenID := "d63205a8-d923-456b-a01b-0992fcb40968"
	s.token, _ = auth.TokenStringWithIDs(tokenID, userID, acoID)
	s.ad = auth.AuthData{
		TokenID: tokenID,
		UserID:  userID,
		ACOID:   acoID,
	}
	s.server = httptest.NewServer(s.CreateRouter())
	s.rr = httptest.NewRecorder()
}

func (s *MiddlewareTestSuite) TearDownTest() {
	for _, f := range s.TmpFiles {
		os.Remove(f)
	}
	s.server.Close()
}

func (s *MiddlewareTestSuite) TestRequireTokenAuthInvalidSigning() {
	client := s.server.Client()
	badToken := "eyJhbGciOiJFUzM4NCIsInR5cCI6IkpXVCIsImtpZCI6ImlUcVhYSTB6YkFuSkNLRGFvYmZoa00xZi02ck1TcFRmeVpNUnBfMnRLSTgifQ.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiYWRtaW4iOnRydWUsImlhdCI6MTUxNjIzOTAyMn0.cJOP_w-hBqnyTsBm3T6lOE5WpcHaAkLuQGAs1QO-lg2eWs8yyGW8p9WagGjxgvx7h9X72H7pXmXqej3GdlVbFmhuzj45A9SXDOAHZ7bJXwM1VidcPi7ZcrsMSCtP1hiN"

	req, err := http.NewRequest("GET", s.server.URL, nil)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", badToken))
	resp, err := client.Do(req)

	fmt.Println(resp.StatusCode)
	assert.Equal(s.T(), 401, resp.StatusCode)
	assert.Nil(s.T(), err)
}

func (s *MiddlewareTestSuite) TestRequireTokenAuthInvalid() {
	req, err := http.NewRequest("GET", s.server.URL, nil)
	if err != nil {
		log.Fatal(err)
	}

	handler := auth.RequireTokenAuth(mockHandler)

	acoID, userID, tokenID := uuid.NewRandom().String(), uuid.NewRandom().String(), uuid.NewRandom().String()
	tokenString, err := auth.TokenStringWithIDs(userID, acoID, tokenID)
	assert.Nil(s.T(), err)

	token, err := auth.GetProvider().DecodeJWT(tokenString)
	assert.Nil(s.T(), err)
	assert.NotNil(s.T(), token)
	token.Valid = false

	ad := auth.AuthData{
		UserID:  userID,
		ACOID:   acoID,
		TokenID: tokenID,
	}

	ctx := req.Context()
	ctx = context.WithValue(ctx, "token", token)
	ctx = context.WithValue(ctx, "ad", ad)
	req = req.WithContext(ctx)
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), 401, s.rr.Code)
}

func (s *MiddlewareTestSuite) TestRequireTokenAuthBlackListed() {
	req, err := http.NewRequest("GET", s.server.URL, nil)
	if err != nil {
		log.Fatal(err)
	}

	handler := auth.RequireTokenAuth(mockHandler)

	// Blacklisted Token test
	db := database.GetGORMDbConnection()
	defer database.Close(db)
	// using fixture data
	userID := "EFE6E69A-CD6B-4335-A2F2-4DBEDCCD3E73"
	acoID := "DBBD1CE1-AE24-435C-807D-ED45953077D3"
	var user models.User
	if db.Find(&user, "UUID = ?", userID).RecordNotFound() {
		assert.NotNil(s.T(), errors.New("unable to find User"))
	}
	var aco models.ACO
	if db.Find(&aco, "UUID = ?", acoID).RecordNotFound() {
		assert.NotNil(s.T(), errors.New("unable to find ACO"))
	}

	notActiveToken := jwt.New(jwt.SigningMethodRS512)
	notActiveToken.Claims = jwt.MapClaims{
		"exp": 12345,
		"iat": 123,
		"sub": userID,
		"aco": acoID,
		"id":  "f5bd210a-5f95-4ba6-a167-2e9c95b5fbc1",
	}

	// The actual test
	ctx := req.Context()
	ctx = context.WithValue(ctx, "token", notActiveToken)
	req = req.WithContext(ctx)
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), 401, s.rr.Code)

}

func (s *MiddlewareTestSuite) TestRequireTokenAuthValid() {
	client := s.server.Client()

	// Valid token should return a 200 response
	req, err := http.NewRequest("GET", s.server.URL, nil)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", s.token))

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	assert.Equal(s.T(), 200, resp.StatusCode)
}

func (s *MiddlewareTestSuite) TestRequireTokenAuthEmpty() {
	client := s.server.Client()

	// Valid token should return a 200 response
	req, err := http.NewRequest("GET", s.server.URL, nil)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("Authorization", "")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	assert.Equal(s.T(), 401, resp.StatusCode)
}

func (s *MiddlewareTestSuite) TestRequireTokenJobMatchNotEqual() {
	db := database.GetGORMDbConnection()
	defer database.Close(db)

	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Failed",
	}

	db.Save(&j)
	jobID := strconv.Itoa(int(j.ID))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)

	req, err := http.NewRequest("GET", s.server.URL, nil)
	if err != nil {
		log.Fatal(err)
	}

	handler := auth.RequireTokenJobMatch(mockHandler)

	acoID, userID, tokenID := uuid.NewRandom().String(), uuid.NewRandom().String(), uuid.NewRandom().String()
	tokenString, err := auth.TokenStringWithIDs(tokenID, userID, acoID)
	assert.Nil(s.T(), err)

	token, err := s.AuthBackend.GetJWToken(tokenString)
	assert.Nil(s.T(), err)
	assert.NotNil(s.T(), token)

	ctx := req.Context()
	// ctx = context.WithValue(ctx, "token", token)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), 404, s.rr.Code)
}

func (s *MiddlewareTestSuite) TestRequireTokenJobMatchEqual() {
	db := database.GetGORMDbConnection()
	defer database.Close(db)

	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Failed",
	}

	db.Save(&j)
	jobID := strconv.Itoa(int(j.ID))
	req, err := http.NewRequest("GET", s.server.URL, nil)
	if err != nil {
		log.Fatal(err)
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)

	handler := auth.RequireTokenJobMatch(mockHandler)

	acoID, userID, tokenID := "DBBD1CE1-AE24-435C-807D-ED45953077D3", uuid.NewRandom().String(), uuid.NewRandom().String()
	tokenString, err := auth.TokenStringWithIDs(tokenID, userID, acoID)
	assert.Nil(s.T(), err)

	token, err := auth.GetProvider().DecodeJWT(tokenString)
	assert.Nil(s.T(), err)
	assert.NotNil(s.T(), token)

	ctx := context.WithValue(req.Context(), "token", token)
	ad := auth.AuthData{
		ACOID:   acoID,
		UserID:  userID,
		TokenID: tokenID,
	}
	ctx = context.WithValue(ctx, "ad", ad)

	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), 200, s.rr.Code)
}

func (s *MiddlewareTestSuite) TestRequireTokenACOMatchNoClaims() {
	db := database.GetGORMDbConnection()
	defer database.Close(db)

	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Failed",
	}

	db.Save(&j)
	jobID := strconv.Itoa(int(j.ID))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)

	req, err := http.NewRequest("GET", s.server.URL, nil)
	if err != nil {
		log.Fatal(err)
	}

	handler := auth.RequireTokenJobMatch(mockHandler)

	acoID, userID, tokenID := uuid.NewRandom().String(), uuid.NewRandom().String(), uuid.NewRandom().String()
	tokenString, err := auth.TokenStringWithIDs(tokenID, userID, acoID)
	assert.Nil(s.T(), err)

	token, err := auth.GetProvider().DecodeJWT(tokenString)
	assert.Nil(s.T(), err)
	assert.NotNil(s.T(), token)
	token.Claims = nil

	ctx := req.Context()
	ctx = context.WithValue(ctx, "token", token)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), 404, s.rr.Code)
}

func TestMiddlewareTestSuite(t *testing.T) {
	suite.Run(t, new(MiddlewareTestSuite))
}
