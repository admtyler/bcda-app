package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/CMSgov/bcda-app/bcda/auth"
	"github.com/CMSgov/bcda-app/bcda/encryption"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/CMSgov/bcda-app/bcda/database"
	"github.com/CMSgov/bcda-app/bcda/models"
	"github.com/CMSgov/bcda-app/bcda/responseutils"
	"github.com/CMSgov/bcda-app/bcda/testUtils"
	que "github.com/bgentry/que-go"
	jwt "github.com/dgrijalva/jwt-go"
	fhirmodels "github.com/eug48/fhir/models"
	"github.com/go-chi/chi"
	"github.com/jackc/pgx"
	"github.com/jinzhu/gorm"
	"github.com/pborman/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type APITestSuite struct {
	testUtils.AuthTestSuite
	rr *httptest.ResponseRecorder
	db *gorm.DB
}

func (s *APITestSuite) SetupTest() {
	models.InitializeGormModels()
	auth.InitializeGormModels()
	s.db = database.GetGORMDbConnection()
	s.rr = httptest.NewRecorder()
}

func (s *APITestSuite) TearDownTest() {
	database.Close(s.db)
}

func (s *APITestSuite) TestBulkEOBRequest() {
	acoID := "0c527d2e-2e8a-4808-b11d-0fa06baf8254"
	user, err := models.CreateUser("api.go Test User", "testbulkeobrequest@example.com", uuid.Parse(acoID))
	if err != nil {
		s.T().Error(err)
	}

	token := jwt.New(jwt.SigningMethodRS512)
	token.Claims = jwt.MapClaims{
		"sub": user.UUID.String(),
		"aco": acoID,
		"id":  uuid.NewRandom().String(),
	}
	token.Valid = true

	req := httptest.NewRequest("GET", "/api/v1/test/ExplanationOfBenefit/$export", nil)
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	queueDatabaseURL := os.Getenv("QUEUE_DATABASE_URL")
	pgxcfg, err := pgx.ParseURI(queueDatabaseURL)
	if err != nil {
		s.T().Error(err)
	}

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:   pgxcfg,
		AfterConnect: que.PrepareStatements,
	})
	if err != nil {
		s.T().Error(err)
	}
	defer pgxpool.Close()

	qc = que.NewClient(pgxpool)

	handler := http.HandlerFunc(bulkEOBRequest)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusAccepted, s.rr.Code)
	s.db.Where("user_id = ?", user.UUID).Delete(models.Job{})
	s.db.Where("uuid = ?", user.UUID).Delete(models.User{})
}

func (s *APITestSuite) TestBulkEOBRequestNoBeneficiariesInACO() {
	s.SetupAuthBackend()

	userID := "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"
	acoID := "DBBD1CE1-AE24-435C-807D-ED45953077D3"

	token := jwt.New(jwt.SigningMethodRS512)
	token.Claims = jwt.MapClaims{
		"sub": userID,
		"aco": acoID,
		"id":  uuid.NewRandom().String(),
	}
	token.Valid = true

	req := httptest.NewRequest("GET", "/api/v1/ExplanationOfBenefit/$export", nil)
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	queueDatabaseURL := os.Getenv("QUEUE_DATABASE_URL")
	pgxcfg, err := pgx.ParseURI(queueDatabaseURL)
	if err != nil {
		s.T().Error(err)
	}

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:   pgxcfg,
		AfterConnect: que.PrepareStatements,
	})
	if err != nil {
		s.T().Error(err)
	}
	defer pgxpool.Close()

	qc = que.NewClient(pgxpool)

	handler := http.HandlerFunc(bulkEOBRequest)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusAccepted, s.rr.Code)
}

func (s *APITestSuite) TestBulkEOBRequestMissingToken() {
	req := httptest.NewRequest("GET", "/api/v1/ExplanationOfBenefit/$export", nil)

	handler := http.HandlerFunc(bulkEOBRequest)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusUnauthorized, s.rr.Code)

	var respOO fhirmodels.OperationOutcome
	err := json.Unmarshal(s.rr.Body.Bytes(), &respOO)
	if err != nil {
		s.T().Error(err)
	}

	assert.Equal(s.T(), responseutils.Error, respOO.Issue[0].Severity)
	assert.Equal(s.T(), responseutils.Exception, respOO.Issue[0].Code)
	assert.Equal(s.T(), responseutils.TokenErr, respOO.Issue[0].Details.Coding[0].Display)
}

