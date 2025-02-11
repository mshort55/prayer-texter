package prayertexter

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type TestCase struct {
	description string
	state       State

	expectedGetItemCalls    int
	expectedPutItemCalls    int
	expectedDeleteItemCalls int
	expectedSendTextCalls   int

	expectedMembers       []Member
	expectedPrayers       []Prayer
	expectedTexts         []TextMessage
	expectedPhones        IntercessorPhones
	expectedDeleteItemKey string
	expectedError         bool
	expectedPrayerQueue   bool

	mockGetItemResults []struct {
		Output *dynamodb.GetItemOutput
		Error  error
	}
	mockPutItemResults []struct {
		Error error
	}
	mockDeleteItemResults []struct {
		Error error
	}
	mockSendTextResults []struct {
		Error error
	}
}

func setMocks(ddbMock *MockDDBConnecter, txtMock *MockTextService, test TestCase) {
	ddbMock.GetItemResults = test.mockGetItemResults
	ddbMock.PutItemResults = test.mockPutItemResults
	ddbMock.DeleteItemResults = test.mockDeleteItemResults
	txtMock.SendTextResults = test.mockSendTextResults
}

func testNumMethodCalls(ddbMock *MockDDBConnecter, txtMock *MockTextService, t *testing.T, test TestCase) {
	if ddbMock.GetItemCalls != test.expectedGetItemCalls {
		t.Errorf("expected GetItem to be called %v, got %v",
			test.expectedGetItemCalls, ddbMock.GetItemCalls)
	}

	if ddbMock.PutItemCalls != test.expectedPutItemCalls {
		t.Errorf("expected PutItem to be called %v, got %v",
			test.expectedPutItemCalls, ddbMock.PutItemCalls)
	}

	if ddbMock.DeleteItemCalls != test.expectedDeleteItemCalls {
		t.Errorf("expected DeleteItem to be called %v, got %v",
			test.expectedDeleteItemCalls, ddbMock.DeleteItemCalls)
	}

	if txtMock.SendTextCalls != test.expectedSendTextCalls {
		t.Errorf("expected SendText to be called %v, got %v",
			test.expectedSendTextCalls, txtMock.SendTextCalls)
	}
}

func testMembers(inputs []dynamodb.PutItemInput, t *testing.T, test TestCase) {
	index := 0

	for _, input := range inputs {
		if *input.TableName != memberTable {
			continue
		}

		if index >= len(test.expectedMembers) {
			t.Errorf("there are more Members in put inputs than in expected Members")
		}

		var actualMem Member
		if err := attributevalue.UnmarshalMap(input.Item, &actualMem); err != nil {
			t.Errorf("failed to unmarshal PutItemInput into Member: %v", err)
		}

		// replace date to make mocking easier
		if actualMem.WeeklyPrayerDate != "" {
			actualMem.WeeklyPrayerDate = "dummy date/time"
		}

		expectedMem := test.expectedMembers[index]
		if actualMem != expectedMem {
			t.Errorf("expected Member %v, got %v", expectedMem, actualMem)
		}

		index++
	}

	if index < len(test.expectedMembers) {
		t.Errorf("there are more Members in expected Members than in put inputs")
	}
}

func testPrayers(inputs []dynamodb.PutItemInput, t *testing.T, test TestCase, queue bool) {
	// need to be careful here not to do unit tests with prayers from both the active prayer
	// table and queued prayers table because it will probably break this mock
	index := 0
	expectedTable := getPrayerTable(queue)

	for _, input := range inputs {

		if *input.TableName != expectedTable {
			continue
		}

		if index >= len(test.expectedPrayers) {
			t.Errorf("there are more Prayers in put inputs than in expected Prayers of table type: %v", expectedTable)
		}

		var actualPryr Prayer
		if err := attributevalue.UnmarshalMap(input.Item, &actualPryr); err != nil {
			t.Errorf("failed to unmarshal PutItemInput into Prayer: %v", err)
		}

		// replace date to make mocking easier
		// replace phone to make mocking easier as phone is normally randomly generated when adding
		// to the prayer queue table
		if !queue {
			actualPryr.Intercessor.WeeklyPrayerDate = "dummy date/time"
		} else if queue {
			actualPryr.IntercessorPhone = "1234567890"
		}

		expectedPryr := test.expectedPrayers[index]
		if actualPryr != expectedPryr {
			t.Errorf("expected Prayer %v, got %v", expectedPryr, actualPryr)
		}

		index++
	}

	if index < len(test.expectedPrayers) {
		t.Errorf("there are more Prayers in expected Prayers than in put inputs of table type: %v", expectedTable)
	}
}

