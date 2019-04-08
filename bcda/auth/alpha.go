package auth

import (
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/CMSgov/bcda-app/bcda/database"
	"github.com/CMSgov/bcda-app/bcda/models"
	jwt "github.com/dgrijalva/jwt-go"
	"github.com/pborman/uuid"
	log "github.com/sirupsen/logrus"
)

type AlphaAuthPlugin struct{}

func (p AlphaAuthPlugin) RegisterClient(localID string) (Credentials, error) {
	if localID == "" {
		return Credentials{}, errors.New("provide a non-empty string")
	}

	aco, err := getACOFromDB(localID)
	if err != nil {
		return Credentials{}, err
	}

	if aco.AlphaSecret != "" {
		return Credentials{}, fmt.Errorf("aco %s has a secret", localID)
	}

	s, err := generateClientSecret()
	if err != nil {
		return Credentials{}, err
	}

	hashedSecret := NewHash(s)

	db := database.GetGORMDbConnection()
	defer database.Close(db)
	aco.ClientID = localID
	aco.AlphaSecret = hashedSecret.String()
	err = db.Save(&aco).Error
	if err != nil {
		return Credentials{}, err
	}

	return Credentials{ClientName: aco.Name, ClientID: localID, ClientSecret: s}, nil
}

func generateClientSecret() (string, error) {
	b := make([]byte, 40)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", b), nil
}

func (p AlphaAuthPlugin) UpdateClient(params []byte) ([]byte, error) {
	return nil, errors.New("not yet implemented")
}

func (p AlphaAuthPlugin) DeleteClient(clientID string) error {
	aco, err := GetACOByClientID(clientID)
	if err != nil {
		return err
	}

	aco.ClientID = ""
	aco.AlphaSecret = ""

	db := database.GetGORMDbConnection()
	defer database.Close(db)
	err = db.Save(&aco).Error
	if err != nil {
		return err
	}

	return nil
}

func (p AlphaAuthPlugin) GenerateClientCredentials(clientID string, ttl int) (Credentials, error) {
	return Credentials{}, fmt.Errorf("GenerateClientCredentials is not implemented for alpha auth")
}

func (p AlphaAuthPlugin) RevokeClientCredentials(clientID string) error {
	return fmt.Errorf("RevokeClientCredentials is not implemented for alpha auth")
}

// MakeAccessToken manufactures an access token for the given credentials
func (p AlphaAuthPlugin) MakeAccessToken(credentials Credentials) (string, error) {
	if credentials.ClientSecret == "" || credentials.ClientID == "" {
		return "", fmt.Errorf("missing or incomplete credentials")
	}
	aco, err := GetACOByClientID(credentials.ClientID)
	if err != nil {
		return "", fmt.Errorf("invalid credentials; %s", err)
	}
	// when we have ClientSecret in ACO, adjust following line
	Hash(aco.AlphaSecret).IsHashOf(credentials.ClientSecret)
	var user models.User
	if database.GetGORMDbConnection().First(&user, "aco_id = ?", aco.UUID).RecordNotFound() {
		return "", fmt.Errorf("invalid credentials; unable to locate User for ACO with id of %s", aco.UUID)
	}
	issuedAt := time.Now().Unix()
	expiresAt := time.Now().Add(time.Hour * time.Duration(TokenTTL)).Unix()
	return GenerateTokenString(uuid.NewRandom().String(), user.UUID.String(), aco.UUID.String(), issuedAt, expiresAt)
}

// RequestAccessToken generate a token for the ACO, either for a specified UserID or (if not provided) any user in the ACO
func (p AlphaAuthPlugin) RequestAccessToken(creds Credentials, ttl int) (Token, error) {
	var userUUID, acoUUID uuid.UUID
	var user models.User
	var err error
	token := Token{}

	if creds.UserID == "" && creds.ClientID == "" {
		return token, fmt.Errorf("must provide either UserID or ClientID")
	}

	if ttl < 0 {
		return token, fmt.Errorf("invalid TTL: %d", ttl)
	}

	db := database.GetGORMDbConnection()
	defer database.Close(db)

	if creds.UserID != "" {
		userUUID = uuid.Parse(creds.UserID)
		if userUUID == nil {
			return token, fmt.Errorf("user ID must be a UUID")
		}

		if db.First(&user, "UUID = ?", creds.UserID).RecordNotFound() {
			return token, fmt.Errorf("unable to locate User with id of %s", creds.UserID)
		}

		userUUID = user.UUID
		acoUUID = user.ACOID
	} else {
		var aco models.ACO
		aco, err = getACOFromDB(creds.ClientID)
		if err != nil {
			return token, err
		}

		if err = db.First(&user, "aco_id = ?", aco.UUID).Error; err != nil {
			return token, errors.New("no user found for " + aco.UUID.String())
		}

		userUUID = user.UUID
		acoUUID = aco.UUID
	}

	token.UUID = uuid.NewRandom()
	token.UserID = userUUID
	token.ACOID = acoUUID
	token.IssuedAt = time.Now().Unix()
	token.ExpiresOn = time.Now().Add(time.Hour * time.Duration(ttl)).Unix()
	token.Active = true

	if err = db.Create(&token).Error; err != nil {
		return Token{}, err
	}

	token.TokenString, err = GenerateTokenString(token.UUID.String(), token.UserID.String(), token.ACOID.String(), token.IssuedAt, token.ExpiresOn)
	if err != nil {
		return Token{}, err
	}

	return token, nil
}

func (p AlphaAuthPlugin) RevokeAccessToken(tokenString string) error {
	return fmt.Errorf("RevokeAccessToken is not implemented for alpha auth")
}

func (p AlphaAuthPlugin) ValidateJWT(tokenString string) error {
	t, err := p.DecodeJWT(tokenString)
	if err != nil {
		log.Errorf("could not decode token %s because %s", tokenString, err)
		return err
	}

	c := t.Claims.(*CommonClaims)

	err = checkRequiredClaims(c)
	if err != nil {
		return err
	}

	err = c.Valid()
	if err != nil {
		return err
	}

	_, err = getACOFromDB(c.ACOID)
	if err != nil {
		return err
	}

	b := isActive(t)
	if !b {
		return fmt.Errorf("token with id: %v is not active", c.UUID)
	}

	return nil
}

func checkRequiredClaims(claims *CommonClaims) error {
	if claims.ExpiresAt == 0 ||
		claims.IssuedAt == 0 ||
		claims.Subject == "" ||
		claims.ACOID == "" ||
		claims.UUID == "" {
		return fmt.Errorf("missing one or more required claims")
	}
	return nil
}

func isActive(token *jwt.Token) bool {
	c := token.Claims.(*CommonClaims)

	db := database.GetGORMDbConnection()
	defer database.Close(db)
	var dbt Token
	return !db.Find(&dbt, "UUID = ? AND active = ?", c.UUID, true).RecordNotFound()
}

func (p AlphaAuthPlugin) DecodeJWT(tokenString string) (*jwt.Token, error) {
	keyFunc := func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return InitAlphaBackend().PublicKey, nil
	}

	return jwt.ParseWithClaims(tokenString, &CommonClaims{}, keyFunc)
}

func getACOFromDB(acoUUID string) (models.ACO, error) {
	var (
		db  = database.GetGORMDbConnection()
		aco models.ACO
		err error
	)
	defer database.Close(db)

	if db.Find(&aco, "UUID = ?", uuid.Parse(acoUUID)).RecordNotFound() {
		err = errors.New("no ACO record found for " + acoUUID)
	}
	return aco, err
}
