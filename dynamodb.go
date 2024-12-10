package main

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

type dbGetter interface {
	getItem(attr, key, table string) *dynamodb.GetItemOutput
}

type dbPutter interface {
	putItem(table string, data map[string]types.AttributeValue)
}

type dbDeleter interface {
	delItem(attr, key, table string)
}

type database struct {
	clnt *dynamodb.Client
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

func (d database) getItem(attr, key, table string) *dynamodb.GetItemOutput {
	out, err := d.clnt.GetItem(context.TODO(), &dynamodb.GetItemInput{
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

func (d database) putItem(table string, data map[string]types.AttributeValue) {
	_, err := d.clnt.PutItem(context.TODO(), &dynamodb.PutItemInput{
		TableName: &table,
		Item:      data,
	})

	if err != nil {
		log.Fatalf("unable to put item, %v", err)
	}
}

func (d database) delItem(attr, key, table string) {
	_, err := d.clnt.DeleteItem(context.TODO(), &dynamodb.DeleteItemInput{
		TableName: &table,
		Key: map[string]types.AttributeValue{
			attr: &types.AttributeValueMemberS{Value: key},
		},
	})

	if err != nil {
		log.Fatalf("unable to delete item, %v", err)
	}
}