func testPhones(inputs []dynamodb.PutItemInput, t *testing.T, test TestCase) {
	index := 0

	for _, input := range inputs {
		if *input.TableName != intercessorPhonesTable {
			continue
		} else if val, ok := input.Item[intercessorPhonesAttribute]; !ok {
			continue
		} else if stringVal, isString := val.(*types.AttributeValueMemberS); !isString {
			continue
		} else if stringVal.Value != intercessorPhonesKey {
			continue
		}

		if index > 1 {
			t.Errorf("there are more IntercessorPhones in expected IntercessorPhones than 1 which is not expected")
		}

		var actualPhones IntercessorPhones
		if err := attributevalue.UnmarshalMap(input.Item, &actualPhones); err != nil {
			t.Errorf("failed to unmarshal PutItemInput into IntercessorPhones: %v", err)
		}

		if !reflect.DeepEqual(actualPhones, test.expectedPhones) {
			t.Errorf("expected IntercessorPhones %v, got %v", test.expectedPhones, actualPhones)
		}

		index++
	}
}

func testTxtMessage(txtMock *MockTextService, t *testing.T, test TestCase) {
	for i, txt := range txtMock.SendTextInputs {
		// Some text messages use PLACEHOLDER and replace that with the txt recipients name
		// Therefor to make testing easier, the message body is replaced by the msg constant
		if strings.Contains(txt.Body, "Hello! Please pray for") {
			txt.Body = msgPrayerIntro
		} else if strings.Contains(txt.Body, "There was profanity found in your prayer request:") {
			txt.Body = msgProfanityFound
		} else if strings.Contains(txt.Body, "You're prayer request has been prayed for by") {
			txt.Body = msgPrayerConfirmation
		}

		// This part makes mocking messages less painful. We do not need to worry about new lines,
		// pre, or post messages. They are removed when messages are tested.
		for _, t := range []*TextMessage{&txt, &test.expectedTexts[i]} {
			for _, str := range []string{"\n", msgPre, msgPost} {
				t.Body = strings.ReplaceAll(t.Body, str, "")
			}
		}

		if txt != test.expectedTexts[i] {
			t.Errorf("expected txt %v, got %v",
				test.expectedTexts[i], txt)
		}
	}
}

