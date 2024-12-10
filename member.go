package main

import (
	"log"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
)

type Member struct {
	Intercessor       bool
	Name              string
	Phone             string
	PrayerCount       int
	SetupStage        int
	SetupStatus       string
	WeeklyPrayerDate  string
	WeeklyPrayerLimit int
}

const (
	memberAttribute = "Phone"
	memberTable     = "Members"
)

func sendText(body string, recipient string) {
	log.Printf("Sending to: %v\n", recipient)
	log.Printf("Body: %v\n", body)
}

func (m Member) sendMessage(body string) {
	sendText(body, m.Phone)
}

func (m Member) get(d dbGetter, table string) Member {
	resp := d.getItem(memberAttribute, m.Phone, table)

	if err := attributevalue.UnmarshalMap(resp.Item, &m); err != nil {
		log.Fatalf("unmarshal failed for get member, %v", err)
	}

	return m
}

func (m Member) put(d dbPutter, table string) {
	data, err := attributevalue.MarshalMap(m)
	if err != nil {
		log.Fatalf("unmarshal failed for put member, %v", err)
	}

	d.putItem(table, data)
}

func (m Member) delete(d dbDeleter) {
	d.delItem(memberAttribute, m.Phone, memberTable)
}