func (s *APITestSuite) TestBulkEOBRequestUserDoesNotExist() {
	s.SetupAuthBackend()

	acoID := "dbbd1ce1-ae24-435c-807d-ed45953077d3"
	subID := "82503a18-bf3b-436d-ba7b-bae09b7ffdff"
	tokenID := "665341c9-7d0c-4844-b66f-5910d9d0822f"

	token := jwt.New(jwt.SigningMethodRS512)
	token.Claims = jwt.MapClaims{
		"sub": subID,
		"aco": acoID,
		"id":  tokenID,
	}
	token.Valid = true

	req := httptest.NewRequest("GET", "/api/v1/ExplanationOfBenefit/$export", nil)
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler := http.HandlerFunc(bulkEOBRequest)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusInternalServerError, s.rr.Code)

	var respOO fhirmodels.OperationOutcome
	err := json.Unmarshal(s.rr.Body.Bytes(), &respOO)
	if err != nil {
		s.T().Error(err)
	}

	assert.Equal(s.T(), responseutils.Error, respOO.Issue[0].Severity)
	assert.Equal(s.T(), responseutils.Exception, respOO.Issue[0].Code)
	assert.Equal(s.T(), responseutils.DbErr, respOO.Issue[0].Details.Coding[0].Display)
}

func (s *APITestSuite) TestBulkEOBRequestNoQueue() {
	qc = nil
	s.SetupAuthBackend()

	acoID := "0c527d2e-2e8a-4808-b11d-0fa06baf8254"
	user, err := models.CreateUser("api.go Test User", "testbulkrequestnoqueue@example.com", uuid.Parse(acoID))
	if err != nil {
		s.T().Error(err)
	}
	defer s.db.Where("uuid = ?", user.UUID).Delete(models.User{})

	token := jwt.New(jwt.SigningMethodRS512)
	token.Claims = jwt.MapClaims{
		"sub": user.UUID.String(),
		"aco": acoID,
		"id":  uuid.NewRandom().String(),
	}
	token.Valid = true

	req := httptest.NewRequest("GET", "/api/v1/ExplanationOfBenefit/$export", nil)
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler := http.HandlerFunc(bulkEOBRequest)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusInternalServerError, s.rr.Code)

	var respOO fhirmodels.OperationOutcome
	err = json.Unmarshal(s.rr.Body.Bytes(), &respOO)
	if err != nil {
		s.T().Error(err)
	}

	assert.Equal(s.T(), responseutils.Error, respOO.Issue[0].Severity)
	assert.Equal(s.T(), responseutils.Exception, respOO.Issue[0].Code)
	assert.Equal(s.T(), responseutils.Processing, respOO.Issue[0].Details.Coding[0].Display)
}

func (s *APITestSuite) TestBulkPatientRequest() {
	s.SetupAuthBackend()

	origPtExp := os.Getenv("ENABLE_PATIENT_EXPORT")
	os.Setenv("ENABLE_PATIENT_EXPORT", "true")

	acoID := "0c527d2e-2e8a-4808-b11d-0fa06baf8254"
	user, err := models.CreateUser("api.go Test User", "testbulkpatientrequest@example.com", uuid.Parse(acoID))
	if err != nil {
		s.T().Error(err)
	}

	defer func() {
		os.Setenv("ENABLE_PATIENT_EXPORT", origPtExp)
		s.db.Where("user_id = ?", user.UUID).Delete(models.Job{})
		s.db.Where("uuid = ?", user.UUID).Delete(models.User{})
	}()

	token := jwt.New(jwt.SigningMethodRS512)
	token.Claims = jwt.MapClaims{
		"sub": user.UUID.String(),
		"aco": acoID,
		"id":  uuid.NewRandom().String(),
	}
	token.Valid = true

	req := httptest.NewRequest("GET", "/api/v1/test/Patient/$export", nil)
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	queueDatabaseURL := os.Getenv("QUEUE_DATABASE_URL")
	pgxcfg, err := pgx.ParseURI(queueDatabaseURL)
	if err != nil {
		s.T().Error(err)
	}

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:   pgxcfg,
		AfterConnect: que.PrepareStatements,
	})
	if err != nil {
		s.T().Error(err)
	}
	defer pgxpool.Close()

	qc = que.NewClient(pgxpool)

	handler := http.HandlerFunc(bulkPatientRequest)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusAccepted, s.rr.Code)
}

