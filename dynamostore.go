package dynamostore

import (
	"encoding/base32"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
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
	ID         string
	Data       string
	ModifiedAt time.Time
}

func NewDynamoStore(table string, keyPairs ...[]byte) (*DynamoStore, error) {

	client := dynamodb.New(session.New())                               //TODO add parameters for session.New()
	if err := createTableIfNotExists(client, table, 5, 5); err != nil { // TODO give proper value to read and write capacity
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
func (s *DynamoStore) Get(r *http.Request, name string) (*sessions.Session, error) {
	return sessions.GetRegistry(r).Get(s, name)
}

// New returns a session for the given name without adding it to the registry.
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

	input := &dynamodb.PutItemInput{
		Item: map[string]*dynamodb.AttributeValue{
			"ID": {
				S: aws.String(session.ID),
			},
			"Data": {
				S: aws.String(encoded),
			},
			"ModifiedAt": {
				N: aws.String(time.Now().String()),
			},
		},
		TableName: aws.String(s.table),
	}

	_, err = s.client.PutItem(input)
	if err != nil {
		return err
	}

	return nil
}

// load reads the session from dynamoDB.
// returns error if session data does not exist in dynamoDB
func (s *DynamoStore) load(session *sessions.Session) error {
	input := &dynamodb.GetItemInput{
		Key: map[string]*dynamodb.AttributeValue{
			"ID": {
				S: aws.String(session.ID),
			},
		},
		TableName: aws.String(s.table),
	}

	result, err := s.client.GetItem(input)
	if err != nil {
		return err
	}

	if err := securecookie.DecodeMulti(session.Name(), result.Item["Data"].GoString(), &session.Values,
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
			"ID": {
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
					AttributeName: aws.String("ID"),
					AttributeType: aws.String("S"),
				}, {
					AttributeName: aws.String("Data"),
					AttributeType: aws.String("S"),
				}},
				KeySchema: []*dynamodb.KeySchemaElement{{
					AttributeName: aws.String("ID"),
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
