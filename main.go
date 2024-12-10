package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

// MUST BE SET by go build -ldflags "-X main.version=999"
// like 0.6.14-0-g26fe727 or 0.6.14-2-g9118702-dirty

//lint:ignore U1000 - var used in Makefile
var version string // do not remove or modify

type TextMessage struct {
	Body  string `json:"body"`
	Phone string `json:"phone-number"`
}

func signUp(db database, txt TextMessage, mem Member) {
	const (
		nameRequest             = "Text your name, or 2 to stay anonymous"
		memberTypeRequest       = "Text 1 for prayer request, or 2 to be added to the intercessors list (to pray for others)"
		prayerInstructions      = "You are now signed up to send prayer requests! Please send them directly to this number."
		prayerNumRequest        = "Send the max number of prayer texts you are willing to receive and pray for per week."
		intercessorInstructions = "You are now signed up to receive prayer requests. Please try to pray for the requests ASAP. Once you are done praying, send 'prayed' back to this number for confirmation."
		wrongInput              = "Wrong input received during sign up process. Please try again."
	)

	if strings.ToLower(txt.Body) == "pray" {
		// stage 1
		mem.SetupStatus = "in-progress"
		mem.SetupStage = 1
		mem.put(db, memberTable)
		mem.sendMessage(nameRequest)
	} else if txt.Body != "2" && mem.SetupStage == 1 {
		// stage 2 name request
		mem.SetupStage = 2
		mem.Name = txt.Body
		mem.put(db, memberTable)
		mem.sendMessage(memberTypeRequest)
	} else if txt.Body == "2" && mem.SetupStage == 1 {
		// stage 2 name request
		mem.SetupStage = 2
		mem.Name = "Anonymous"
		mem.put(db, memberTable)
		mem.sendMessage(memberTypeRequest)
	} else if txt.Body == "1" && mem.SetupStage == 2 {
		// final message for member sign up
		mem.SetupStatus = "completed"
		mem.SetupStage = 99
		mem.Intercessor = false
		mem.put(db, memberTable)
		mem.sendMessage(prayerInstructions)
	} else if txt.Body == "2" && mem.SetupStage == 2 {
		// stage 3 intercessor sign up
		mem.SetupStage = 3
		mem.Intercessor = true
		mem.put(db, memberTable)
		mem.sendMessage(prayerNumRequest)
	} else if mem.SetupStage == 3 {
		// final message for intercessor sign up
		if num, err := strconv.Atoi(txt.Body); err == nil {
			phones := IntercessorPhones{}.get(db)
			phones = phones.addPhone(mem.Phone)
			phones.put(db)

			mem.SetupStatus = "completed"
			mem.SetupStage = 99
			mem.WeeklyPrayerLimit = num
			mem.put(db, memberTable)
			mem.sendMessage(intercessorInstructions)
		} else {
			mem.sendMessage(wrongInput)
		}
	} else {
		// catch all response for incorrect input
		mem.sendMessage(wrongInput)
	}
}

func findIntercessors(db database) []Member {
	var intercessors []Member

	for len(intercessors) < numIntercessorsPerPrayer {
		allPhones := IntercessorPhones{}.get(db)
		randPhones := allPhones.genRandPhones()

		for _, phn := range randPhones {
			intr := Member{Phone: phn}.get(db, memberTable)

			if intr.PrayerCount < intr.WeeklyPrayerLimit {
				intercessors = append(intercessors, intr)
				intr.PrayerCount += 1
				allPhones = allPhones.delPhone(intr.Phone)
				intr.put(db, memberTable)

				if intr.WeeklyPrayerDate == "" {
					intr.WeeklyPrayerDate = time.Now().Format(time.RFC3339)
					intr.put(db, memberTable)
				}
			} else if intr.PrayerCount >= intr.WeeklyPrayerLimit {
				currentTime := time.Now()
				previousTime, err := time.Parse(time.RFC3339, intr.WeeklyPrayerDate)
				if err != nil {
					log.Fatalf("date parse failed, %v", err)
				}

				diff := currentTime.Sub(previousTime).Hours()
				// reset prayer counter if time between now and weekly prayer date is greater than
				// 7 days
				if (diff / 24) > 7 {
					intercessors = append(intercessors, intr)
					intr.PrayerCount = 1
					allPhones = allPhones.delPhone(intr.Phone)
					intr.WeeklyPrayerDate = time.Now().Format(time.RFC3339)
					intr.put(db, memberTable)
				} else if (diff / 24) < 7 {
					allPhones = allPhones.delPhone(intr.Phone)
				}
			}
		}
	}

	return intercessors
}

func prayerRequest(db database, txt TextMessage, mem Member) {
	const (
		prayerIntro        = "Hello! Please pray for this person:\n"
		prayerConfirmation = "Your prayer request has been sent out!"
	)

	intercessors := findIntercessors(db)

	for _, intr := range intercessors {
		pryr := Prayer{
			Intercessor:      intr,
			IntercessorPhone: intr.Phone,
			Request:          txt.Body,
			Requestor:        mem,
		}
		pryr.put(db)
		intr.sendMessage(prayerIntro + pryr.Request)
	}

	mem.sendMessage(prayerConfirmation)
}

func mainFlow(txt TextMessage) {
	const (
		removeUser = "You have been removed from prayer texter. If you ever want to sign back up, text the word pray to this number."
	)

	db := database{}
	db.clnt = getDdbClient()

	mem := Member{Phone: txt.Phone}.get(db, memberTable)

	if strings.ToLower(txt.Body) == "pray" || mem.SetupStatus == "in-progress" {
		signUp(db, txt, mem)
	} else if strings.ToLower(txt.Body) == "cancel" || strings.ToLower(txt.Body) == "stop" {
		mem.delete(db)
		if mem.Intercessor {
			phones := IntercessorPhones{}.get(db)
			phones = phones.delPhone(mem.Phone)
			phones.put(db)
		}
		mem.sendMessage(removeUser)
	} else if mem.SetupStatus == "completed" {
		prayerRequest(db, txt, mem)
	} else if mem.SetupStatus == "" {
		log.Printf("%v is not a registered user, dropping message", mem.Phone)
	}
}

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (
	events.APIGatewayProxyResponse, error) {
	txt := TextMessage{}

	if err := json.Unmarshal([]byte(req.Body), &txt); err != nil {
		log.Fatalf("failed to unmarshal api gateway request, %v", err.Error())
		return events.APIGatewayProxyResponse{StatusCode: http.StatusInternalServerError}, nil
	}

	mainFlow(txt)

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       "Completed Successfully",
	}, nil
}

func main() {
	lambda.Start(handler)
}
