package models

import (
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"os"

	"github.com/CMSgov/bcda-app/bcda/database"
	"github.com/CMSgov/bcda-app/bcda/utils"
	"github.com/bgentry/que-go"
	"github.com/jinzhu/gorm"
	"github.com/pborman/uuid"
	log "github.com/sirupsen/logrus"
)

//Setting a default here.  It may need to change.  It also shouldn't really be used much.
const BCDA_FHIR_MAX_RECORDS_DEFAULT = 10000

func InitializeGormModels() *gorm.DB {
	log.Println("Initialize bcda models")
	db := database.GetGORMDbConnection()
	defer database.Close(db)

	// Migrate the schema
	// Add your new models here
	// This should probably not be called in production
	// What happens when you need to make a database change, there's already data you need to preserve, and
	// you need to run a script to migrate existing data to its new home or shape?
	db.AutoMigrate(
		&ACO{},
		&User{},
		&Job{},
		&JobKey{},
		&Beneficiary{},
		&ACOBeneficiary{},
	)

	db.Model(&ACOBeneficiary{}).AddForeignKey("aco_id", "acos(uuid)", "RESTRICT", "RESTRICT")
	db.Model(&ACOBeneficiary{}).AddForeignKey("beneficiary_id", "beneficiaries(id)", "RESTRICT", "RESTRICT")

	return db
}

type Job struct {
	gorm.Model
	ACO        ACO       `gorm:"foreignkey:ACOID;association_foreignkey:UUID"` // aco
	ACOID      uuid.UUID `gorm:"type:char(36)" json:"aco_id"`
	User       User      `gorm:"foreignkey:UserID;association_foreignkey:UUID"` // user
	UserID     uuid.UUID `gorm:"type:char(36)"`
	RequestURL string    `json:"request_url"` // request_url
	Status     string    `json:"status"`      // status
	JobCount   int
	JobKeys    []JobKey
}

func (job *Job) CheckCompletedAndCleanup() (bool, error) {

	// Trivial case, no need to keep going
	if job.Status == "Completed" {
		return true, nil
	}
	db := database.GetGORMDbConnection()
	defer database.Close(db)

	var completedJobs int64
	db.Model(&JobKey{}).Where("job_id = ?", job.ID).Count(&completedJobs)

	if int(completedJobs) >= job.JobCount {

		staging := fmt.Sprintf("%s/%d", os.Getenv("FHIR_STAGING_DIR"), job.ID)
		err := os.Remove(staging)

		if err != nil {
			log.Error(err)
		}

		return true, db.Model(&job).Update("status", "Completed").Error

	}

	return false, nil
}

func (job *Job) GetEnqueJobs(encrypt bool, t string) (enqueJobs []*que.Job, err error) {
	var jobIDs []string

	var rowCount = 0
	maxBeneficiaries := utils.GetEnvInt("BCDA_FHIR_MAX_RECORDS", BCDA_FHIR_MAX_RECORDS_DEFAULT)

	db := database.GetGORMDbConnection()
	defer database.Close(db)
	var aco ACO
	err = db.Find(&aco, "uuid = ?", job.ACOID).Error
	if err != nil {
		return nil, err
	}

	beneficiaryIDs, err := aco.GetBeneficiaryIDs()
	if err != nil {
		return nil, err
	}

	for _, id := range beneficiaryIDs {
		rowCount++
		jobIDs = append(jobIDs, id)
		if len(jobIDs) >= maxBeneficiaries || rowCount >= len(beneficiaryIDs) {

			args, err := json.Marshal(jobEnqueueArgs{
				ID:             int(job.ID),
				ACOID:          job.ACOID.String(),
				UserID:         job.UserID.String(),
				BeneficiaryIDs: jobIDs,
				ResourceType:   t,
				// TODO: remove `Encrypt` when file encryption disable functionality is ready to be deprecated
				Encrypt: encrypt,
			})
			if err != nil {
				return nil, err
			}

			j := &que.Job{
				Type: "ProcessJob",
				Args: args,
			}

			enqueJobs = append(enqueJobs, j)

			jobIDs = []string{}
		}
	}
	return enqueJobs, nil
}

type JobKey struct {
	gorm.Model
	Job          Job  `gorm:"foreignkey:jobID"`
	JobID        uint `gorm:"primary_key" json:"job_id"`
	EncryptedKey []byte
	FileName     string `gorm:"type:char(127)"`
}

// ACO-Beneficiary relationship models based on https://github.com/jinzhu/gorm/issues/719#issuecomment-168485989
type ACO struct {
	gorm.Model
	UUID             uuid.UUID `gorm:"primary_key;type:char(36)" json:"uuid"`
	CMSID            *string   `gorm:"type:char(5)" json:"cms_id"`
	Name             string    `json:"name"`
	ClientID         string    `json:"client_id"`
	AlphaSecret      string    `json:"alpha_secret"`
	ACOBeneficiaries []*ACOBeneficiary
}

