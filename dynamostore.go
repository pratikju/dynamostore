package dynamostore

import (
	"net/http"
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
	return nil
}

// save writes encoded session.Values into dynamoDB.
// returns error if there is an error while saving the session in dynamoDB
func (s *DynamoStore) save(session *sessions.Session) error {
	return nil
}

// load reads the session from dynamoDB.
// returns error if session data does not exist in dynamoDB
func (s *DynamoStore) load(session *sessions.Session) error {
	return nil
}

// delete removes the session from dynamodb.
// returns error if there is an error in deletion of session from dynamoDB
func (s *DynamoStore) delete(session *sessions.Session) error {
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
