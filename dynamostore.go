package dynamostore

import (
	"encoding/base32"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
)

const (
	// DefaultDynamoDBTableName is used when no table name is configured explicitly.
	DefaultDynamoDBTableName = "session-backend"
	// DefaultDynamoDBReadCapacity is the default read capacity that is used when none is configured explicitly.
	DefaultDynamoDBReadCapacity = 5
	// DefaultDynamoDBWriteCapacity is the default write capacity that is used when none is configured explicitly.
	DefaultDynamoDBWriteCapacity = 5
	// DefaultDynamoDBRegion is used when no region is configured explicitly.
	DefaultDynamoDBRegion = "us-east-1"
)

// DynamoStore stores sessions in dynamoDB.
type DynamoStore struct {
	table   string
	client  *dynamodb.DynamoDB
	Codecs  []securecookie.Codec
	Options *sessions.Options // default configuration
}

// Session object stored in dynamoDB
type Session struct {
	// Identifier for session values
	ID string `json:"id"`
	// Encoded session values
	Data string `json:"data"`
	// Unix timestamp indicating when the session values were modified
	ModifiedAt int64 `json:"modified_at"`
}

// NewDynamoStore creates the dynamoDB store from given configuration
// config parameters expects the following keys:
//
// 1. table for dynamoDB table to store the session.
//
// 2. read_capacity for read provisioned throughput for dynamoDB table.
//
// 3. write_capacity for write provisioned throughput for dynamoDB table.
//
// 4. region for aws region where the dynamoDB table will be created.
//
// 5. endpoint for aws dynamoDB endpoint.
//
// If any of the keys is missing, corresponding default value for the key will be used.
//
// See https://github.com/gorilla/sessions/blob/master/store.go for detailed information on what keyPairs does.
func NewDynamoStore(config map[string]string, keyPairs ...[]byte) (*DynamoStore, error) {

	table := config["table"]
	if table == "" {
		table = DefaultDynamoDBTableName
	}

	readCapacityString := config["read_capacity"]
	if readCapacityString == "" {
		readCapacityString = "0"
	}
	readCapacity, err := strconv.Atoi(readCapacityString)
	if err != nil {
		return nil, fmt.Errorf("invalid read capacity: %q", readCapacityString)
	}
	if readCapacity == 0 {
		readCapacity = DefaultDynamoDBReadCapacity
	}

	writeCapacityString := config["write_capacity"]
	if writeCapacityString == "" {
		writeCapacityString = "0"
	}
	writeCapacity, err := strconv.Atoi(writeCapacityString)
	if err != nil {
		return nil, fmt.Errorf("invalid write capacity: %q", writeCapacityString)
	}
	if writeCapacity == 0 {
		writeCapacity = DefaultDynamoDBWriteCapacity
	}

	region := config["region"]
	if region == "" {
		region = DefaultDynamoDBRegion
	}

	endpoint := config["endpoint"]

	session, err := session.NewSession(&aws.Config{
		Region:   aws.String(region),
		Endpoint: aws.String(endpoint),
	})
	if err != nil {
		return nil, err
	}

	client := dynamodb.New(session)
	if err := createTableIfNotExists(client, table, readCapacity, writeCapacity); err != nil {
		return nil, err
	}

	return &DynamoStore{
		table:  table,
		client: client,
		Codecs: securecookie.CodecsFromPairs(keyPairs...),
		Options: &sessions.Options{
			Path:   "/",
			MaxAge: 86400 * 30,
		},
	}, nil
}