func (s *APITestSuite) TestBulkCoverageRequest() {
	s.SetupAuthBackend()

	origPtExp := os.Getenv("ENABLE_COVERAGE_EXPORT")
	os.Setenv("ENABLE_COVERAGE_EXPORT", "true")

	acoID := "0c527d2e-2e8a-4808-b11d-0fa06baf8254"
	user, err := models.CreateUser("api.go Test User", "testbulkcoveragerequest@example.com", uuid.Parse(acoID))
	if err != nil {
		s.T().Error(err)
	}

	defer func() {
		os.Setenv("ENABLE_COVERAGE_EXPORT", origPtExp)
		s.db.Where("user_id = ?", user.UUID).Delete(models.Job{})
		s.db.Where("uuid = ?", user.UUID).Delete(models.User{})
	}()

	token := jwt.New(jwt.SigningMethodRS512)
	token.Claims = jwt.MapClaims{
		"sub": user.UUID.String(),
		"aco": acoID,
		"id":  uuid.NewRandom().String(),
	}
	token.Valid = true

	req := httptest.NewRequest("GET", "/api/v1/test/Coverage/$export", nil)
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	queueDatabaseURL := os.Getenv("QUEUE_DATABASE_URL")
	pgxcfg, err := pgx.ParseURI(queueDatabaseURL)
	if err != nil {
		s.T().Error(err)
	}

	pgxpool, err := pgx.NewConnPool(pgx.ConnPoolConfig{
		ConnConfig:   pgxcfg,
		AfterConnect: que.PrepareStatements,
	})
	if err != nil {
		s.T().Error(err)
	}
	defer pgxpool.Close()

	qc = que.NewClient(pgxpool)

	handler := http.HandlerFunc(bulkCoverageRequest)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusAccepted, s.rr.Code)
}

func (s *APITestSuite) TestBulkRequestInvalidType() {
	req := httptest.NewRequest("GET", "/api/v1/test/Foo/$export", nil)

	bulkRequest("Foo", s.rr, req)

	assert.Equal(s.T(), http.StatusBadRequest, s.rr.Code)
}

func (s *APITestSuite) TestJobStatusInvalidJobID() {
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%s", "test"), nil)

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", "test")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	var respOO fhirmodels.OperationOutcome
	err := json.Unmarshal(s.rr.Body.Bytes(), &respOO)
	if err != nil {
		s.T().Error(err)
	}

	assert.Equal(s.T(), responseutils.Error, respOO.Issue[0].Severity)
	assert.Equal(s.T(), responseutils.Exception, respOO.Issue[0].Code)
	assert.Equal(s.T(), responseutils.DbErr, respOO.Issue[0].Details.Coding[0].Display)
}

func (s *APITestSuite) TestJobStatusJobDoesNotExist() {
	jobID := "1234"
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%s", jobID), nil)

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", jobID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	var respOO fhirmodels.OperationOutcome
	err := json.Unmarshal(s.rr.Body.Bytes(), &respOO)
	if err != nil {
		s.T().Error(err)
	}

	assert.Equal(s.T(), responseutils.Error, respOO.Issue[0].Severity)
	assert.Equal(s.T(), responseutils.Exception, respOO.Issue[0].Code)
	assert.Equal(s.T(), responseutils.DbErr, respOO.Issue[0].Details.Coding[0].Display)
}

func (s *APITestSuite) TestJobStatusPending() {
	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Pending",
	}
	s.db.Save(&j)

	req, err := http.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)
	assert.Nil(s.T(), err)

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusAccepted, s.rr.Code)
	assert.Equal(s.T(), "Pending", s.rr.Header().Get("X-Progress"))
	assert.Equal(s.T(), "", s.rr.Header().Get("Expires"))
	s.db.Delete(&j)
}

func (s *APITestSuite) TestJobStatusInProgress() {
	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "In Progress",
	}
	s.db.Save(&j)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusAccepted, s.rr.Code)
	assert.Equal(s.T(), "In Progress", s.rr.Header().Get("X-Progress"))
	assert.Equal(s.T(), "", s.rr.Header().Get("Expires"))

	s.db.Delete(&j)
}

