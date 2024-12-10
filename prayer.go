package main

import (
	"log"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
)

type Prayer struct {
	Intercessor      Member
	IntercessorPhone string
	Request          string
	Requestor        Member
}

const (
	prayerAttribute = "IntercessorPhone"
	prayerTable     = "ActivePrayers"
)

func (p Prayer) get(d dbGetter) Prayer {
	resp := d.getItem(prayerAttribute, p.IntercessorPhone, prayerTable)

	if err := attributevalue.UnmarshalMap(resp.Item, &p); err != nil {
		log.Fatalf("unmarshal failed for get prayer, %v", err)
	}

	return p
}

func (p Prayer) put(d dbPutter) {
	data, err := attributevalue.MarshalMap(p)
	if err != nil {
		log.Fatalf("unmarshal failed for put prayer, %v", err)
	}

	d.putItem(prayerTable, data)
}

func (p Prayer) delete(d dbDeleter) {
	d.delItem(prayerAttribute, p.IntercessorPhone, prayerTable)
}
