package init

// This package is for database initialization during unit tests

import (
	"fmt"
	"testing"

	"github.com/CMSgov/bcda-app/bcda/auth"
	"github.com/CMSgov/bcda-app/bcda/models"

	"github.com/stretchr/testify/suite"
)

type InitTestSuite struct {
	suite.Suite
}

func (s *InitTestSuite) TestInitializeModels() {
	models.InitializeGormModels()
	auth.InitializeGormModels()

	assert := s.Assert()

	const ACOName = "ACO Name"
	cmsID := "A12345"
	acoUUID, err := models.CreateACO(ACOName, &cmsID)

	assert.Nil(err)
	assert.NotNil(acoUUID)
	fmt.Println("DB successfully created!")
}

func TestInitTestSuite(t *testing.T) {
	suite.Run(t, new(InitTestSuite))
}