func (s *APITestSuite) TestJobStatusFailed() {
	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Failed",
	}

	s.db.Save(&j)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusInternalServerError, s.rr.Code)

	s.db.Delete(&j)
}

func (s *APITestSuite) TestJobStatusCompleted() {
	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Completed",
	}
	s.db.Save(&j)
	// Encrypt something to get a fake key to put in the job key
	fileName := "dbbd1ce1-ae24-435c-807d-ed45953077d3.ndjson"
	_, encryptedKey, err := encryption.EncryptBytes(s.AuthBackend.PublicKey, []byte("FOO"), fileName)
	assert.Nil(s.T(), err)
	jobKey := models.JobKey{JobID: j.ID, EncryptedKey: encryptedKey, FileName: fileName}
	err = s.db.Save(&jobKey).Error
	assert.Nil(s.T(), err)
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)
	req.TLS = &tls.ConnectionState{}

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
	assert.Equal(s.T(), "application/json", s.rr.Header().Get("Content-Type"))
	// There seems to be some slight difference in precision here.  Match on first 20 chars sb fine.
	assert.Equal(s.T(), j.CreatedAt.Add(GetJobTimeout()).String()[:20], s.rr.Header().Get("Expires")[:20])

	var rb bulkResponseBody
	err = json.Unmarshal(s.rr.Body.Bytes(), &rb)
	if err != nil {
		s.T().Error(err)
	}

	expectedurl := fmt.Sprintf("%s/%s/%s", "http://example.com/data", fmt.Sprint(j.ID), "dbbd1ce1-ae24-435c-807d-ed45953077d3.ndjson")

	assert.Equal(s.T(), j.RequestURL, rb.RequestURL)
	assert.Equal(s.T(), true, rb.RequiresAccessToken)
	assert.Equal(s.T(), "ExplanationOfBenefit", rb.Files[0].Type)
	assert.Equal(s.T(), expectedurl, rb.Files[0].URL)
	assert.NotNil(s.T(), rb.KeyMap)
	assert.Empty(s.T(), rb.Errors)

	s.db.Delete(&j)
}

func (s *APITestSuite) TestJobStatusCompletedErrorFileExists() {
	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Completed",
	}
	s.db.Save(&j)
	fileName := fmt.Sprintf("%s.ndjson", uuid.NewRandom().String())
	jobKey := models.JobKey{
		JobID:        j.ID,
		FileName:     fileName,
		EncryptedKey: []byte("Encrypted Key"),
	}
	s.db.Save(&jobKey)
	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)
	req.TLS = &tls.ConnectionState{}

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	f := fmt.Sprintf("%s/%s", os.Getenv("FHIR_PAYLOAD_DIR"), fmt.Sprint(j.ID))
	if _, err := os.Stat(f); os.IsNotExist(err) {
		err = os.MkdirAll(f, os.ModePerm)
		if err != nil {
			s.T().Error(err)
		}
	}

	errFilePath := fmt.Sprintf("%s/%s/%s-error.ndjson", os.Getenv("FHIR_PAYLOAD_DIR"), fmt.Sprint(j.ID), j.ACOID)
	_, err := os.Create(errFilePath)
	if err != nil {
		s.T().Error(err)
	}

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
	assert.Equal(s.T(), "application/json", s.rr.Header().Get("Content-Type"))

	var rb bulkResponseBody
	err = json.Unmarshal(s.rr.Body.Bytes(), &rb)
	if err != nil {
		s.T().Error(err)
	}

	dataurl := fmt.Sprintf("%s/%s/%s", "http://example.com/data", fmt.Sprint(j.ID), fileName)
	errorurl := fmt.Sprintf("%s/%s/%s", "http://example.com/data", fmt.Sprint(j.ID), "dbbd1ce1-ae24-435c-807d-ed45953077d3-error.ndjson")

	assert.Equal(s.T(), j.RequestURL, rb.RequestURL)
	assert.Equal(s.T(), true, rb.RequiresAccessToken)
	assert.Equal(s.T(), "ExplanationOfBenefit", rb.Files[0].Type)
	assert.Equal(s.T(), dataurl, rb.Files[0].URL)
	assert.Equal(s.T(), "OperationOutcome", rb.Errors[0].Type)
	assert.Equal(s.T(), errorurl, rb.Errors[0].URL)

	s.db.Delete(&j)
	os.Remove(errFilePath)
}