func TestMainFlowSignUp(t *testing.T) {
	testCases := []TestCase{
		{
			description: "Sign up stage ONE: user texts the word pray to start sign up process",

			state: State{
				Message: TextMessage{
					Body:  "pray",
					Phone: "123-456-7890",
				},
			},

			expectedMembers: []Member{
				{
					Phone:       "123-456-7890",
					SetupStage:  1,
					SetupStatus: "in-progress",
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgNameRequest,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  4,
			expectedSendTextCalls: 1,
		},
		{
			description: "Sign up stage ONE: user texts the word Pray (capitol P) to start sign up process",

			state: State{
				Message: TextMessage{
					Body:  "Pray",
					Phone: "123-456-7890",
				},
			},

			expectedMembers: []Member{
				{
					Phone:       "123-456-7890",
					SetupStage:  1,
					SetupStatus: "in-progress",
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgNameRequest,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  4,
			expectedSendTextCalls: 1,
		},
		{
			description: "Sign up stage ONE: get Member error",

			state: State{
				Message: TextMessage{
					Body:  "pray",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: nil,
					Error:  errors.New("first get item failure"),
				},
			},

			expectedError:        true,
			expectedGetItemCalls: 2,
			expectedPutItemCalls: 1,
		},
		{
			description: "Sign up stage TWO-A: user texts name",

			state: State{
				Message: TextMessage{
					Body:  "John Doe",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "1"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
			},

			expectedMembers: []Member{
				{
					Name:        "John Doe",
					Phone:       "123-456-7890",
					SetupStage:  2,
					SetupStatus: "in-progress",
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgMemberTypeRequest,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  4,
			expectedSendTextCalls: 1,
		},
		{
			description: "Sign up stage TWO-B: user texts 2 to remain anonymous",

			state: State{
				Message: TextMessage{
					Body:  "2",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "1"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
			},

			expectedMembers: []Member{
				{
					Name:        "Anonymous",
					Phone:       "123-456-7890",
					SetupStage:  2,
					SetupStatus: "in-progress",
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgMemberTypeRequest,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  4,
			expectedSendTextCalls: 1,
		},
		{
			description: "Sign up final prayer message: user texts 1 which means they do not want to be an intercessor",

			state: State{
				Message: TextMessage{
					Body:  "1",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "2"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
			},

			expectedMembers: []Member{
				{
					Intercessor: false,
					Name:        "John Doe",
					Phone:       "123-456-7890",
					SetupStage:  99,
					SetupStatus: "completed",
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgPrayerInstructions + "\n\n" + msgSignUpConfirmation,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  4,
			expectedSendTextCalls: 1,
		},
		{
			description: "Sign up stage THREE: user texts 2 which means they want to be an intercessor",

			state: State{
				Message: TextMessage{
					Body:  "2",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "2"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
			},

			expectedMembers: []Member{
				{
					Intercessor: true,
					Name:        "John Doe",
					Phone:       "123-456-7890",
					SetupStage:  3,
					SetupStatus: "in-progress",
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgPrayerNumRequest,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  4,
			expectedSendTextCalls: 1,
		},
		{
			description: "Sign up final intercessor message: user texts the number of prayers they are willing to receive per week",

			state: State{
				Message: TextMessage{
					Body:  "10",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor": &types.AttributeValueMemberBOOL{Value: true},
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "3"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
								&types.AttributeValueMemberS{Value: "333-333-3333"},
							}},
						},
					},
					Error: nil,
				},
			},

			expectedMembers: []Member{
				{
					Intercessor:       true,
					Name:              "John Doe",
					Phone:             "123-456-7890",
					SetupStage:        99,
					SetupStatus:       "completed",
					WeeklyPrayerDate:  "dummy date/time",
					WeeklyPrayerLimit: 10,
				},
			},

			expectedPhones: IntercessorPhones{
				Key: intercessorPhonesKey,
				Phones: []string{
					"111-111-1111",
					"222-222-2222",
					"333-333-3333",
					"123-456-7890",
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgPrayerInstructions + "\n\n" + msgIntercessorInstructions + "\n\n" + msgSignUpConfirmation,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  5,
			expectedPutItemCalls:  5,
			expectedSendTextCalls: 1,
		},
		{
			description: "Sign up final intercessor message: put IntercessorPhones error",

			state: State{
				Message: TextMessage{
					Body:  "10",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor": &types.AttributeValueMemberBOOL{Value: true},
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "3"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
								&types.AttributeValueMemberS{Value: "333-333-3333"},
							}},
						},
					},
					Error: nil,
				},
			},

			mockPutItemResults: []struct {
				Error error
			}{
				{
					Error: nil,
				},
				{
					Error: nil,
				},
				{
					Error: errors.New("third put item failure"),
				},
			},

			expectedError:        true,
			expectedGetItemCalls: 5,
			expectedPutItemCalls: 4,
		},
	}

	for _, test := range testCases {
		txtMock := &MockTextService{}
		ddbMock := &MockDDBConnecter{}

		t.Run(test.description, func(t *testing.T) {
			setMocks(ddbMock, txtMock, test)

			if test.expectedError {
				// handles failures for error mocks
				if err := MainFlow(test.state, ddbMock, txtMock); err == nil {
					t.Fatalf("expected error, got nil")
				}
				testNumMethodCalls(ddbMock, txtMock, t, test)
			} else {
				// handles success test cases
				if err := MainFlow(test.state, ddbMock, txtMock); err != nil {
					t.Fatalf("unexpected error starting MainFlow: %v", err)
				}

				testNumMethodCalls(ddbMock, txtMock, t, test)
				testTxtMessage(txtMock, t, test)
				testMembers(ddbMock.PutItemInputs, t, test)
				testPhones(ddbMock.PutItemInputs, t, test)
			}
		})
	}
}

func TestMainFlowSignUpWrongInputs(t *testing.T) {
	testCases := []TestCase{
		// these test cases should do 1 get Member only because return nil on signUpWrongInput
		// 1 get Member call only shows that they took the correct flow
		{
			description: "pray misspelled - returns non registered user and exits",

			state: State{
				Message: TextMessage{
					Body:  "prayyy",
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls: 4,
			expectedPutItemCalls: 3,
		},
		{
			description: "Sign up stage THREE: did not send 1 or 2 as expected to answer msgMemberTypeRequest",

			state: State{
				Message: TextMessage{
					Body:  "wrong response to question",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "2"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgWrongInput,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  3,
			expectedSendTextCalls: 1,
		},
		{
			description: "Sign up final intercessor message: did not send number as expected",

			state: State{
				Message: TextMessage{
					Body:  "wrong response to question",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor": &types.AttributeValueMemberBOOL{Value: true},
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "3"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgWrongInput,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  3,
			expectedSendTextCalls: 1,
		},
	}

	for _, test := range testCases {
		txtMock := &MockTextService{}
		ddbMock := &MockDDBConnecter{}

		t.Run(test.description, func(t *testing.T) {
			setMocks(ddbMock, txtMock, test)

			if err := MainFlow(test.state, ddbMock, txtMock); err != nil {
				t.Fatalf("unexpected error starting MainFlow: %v", err)
			}

			testNumMethodCalls(ddbMock, txtMock, t, test)
			testTxtMessage(txtMock, t, test)
		})
	}
}

func TestMainFlowMemberDelete(t *testing.T) {
	testCases := []TestCase{
		{
			description: "Delete non intercessor member with cancel txt - phone list stays the same",

			state: State{
				Message: TextMessage{
					Body:  "cancel",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
						},
					},
					Error: nil,
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgRemoveUser,
					Phone: "123-456-7890",
				},
			},

			expectedDeleteItemKey:   "123-456-7890",
			expectedGetItemCalls:    4,
			expectedPutItemCalls:    3,
			expectedDeleteItemCalls: 1,
			expectedSendTextCalls:   1,
		},
		{
			description: "Delete intercessor member with STOP txt - phone list changes",

			state: State{
				Message: TextMessage{
					Body:  "STOP",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor": &types.AttributeValueMemberBOOL{Value: true},
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
								&types.AttributeValueMemberS{Value: "333-333-3333"},
								&types.AttributeValueMemberS{Value: "123-456-7890"},
							}},
						},
					},
					Error: nil,
				},
			},

			expectedPhones: IntercessorPhones{
				Key: intercessorPhonesKey,
				Phones: []string{
					"111-111-1111",
					"222-222-2222",
					"333-333-3333",
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgRemoveUser,
					Phone: "123-456-7890",
				},
			},

			expectedDeleteItemKey:   "123-456-7890",
			expectedGetItemCalls:    5,
			expectedPutItemCalls:    4,
			expectedDeleteItemCalls: 1,
			expectedSendTextCalls:   1,
		},
		{
			description: "Delete member - expected error on DelItem",

			state: State{
				Message: TextMessage{
					Body:  "cancel",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor": &types.AttributeValueMemberBOOL{Value: true},
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
						},
					},
					Error: nil,
				},
			},

			mockDeleteItemResults: []struct {
				Error error
			}{
				{
					Error: errors.New("delete item failure"),
				},
			},

			expectedError:           true,
			expectedGetItemCalls:    4,
			expectedPutItemCalls:    3,
			expectedDeleteItemCalls: 1,
		},
	}

	for _, test := range testCases {
		txtMock := &MockTextService{}
		ddbMock := &MockDDBConnecter{}

		t.Run(test.description, func(t *testing.T) {
			setMocks(ddbMock, txtMock, test)

			if test.expectedError {
				// handles failures for error mocks
				if err := MainFlow(test.state, ddbMock, txtMock); err == nil {
					t.Fatalf("expected error, got nil")
				}
				testNumMethodCalls(ddbMock, txtMock, t, test)
			} else {
				// handles success test cases
				if err := MainFlow(test.state, ddbMock, txtMock); err != nil {
					t.Fatalf("unexpected error starting MainFlow: %v", err)
				}

				testNumMethodCalls(ddbMock, txtMock, t, test)
				testTxtMessage(txtMock, t, test)
				testPhones(ddbMock.PutItemInputs, t, test)

				delInput := ddbMock.DeleteItemInputs[0]
				if *delInput.TableName != memberTable {
					t.Errorf("expected Member table name %v, got %v",
						memberTable, *delInput.TableName)
				}

				mem := Member{}
				if err := attributevalue.UnmarshalMap(delInput.Key, &mem); err != nil {
					t.Fatalf("failed to unmarshal to Member: %v", err)
				}

				if mem.Phone != test.expectedDeleteItemKey {
					t.Errorf("expected Member phone %v for delete key, got %v",
						test.expectedDeleteItemKey, mem.Phone)
				}

				// if len(ddbMock.PutItemInputs) > 0 {
				// 	putInput := ddbMock.PutItemInputs[0]
				// 	testPhones(putInput, t, test)
				// }
			}
		})
	}
}

func TestMainFlowHelp(t *testing.T) {
	testCases := []TestCase{
		{
			description: "Setup stage 99 user texts help and receives the help message",

			state: State{
				Message: TextMessage{
					Body:  "help",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
						},
					},
					Error: nil,
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgHelp,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  3,
			expectedSendTextCalls: 1,
		},
		{
			description: "Setup stage 1 user texts help and receives the help message",

			state: State{
				Message: TextMessage{
					Body:  "help",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "1"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "in-progress"},
						},
					},
					Error: nil,
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgHelp,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  3,
			expectedSendTextCalls: 1,
		},
	}

	for _, test := range testCases {
		txtMock := &MockTextService{}
		ddbMock := &MockDDBConnecter{}

		t.Run(test.description, func(t *testing.T) {
			setMocks(ddbMock, txtMock, test)

			if err := MainFlow(test.state, ddbMock, txtMock); err != nil {
				t.Fatalf("unexpected error starting MainFlow: %v", err)
			}

			testNumMethodCalls(ddbMock, txtMock, t, test)
			testTxtMessage(txtMock, t, test)
			testMembers(ddbMock.PutItemInputs, t, test)
			testPrayers(ddbMock.PutItemInputs, t, test, test.expectedPrayerQueue)
		})
	}
}

func TestMainFlowPrayerRequest(t *testing.T) {

	// getMember (initial in MainFlow)
	// getIntPhones (inside findIntercessors)
	// getMember (inside findIntercessors) (2 times)
	// putMember (inside findIntercessors) (2 times)
	// putPrayer (end of prayerRequest) (2 times)

	testCases := []TestCase{
		{
			description: "Successful simple prayer request flow",

			state: State{
				Message: TextMessage{
					Body:  "I need prayer for...",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
							}},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "0"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "2024-12-01T01:00:00Z"},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor2"},
							"Phone":             &types.AttributeValueMemberS{Value: "222-222-2222"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "0"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "2024-12-01T01:00:00Z"},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
			},

			expectedMembers: []Member{
				{
					Intercessor:       true,
					Name:              "Intercessor1",
					Phone:             "111-111-1111",
					PrayerCount:       1,
					SetupStage:        99,
					SetupStatus:       "completed",
					WeeklyPrayerDate:  "dummy date/time",
					WeeklyPrayerLimit: 5,
				},
				{
					Intercessor:       true,
					Name:              "Intercessor2",
					Phone:             "222-222-2222",
					PrayerCount:       1,
					SetupStage:        99,
					SetupStatus:       "completed",
					WeeklyPrayerDate:  "dummy date/time",
					WeeklyPrayerLimit: 5,
				},
			},

			expectedPrayers: []Prayer{
				{
					Intercessor: Member{
						Intercessor:       true,
						Name:              "Intercessor1",
						Phone:             "111-111-1111",
						PrayerCount:       1,
						SetupStage:        99,
						SetupStatus:       "completed",
						WeeklyPrayerDate:  "dummy date/time",
						WeeklyPrayerLimit: 5,
					},
					IntercessorPhone: "111-111-1111",
					Request:          "I need prayer for...",
					Requestor: Member{
						Name:        "John Doe",
						Phone:       "123-456-7890",
						SetupStage:  99,
						SetupStatus: "completed",
					},
				},
				{
					Intercessor: Member{
						Intercessor:       true,
						Name:              "Intercessor2",
						Phone:             "222-222-2222",
						PrayerCount:       1,
						SetupStage:        99,
						SetupStatus:       "completed",
						WeeklyPrayerDate:  "dummy date/time",
						WeeklyPrayerLimit: 5,
					},
					IntercessorPhone: "222-222-2222",
					Request:          "I need prayer for...",
					Requestor: Member{
						Name:        "John Doe",
						Phone:       "123-456-7890",
						SetupStage:  99,
						SetupStatus: "completed",
					},
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgPrayerIntro,
					Phone: "111-111-1111",
				},
				{
					Body:  msgPrayerIntro,
					Phone: "222-222-2222",
				},
				{
					Body:  msgPrayerSentOut,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  7,
			expectedPutItemCalls:  7,
			expectedSendTextCalls: 3,
		},
		{
			description: "Profanity detected",

			state: State{
				Message: TextMessage{
					Body:  "fuckkk you",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
						},
					},
					Error: nil,
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgProfanityFound,
					Phone: "123-456-7890",
				},
			},

			expectedGetItemCalls:  4,
			expectedPutItemCalls:  3,
			expectedSendTextCalls: 1,
		},
		{
			description: "Error with first put Member in findIntercessors",

			state: State{
				Message: TextMessage{
					Body:  "I need prayer for...",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
							}},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "0"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "2024-12-01T01:00:00Z"},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor2"},
							"Phone":             &types.AttributeValueMemberS{Value: "222-222-2222"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "0"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "2024-12-01T01:00:00Z"},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
			},

			mockPutItemResults: []struct {
				Error error
			}{
				{
					Error: nil,
				},
				{
					Error: nil,
				},
				{
					Error: errors.New("put item failure"),
				},
			},

			expectedGetItemCalls: 6,
			expectedPutItemCalls: 4,
			expectedError:        true,
		},
		{
			description: "No available intercessors because of maxed out prayer counters",

			state: State{
				Message: TextMessage{
					Body:  "I need prayer for...",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
							"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
							"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
							}},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "5"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor2"},
							"Phone":             &types.AttributeValueMemberS{Value: "222-222-2222"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "5"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
			},

			expectedPrayers: []Prayer{
				{
					IntercessorPhone: "1234567890",
					Request:          "I need prayer for...",
					Requestor: Member{
						Name:        "John Doe",
						Phone:       "123-456-7890",
						SetupStage:  99,
						SetupStatus: "completed",
					},
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgPrayerQueued,
					Phone: "123-456-7890",
				},
			},

			expectedPrayerQueue:   true,
			expectedGetItemCalls:  8,
			expectedPutItemCalls:  4,
			expectedSendTextCalls: 1,
		},
	}

	for _, test := range testCases {
		txtMock := &MockTextService{}
		ddbMock := &MockDDBConnecter{}

		t.Run(test.description, func(t *testing.T) {
			setMocks(ddbMock, txtMock, test)

			if test.expectedError {
				// handles failures for error mocks
				if err := MainFlow(test.state, ddbMock, txtMock); err == nil {
					t.Fatalf("expected error, got nil")
				}
				testNumMethodCalls(ddbMock, txtMock, t, test)
			} else {
				// handles success test cases
				if err := MainFlow(test.state, ddbMock, txtMock); err != nil {
					t.Fatalf("unexpected error starting MainFlow: %v", err)
				}

				testNumMethodCalls(ddbMock, txtMock, t, test)
				testTxtMessage(txtMock, t, test)
				testMembers(ddbMock.PutItemInputs, t, test)
				testPrayers(ddbMock.PutItemInputs, t, test, test.expectedPrayerQueue)
			}
		})
	}
}

func TestFindIntercessors(t *testing.T) {
	testCases := []TestCase{
		{
			// this mocks the get member outputs so we do not need to worry about the math/rand part
			// #3 gets selected because the date is past 7 days; date + counter gets reset
			// #5 gets chosen because it has 1 prayer slot available
			description: "This should pick #3 and #5 intercessors based on prayer counts/dates",

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
								&types.AttributeValueMemberS{Value: "333-333-3333"},
								&types.AttributeValueMemberS{Value: "444-444-4444"},
								&types.AttributeValueMemberS{Value: "555-555-5555"},
							}},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "5"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor2"},
							"Phone":             &types.AttributeValueMemberS{Value: "222-222-2222"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "100"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().AddDate(0, 0, -2).Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "100"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor3"},
							"Phone":             &types.AttributeValueMemberS{Value: "333-333-3333"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "15"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().AddDate(0, 0, -7).Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "15"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor4"},
							"Phone":             &types.AttributeValueMemberS{Value: "444-444-4444"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "9"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().AddDate(0, 0, -6).Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "9"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor5"},
							"Phone":             &types.AttributeValueMemberS{Value: "555-555-5555"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "4"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
			},

			expectedMembers: []Member{
				{
					Intercessor:       true,
					Name:              "Intercessor3",
					Phone:             "333-333-3333",
					PrayerCount:       1,
					SetupStage:        99,
					SetupStatus:       "completed",
					WeeklyPrayerDate:  "dummy date/time",
					WeeklyPrayerLimit: 15,
				},
				{
					Intercessor:       true,
					Name:              "Intercessor5",
					Phone:             "555-555-5555",
					PrayerCount:       5,
					SetupStage:        99,
					SetupStatus:       "completed",
					WeeklyPrayerDate:  "dummy date/time",
					WeeklyPrayerLimit: 5,
				},
			},

			expectedGetItemCalls: 6,
			expectedPutItemCalls: 2,
		},
		{
			description: "This should return a single intercessor because only one does not have maxed out prayers",

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
								&types.AttributeValueMemberS{Value: "333-333-3333"},
							}},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "5"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor2"},
							"Phone":             &types.AttributeValueMemberS{Value: "222-222-2222"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "5"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor3"},
							"Phone":             &types.AttributeValueMemberS{Value: "333-333-3333"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "4"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
			},

			expectedMembers: []Member{
				{
					Intercessor:       true,
					Name:              "Intercessor3",
					Phone:             "333-333-3333",
					PrayerCount:       5,
					SetupStage:        99,
					SetupStatus:       "completed",
					WeeklyPrayerDate:  "dummy date/time",
					WeeklyPrayerLimit: 5,
				},
			},

			expectedGetItemCalls: 4,
			expectedPutItemCalls: 1,
		},
		{
			description: "This should return nil because all intercessors are maxed out on prayer requests",

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Name": &types.AttributeValueMemberS{Value: intercessorPhonesKey},
							"Phones": &types.AttributeValueMemberL{Value: []types.AttributeValue{
								&types.AttributeValueMemberS{Value: "111-111-1111"},
								&types.AttributeValueMemberS{Value: "222-222-2222"},
							}},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "5"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor2"},
							"Phone":             &types.AttributeValueMemberS{Value: "222-222-2222"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "5"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
			},

			expectedGetItemCalls: 3,
		},
	}

	for _, test := range testCases {
		txtMock := &MockTextService{}
		ddbMock := &MockDDBConnecter{}

		t.Run(test.description, func(t *testing.T) {
			setMocks(ddbMock, txtMock, test)

			if test.expectedError {
				// handles failures for error mocks
				if _, err := findIntercessors(ddbMock); err == nil {
					t.Fatalf("expected error, got nil")
				}
				testNumMethodCalls(ddbMock, txtMock, t, test)
			} else {
				// handles success test cases
				_, err := findIntercessors(ddbMock)
				if err != nil {
					t.Fatalf("unexpected error starting findIntercessors: %v", err)
				}

				testNumMethodCalls(ddbMock, txtMock, t, test)
				testMembers(ddbMock.PutItemInputs, t, test)
			}
		})
	}
}

func TestMainFlowCompletePrayer(t *testing.T) {
	testCases := []TestCase{
		{
			description: "Successful prayer request completion",

			state: State{
				Message: TextMessage{
					Body:  "prayed",
					Phone: "111-111-1111",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "1"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "dummy date"},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor": &types.AttributeValueMemberM{
								Value: map[string]types.AttributeValue{
									"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
									"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
									"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
									"PrayerCount":       &types.AttributeValueMemberN{Value: "1"},
									"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
									"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
									"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "dummy date"},
									"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
								},
							},
							"IntercessorPhone": &types.AttributeValueMemberS{Value: "111-111-1111"},
							"Request":          &types.AttributeValueMemberS{Value: "Please pray me.."},
							"Requestor": &types.AttributeValueMemberM{
								Value: map[string]types.AttributeValue{
									"Intercessor": &types.AttributeValueMemberBOOL{Value: false},
									"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
									"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
									"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
									"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
								},
							},
						},
					},
					Error: nil,
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgPrayerThankYou,
					Phone: "111-111-1111",
				},
				{
					Body:  msgPrayerConfirmation,
					Phone: "123-456-7890",
				},
			},

			expectedDeleteItemKey:   "111-111-1111",
			expectedGetItemCalls:    5,
			expectedPutItemCalls:    3,
			expectedDeleteItemCalls: 1,
			expectedSendTextCalls:   2,
		},
		{
			description: "No active prayers to mark as prayed",

			state: State{
				Message: TextMessage{
					Body:  "prayed",
					Phone: "111-111-1111",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "1"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "dummy date"},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
			},

			expectedTexts: []TextMessage{
				{
					Body:  msgNoActivePrayer,
					Phone: "111-111-1111",
				},
			},

			expectedGetItemCalls:  5,
			expectedPutItemCalls:  3,
			expectedSendTextCalls: 1,
		},
		{
			description: "Error with delete Prayer",

			state: State{
				Message: TextMessage{
					Body:  "prayed",
					Phone: "123-456-7890",
				},
			},

			mockGetItemResults: []struct {
				Output *dynamodb.GetItemOutput
				Error  error
			}{
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
							"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
							"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
							"PrayerCount":       &types.AttributeValueMemberN{Value: "1"},
							"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
							"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
							"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "dummy date"},
							"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
						},
					},
					Error: nil,
				},
				{
					Output: &dynamodb.GetItemOutput{},
					Error:  nil,
				},
				{
					Output: &dynamodb.GetItemOutput{
						Item: map[string]types.AttributeValue{
							"Intercessor": &types.AttributeValueMemberM{
								Value: map[string]types.AttributeValue{
									"Intercessor":       &types.AttributeValueMemberBOOL{Value: true},
									"Name":              &types.AttributeValueMemberS{Value: "Intercessor1"},
									"Phone":             &types.AttributeValueMemberS{Value: "111-111-1111"},
									"PrayerCount":       &types.AttributeValueMemberN{Value: "1"},
									"SetupStage":        &types.AttributeValueMemberN{Value: "99"},
									"SetupStatus":       &types.AttributeValueMemberS{Value: "completed"},
									"WeeklyPrayerDate":  &types.AttributeValueMemberS{Value: "dummy date"},
									"WeeklyPrayerLimit": &types.AttributeValueMemberN{Value: "5"},
								},
							},
							"IntercessorPhone": &types.AttributeValueMemberS{Value: "111-111-1111"},
							"Request":          &types.AttributeValueMemberS{Value: "Please pray me.."},
							"Requestor": &types.AttributeValueMemberM{
								Value: map[string]types.AttributeValue{
									"Intercessor": &types.AttributeValueMemberBOOL{Value: false},
									"Name":        &types.AttributeValueMemberS{Value: "John Doe"},
									"Phone":       &types.AttributeValueMemberS{Value: "123-456-7890"},
									"SetupStage":  &types.AttributeValueMemberN{Value: "99"},
									"SetupStatus": &types.AttributeValueMemberS{Value: "completed"},
								},
							},
						},
					},
					Error: nil,
				},
			},

			mockDeleteItemResults: []struct {
				Error error
			}{
				{
					Error: errors.New("delete item failure"),
				},
			},

			expectedError:           true,
			expectedGetItemCalls:    5,
			expectedPutItemCalls:    3,
			expectedDeleteItemCalls: 1,
			expectedSendTextCalls:   2,
		},
	}

	for _, test := range testCases {
		txtMock := &MockTextService{}
		ddbMock := &MockDDBConnecter{}

		t.Run(test.description, func(t *testing.T) {
			setMocks(ddbMock, txtMock, test)

			if test.expectedError {
				// handles failures for error mocks
				if err := MainFlow(test.state, ddbMock, txtMock); err == nil {
					t.Fatalf("expected error, got nil")
				}
				testNumMethodCalls(ddbMock, txtMock, t, test)
			} else {
				// handles success test cases
				if err := MainFlow(test.state, ddbMock, txtMock); err != nil {
					t.Fatalf("unexpected error starting MainFlow: %v", err)
				}

				testNumMethodCalls(ddbMock, txtMock, t, test)
				testTxtMessage(txtMock, t, test)

				if len(ddbMock.DeleteItemInputs) != 0 {
					delInput := ddbMock.DeleteItemInputs[0]
					if *delInput.TableName != activePrayersTable {
						t.Errorf("expected Prayer table name %v, got %v",
							activePrayersTable, *delInput.TableName)
					}

					pryr := Prayer{}
					if err := attributevalue.UnmarshalMap(delInput.Key, &pryr); err != nil {
						t.Fatalf("failed to unmarshal to Prayer: %v", err)
					}

					if pryr.IntercessorPhone != test.expectedDeleteItemKey {
						t.Errorf("expected Prayer phone %v for delete key, got %v",
							test.expectedDeleteItemKey, pryr.IntercessorPhone)
					}
				}
			}
		})
	}
}