// Get returns a session for the given name after adding it to the registry.
//
// It returns a new session if the sessions doesn't exist. Access IsNew on
// the session to check if it is an existing session or a new one.
//
// It returns a new session and an error if the session exists but could
// not be decoded.
func (s *DynamoStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

// New returns a session for the given name without adding it to the registry.
//
// The difference between New() and Get() is that calling New() twice will
// decode the session data twice, while Get() registers and reuses the same
func (s *DynamoStore) New(r *http.Request, name string) (*sessions.Session, error) {
	session := sessions.NewSession(s, name)
	opts := *s.Options
	session.Options = &opts
	session.IsNew = true
	var err error
	if c, errCookie := r.Cookie(name); errCookie == nil {
		err = securecookie.DecodeMulti(name, c.Value, &session.ID, s.Codecs...)
		if err == nil {
			err = s.load(session)
			if err == nil {
				session.IsNew = false
			} else {
				err = nil
			}
		}
	}
	return session, err
}

// Save adds a single session to the response.
func (s *DynamoStore) Save(r *http.Request, w http.ResponseWriter, session *sessions.Session) error {
	if session.Options.MaxAge <= 0 {
		if err := s.delete(session); err != nil {
			return err
		}
		http.SetCookie(w, sessions.NewCookie(session.Name(), "", session.Options))
		return nil
	}

	if session.ID == "" {
		session.ID = strings.TrimRight(base32.StdEncoding.EncodeToString(securecookie.GenerateRandomKey(32)), "=")
	}

	if err := s.save(session); err != nil {
		return err
	}

	encoded, err := securecookie.EncodeMulti(session.Name(), session.ID, s.Codecs...)
	if err != nil {
		return err
	}

	http.SetCookie(w, sessions.NewCookie(session.Name(), encoded, session.Options))
	return nil
}

// MaxAge sets the maximum age for the store and the underlying cookie implementation.
// Individual sessions can be deleted by setting Options.MaxAge = -1 for that session.
func (s *DynamoStore) MaxAge(age int) {
	s.Options.MaxAge = age

	// Set the maxAge for each securecookie instance.
	for _, codec := range s.Codecs {
		if sc, ok := codec.(*securecookie.SecureCookie); ok {
			sc.MaxAge(age)
		}
	}
}

// save writes encoded session.Values into dynamoDB.
// returns error if there is an error while saving the session in dynamoDB
func (s *DynamoStore) save(session *sessions.Session) error {
	encoded, err := securecookie.EncodeMulti(session.Name(), session.Values, s.Codecs...)
	if err != nil {
		return err
	}

	sessionObj := &Session{
		ID:         session.ID,
		Data:       encoded,
		ModifiedAt: time.Now().Unix(),
	}

	sessionItem, err := dynamodbattribute.MarshalMap(sessionObj)
	if err != nil {
		return err
	}

	if _, err = s.client.PutItem(&dynamodb.PutItemInput{
		Item:      sessionItem,
		TableName: aws.String(s.table),
	}); err != nil {
		return err
	}

	return nil
}

// load reads the session from dynamoDB.
// returns error if session data does not exist in dynamoDB
func (s *DynamoStore) load(session *sessions.Session) error {
	input := &dynamodb.GetItemInput{
		Key: map[string]*dynamodb.AttributeValue{
			"id": {
				S: aws.String(session.ID),
			},
		},
		TableName: aws.String(s.table),
	}

	result, err := s.client.GetItem(input)
	if err != nil {
		return err
	}

	var sessionObj Session
	if err := dynamodbattribute.UnmarshalMap(result.Item, &sessionObj); err != nil {
		return err
	}

	if err := securecookie.DecodeMulti(session.Name(), sessionObj.Data, &session.Values,
		s.Codecs...); err != nil {
		return err
	}

	return nil
}

// delete removes the session from dynamodb.
// returns error if there is an error in deletion of session from dynamoDB
func (s *DynamoStore) delete(session *sessions.Session) error {
	input := &dynamodb.DeleteItemInput{
		Key: map[string]*dynamodb.AttributeValue{
			"id": {
				S: aws.String(session.ID),
			},
		},
		TableName: aws.String(s.table),
	}

	_, err := s.client.DeleteItem(input)
	if err != nil {
		return err
	}

	return nil
}

// createTableIfNotExists creates a DynamoDB table with a given
// DynamoDB client. If the table already exists, it is not being reconfigured.
func createTableIfNotExists(client *dynamodb.DynamoDB, table string, readCapacity, writeCapacity int) error {
	_, err := client.DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(table),
	})

	if awserr, ok := err.(awserr.Error); ok {
		if awserr.Code() == "ResourceNotFoundException" {
			_, err = client.CreateTable(&dynamodb.CreateTableInput{
				AttributeDefinitions: []*dynamodb.AttributeDefinition{{
					AttributeName: aws.String("id"),
					AttributeType: aws.String("S"),
				}},
				KeySchema: []*dynamodb.KeySchemaElement{{
					AttributeName: aws.String("id"),
					KeyType:       aws.String("HASH"),
				}},
				ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
					ReadCapacityUnits:  aws.Int64(int64(readCapacity)),
					WriteCapacityUnits: aws.Int64(int64(writeCapacity)),
				},
				TableName: aws.String(table),
			})
			if err != nil {
				return err
			}

			err = client.WaitUntilTableExists(&dynamodb.DescribeTableInput{
				TableName: aws.String(table),
			})
			if err != nil {
				return err
			}
		}
	}
	if err != nil {
		return err
	}
	return nil
}