func (s *APITestSuite) TestJobStatusExpired() {
	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Expired",
	}

	s.db.Save(&j)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusGone, s.rr.Code)
	// There seems to be some slight difference in precision here.  Match on first 20 chars sb fine.
	assert.Equal(s.T(), j.CreatedAt.Add(GetJobTimeout()).String()[:20], s.rr.Header().Get("Expires")[:20])
	s.db.Delete(&j)
}

// THis job is old, but has not yet been marked as expired.
func (s *APITestSuite) TestJobStatusNotExpired() {
	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Completed",
	}

	//s.db.Save(&j)
	j.CreatedAt = time.Now().Add(-GetJobTimeout()).Add(-GetJobTimeout())
	s.db.Save(&j)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusGone, s.rr.Code)
	// There seems to be some slight difference in precision here.  Match on first 20 chars sb fine.
	assert.Equal(s.T(), j.CreatedAt.Add(GetJobTimeout()).String()[:20], s.rr.Header().Get("Expires")[:20])
	s.db.Delete(&j)
}

func (s *APITestSuite) TestJobStatusArchived() {
	j := models.Job{
		ACOID:      uuid.Parse("DBBD1CE1-AE24-435C-807D-ED45953077D3"),
		UserID:     uuid.Parse("82503A18-BF3B-436D-BA7B-BAE09B7FFD2F"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Archived",
	}

	s.db.Save(&j)

	req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)

	handler := http.HandlerFunc(jobStatus)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("DBBD1CE1-AE24-435C-807D-ED45953077D3", "82503A18-BF3B-436D-BA7B-BAE09B7FFD2F")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusGone, s.rr.Code)
	// There seems to be some slight difference in precision here.  Match on first 20 chars sb fine.
	assert.Equal(s.T(), j.CreatedAt.Add(GetJobTimeout()).String()[:20], s.rr.Header().Get("Expires")[:20])
	s.db.Delete(&j)
}

func (s *APITestSuite) TestServeData() {
	os.Setenv("FHIR_PAYLOAD_DIR", "../bcdaworker/data/test")
	req := httptest.NewRequest("GET", "/data/test.ndjson", nil)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("fileName", "test.ndjson")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	handler := http.HandlerFunc(serveData)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
	assert.Contains(s.T(), s.rr.Body.String(), `{"resourceType": "Bundle", "total": 33, "entry": [{"resource": {"status": "active", "diagnosis": [{"diagnosisCodeableConcept": {"coding": [{"system": "http://hl7.org/fhir/sid/icd-9-cm", "code": "2113"}]},`)
}

func (s *APITestSuite) TestGetToken() {
	s.SetupAuthBackend()
	req := httptest.NewRequest("GET", "/api/v1/token", nil)

	handler := http.HandlerFunc(getToken)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
	assert.NotEmpty(s.T(), s.rr.Body)
}

func (s *APITestSuite) TestMetadata() {
	req := httptest.NewRequest("GET", "/api/v1/metadata", nil)
	req.TLS = &tls.ConnectionState{}

	handler := http.HandlerFunc(metadata)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
}

func (s *APITestSuite) TestGetVersion() {
	req := httptest.NewRequest("GET", "/_version", nil)

	handler := http.HandlerFunc(getVersion)
	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusOK, s.rr.Code)

	respMap := make(map[string]string)
	err := json.Unmarshal(s.rr.Body.Bytes(), &respMap)
	if err != nil {
		s.T().Error(err.Error())
	}

	assert.Equal(s.T(), "latest", respMap["version"])
}

func makeJWT(acoId, userId string) *jwt.Token {
	token := jwt.New(jwt.SigningMethodRS512)
	token.Claims = jwt.MapClaims{
		"sub": userId,
		"aco": acoId,
		"id":  uuid.NewRandom().String(),
	}
	token.Valid = true
	return token
}

