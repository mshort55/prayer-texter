package main

import (
	"log"
	"math/rand"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
)

type IntercessorPhones struct {
	Name   string
	Phones []string
}

const (
	IntercessorPhonesAttribute = "Name"
	IntercessorPhonesKey       = "IntercessorPhones"
	IntercessorPhonesTable     = "General"
	numIntercessorsPerPrayer   = 2
)

func (i IntercessorPhones) get(d dbGetter) IntercessorPhones {
	resp := d.getItem(IntercessorPhonesAttribute, IntercessorPhonesKey, IntercessorPhonesTable)

	if err := attributevalue.UnmarshalMap(resp.Item, &i); err != nil {
		log.Fatalf("unmarshal failed for get intercessor phones, %v", err)
	}

	return i
}

func (i IntercessorPhones) put(d dbPutter) {
	i.Name = IntercessorPhonesKey

	data, err := attributevalue.MarshalMap(i)
	if err != nil {
		log.Fatalf("marshal failed for put intercessor phones, %v", err)
	}

	d.putItem(IntercessorPhonesTable, data)
}

func (i IntercessorPhones) addPhone(phone string) IntercessorPhones {
	i.Phones = append(i.Phones, phone)

	return i
}

func (i IntercessorPhones) delPhone(phone string) IntercessorPhones {
	var newPhones []string

	for _, p := range i.Phones {
		if p != phone {
			newPhones = append(newPhones, p)
		}
	}

	i.Phones = newPhones

	return i
}

func (i IntercessorPhones) genRandPhones() []string {
	var phones []string

	for len(phones) < numIntercessorsPerPrayer {
		p := i.Phones[rand.Intn(len(i.Phones))]
		phones = append(phones, p)
	}

	return phones
}