func (aco *ACO) GetBeneficiaryIDs() (beneficiaryIDs []string, err error) {
	db := database.GetGORMDbConnection()
	defer database.Close(db)

	beneficiaryJoin := "inner join beneficiaries on acos_beneficiaries.beneficiary_id = beneficiaries.id"
	if err = db.Table("acos_beneficiaries").Joins(beneficiaryJoin).Where("acos_beneficiaries.aco_id = ?", aco.UUID).Pluck("beneficiaries.blue_button_id", &beneficiaryIDs).Error; err != nil {
		log.Errorf("Error retrieving ACO-beneficiaries for ACO ID %s: %s", aco.UUID.String(), err.Error())
		return nil, err
	} else if len(beneficiaryIDs) == 0 {
		log.Errorf("Retrieved 0 ACO-beneficiaries for ACO ID %s", aco.UUID.String())
		return nil, fmt.Errorf("retrieved 0 ACO-beneficiaries for ACO ID %s", aco.UUID.String())
	}

	return beneficiaryIDs, nil
}

type Beneficiary struct {
	gorm.Model
	BlueButtonID string `gorm:"type: text"`
}

type ACOBeneficiary struct {
	ACOID         uuid.UUID `gorm:"type:uuid"`
	ACO           *ACO      `gorm:"foreignkey:ACOID"`
	BeneficiaryID uint
	Beneficiary   *Beneficiary `gorm:"foreignkey:BeneficiaryID;association_foreignkey:ID"`
	// Join model needed for additional fields later, e.g., AttributionDate
}

func (*ACOBeneficiary) TableName() string {
	return "acos_beneficiaries"
}

func (aco *ACO) GetPublicKey() *rsa.PublicKey {
	// todo implement a real thing.  But for now we can use this.
	return GetATOPublicKey()
}

// This exists to provide a known static keys used for ACO's in our alpha tests.
// This key is not meant to protect anything and both halves will be made available publicly
func GetATOPublicKey() *rsa.PublicKey {
	fmt.Println("Looking for a key at:")
	fmt.Println(os.Getenv("ATO_PUBLIC_KEY_FILE"))
	atoPublicKeyFile, err := os.Open(os.Getenv("ATO_PUBLIC_KEY_FILE"))
	if err != nil {
		fmt.Println("failed to open file")
		panic(err)
	}
	return utils.OpenPublicKeyFile(atoPublicKeyFile)
}

func GetATOPrivateKey() *rsa.PrivateKey {
	atoPrivateKeyFile, err := os.Open(os.Getenv("ATO_PRIVATE_KEY_FILE"))
	if err != nil {
		panic(err)
	}
	return utils.OpenPrivateKeyFile(atoPrivateKeyFile)
}

func CreateACO(name string, cmsID *string) (uuid.UUID, error) {
	db := database.GetGORMDbConnection()
	defer database.Close(db)

	aco := ACO{Name: name, CMSID: cmsID, UUID: uuid.NewRandom()}
	db.Create(&aco)

	return aco.UUID, db.Error
}

type User struct {
	gorm.Model
	UUID  uuid.UUID `gorm:"primary_key; type:char(36)" json:"uuid"` // uuid
	Name  string    `json:"name"`                                   // name
	Email string    `json:"email"`                                  // email
	ACO   ACO       `gorm:"foreignkey:ACOID;association_foreignkey:UUID"`
	ACOID uuid.UUID `gorm:"type:char(36)" json:"aco_id"` // aco_id
}

func CreateUser(name string, email string, acoUUID uuid.UUID) (User, error) {
	db := database.GetGORMDbConnection()
	defer database.Close(db)
	var aco ACO
	var user User
	// If we don't find the ACO return a blank user and an error
	if db.First(&aco, "UUID = ?", acoUUID).RecordNotFound() {
		return user, fmt.Errorf("unable to locate ACO with id of %v", acoUUID)
	}
	// check for duplicate email addresses and only make one if it isn't found
	if db.First(&user, "email = ?", email).RecordNotFound() {
		user = User{UUID: uuid.NewRandom(), Name: name, Email: email, ACOID: aco.UUID}
		db.Create(&user)
		return user, nil
	} else {
		return user, fmt.Errorf("unable to create user for %v because a user with that Email address already exists", email)
	}
}

// CLI command only support; note that we are choosing to fail quickly and let the user (one of us) figure it out
func CreateAlphaACO(db *gorm.DB) (ACO, error) {
	var count int
	db.Table("acos").Count(&count)
	aco := ACO{Name: fmt.Sprintf("Alpha ACO %d", count), UUID: uuid.NewRandom()}
	db.Create(&aco)

	return aco, db.Error
}

func AssignAlphaBeneficiaries(db *gorm.DB, aco ACO, acoSize string) error {
	s := "insert into acos_beneficiaries (aco_id, beneficiary_id) select '" + aco.UUID.String() +
		"', b.id from beneficiaries b join acos_beneficiaries ab on b.id = ab.beneficiary_id " +
		"where ab.aco_id = (select uuid from acos where name ilike 'ACO " + acoSize + "')"
	return db.Exec(s).Error
}

// This is not a persistent model so it is not necessary to include in GORM auto migrate.
// swagger:ignore
type jobEnqueueArgs struct {
	ID             int
	ACOID          string
	UserID         string
	BeneficiaryIDs []string
	ResourceType   string
	// TODO: remove `Encrypt` when file encryption disable functionality is ready to be deprecated
	Encrypt bool
}