func (s *APITestSuite) TestJobStatusWithWrongACO() {
	j := models.Job{
		ACOID:      uuid.Parse("dbbd1ce1-ae24-435c-807d-ed45953077d3"),
		UserID:     uuid.Parse("82503a18-bf3b-436d-ba7b-bae09b7ffd2f"),
		RequestURL: "/api/v1/ExplanationOfBenefit/$export",
		Status:     "Pending",
	}
	s.db.Save(&j)

	req, err := http.NewRequest("GET", fmt.Sprintf("/api/v1/jobs/%d", j.ID), nil)
	assert.Nil(s.T(), err)

	handler := auth.RequireTokenJobMatch(http.HandlerFunc(jobStatus))

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("jobID", fmt.Sprint(j.ID))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	token := makeJWT("a40404f7-1ef2-485a-9b71-40fe7acdcbc2", "82503a18-bf3b-436d-ba7b-bae09b7ffd2f")
	req = req.WithContext(context.WithValue(req.Context(), "token", token))

	handler.ServeHTTP(s.rr, req)

	assert.Equal(s.T(), http.StatusNotFound, s.rr.Code)

	s.db.Delete(&j)
}

func (s *APITestSuite) TestHealthCheck() {
	req, err := http.NewRequest("GET", "/_health", nil)
	assert.Nil(s.T(), err)
	handler := http.HandlerFunc(healthCheck)
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
}

func (s *APITestSuite) TestHealthCheckWithBadDatabaseURL() {
	// Mock database.LogFatal() to allow execution to continue despite bad URL
	origLogFatal := database.LogFatal
	defer func() { database.LogFatal = origLogFatal }()
	database.LogFatal = func(args ...interface{}) {
		fmt.Println("FATAL (NO-OP)")
	}
	dbURL := os.Getenv("DATABASE_URL")
	defer os.Setenv("DATABASE_URL", dbURL)
	os.Setenv("DATABASE_URL", "not-a-database")
	req, err := http.NewRequest("GET", "/_health", nil)
	assert.Nil(s.T(), err)
	handler := http.HandlerFunc(healthCheck)
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), http.StatusBadGateway, s.rr.Code)
}

func (s *APITestSuite) TestAuthInfoDefault() {

	// get original provider so we can reset at the end of the test
	originalProvider := auth.GetProviderName()

	// set provider to bogus value and make sure default (alpha) is retrieved
	auth.SetProvider("bogus")
	req := httptest.NewRequest("GET", "/_auth", nil)
	handler := http.HandlerFunc(getAuthInfo)
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
	respMap := make(map[string]string)
	err := json.Unmarshal(s.rr.Body.Bytes(), &respMap)
	if err != nil {
		s.T().Error(err.Error())
	}
	assert.Equal(s.T(), "alpha", respMap["auth_provider"])

	// set provider back to original value
	auth.SetProvider(originalProvider)

}

func (s *APITestSuite) TestAuthInfoAlpha() {

	// get original provider so we can reset at the end of the test
	originalProvider := auth.GetProviderName()

	// set provider to alpha and make sure alpha is retrieved
	auth.SetProvider("alpha")
	req := httptest.NewRequest("GET", "/_auth", nil)
	handler := http.HandlerFunc(getAuthInfo)
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
	respMap := make(map[string]string)
	err := json.Unmarshal(s.rr.Body.Bytes(), &respMap)
	if err != nil {
		s.T().Error(err.Error())
	}
	assert.Equal(s.T(), "alpha", respMap["auth_provider"])

	// set provider back to original value
	auth.SetProvider(originalProvider)

}

func (s *APITestSuite) TestAuthInfoOkta() {

	// get original provider so we can reset at the end of the test
	originalProvider := auth.GetProviderName()

	// set provider to okta and make sure okta is retrieved
	auth.SetProvider("okta")
	req := httptest.NewRequest("GET", "/_auth", nil)
	handler := http.HandlerFunc(getAuthInfo)
	handler.ServeHTTP(s.rr, req)
	assert.Equal(s.T(), http.StatusOK, s.rr.Code)
	respMap := make(map[string]string)
	err := json.Unmarshal(s.rr.Body.Bytes(), &respMap)
	if err != nil {
		s.T().Error(err.Error())
	}
	assert.Equal(s.T(), "okta", respMap["auth_provider"])

	// set provider back to original value
	auth.SetProvider(originalProvider)

}

func TestAPITestSuite(t *testing.T) {
	suite.Run(t, new(APITestSuite))
}
