package prayertexter

import (
	"context"
	"log"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type DDBClient interface {
    GetItem(ctx context.Context, input *dynamodb.GetItemInput, opts ...func(
		*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
    PutItem(ctx context.Context, input *dynamodb.PutItemInput, opts ...func(
		*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
    DeleteItem(ctx context.Context, input *dynamodb.DeleteItemInput, opts ...func(
		*dynamodb.Options)) (*dynamodb.DeleteItemOutput, error)
}

func getDdbClient() *dynamodb.Client {
	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load aws-sdk-go-v2 config, %v", err)
	}

	local, err := strconv.ParseBool(os.Getenv("AWS_SAM_LOCAL"))
	if err != nil {
		log.Fatalf("unable to convert AWS_SAM_LOCAL value to boolean, %v", err)
	}

	var clnt *dynamodb.Client

	if local {
		clnt = dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String("http://dynamodb:8000")
		})
	} else {
		clnt = dynamodb.NewFromConfig(cfg)
	}

	return clnt
}

func getItem(clnt DDBClient, attr, key, table string) *dynamodb.GetItemOutput {
	out, err := clnt.GetItem(context.TODO(), &dynamodb.GetItemInput{
		TableName: &table,
		Key: map[string]types.AttributeValue{
			attr: &types.AttributeValueMemberS{Value: key},
		},
	})

	if err != nil {
		log.Fatalf("unable to get item, %v", err)
	}

	return out
}

func putItem(clnt DDBClient, table string, data map[string]types.AttributeValue) {
	_, err := clnt.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: &table,
		Item:      data,
	})

	if err != nil {
		log.Fatalf("unable to put item, %v", err)
	}
}

func delItem(clnt DDBClient, attr, key, table string) {
	_, err := clnt.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
		TableName: &table,
		Key: map[string]types.AttributeValue{
			attr: &types.AttributeValueMemberS{Value: key},
		},
	})

	if err != nil {
		log.Fatalf("unable to delete item, %v", err)
	}
}
