package prayertexter

import (
	"log/slog"
	"strconv"
	"strings"
	"time"
)

func signUp(txt TextMessage, mem Member, clnt DDBConnecter, sndr TextSender) error {
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
		if err := mem.put(clnt); err != nil {
			slog.Error("put Member failed during sign up stage 1")
			return err
		}
		if err := mem.sendMessage(sndr, nameRequest); err != nil {
			slog.Error("message send failed during sign up stage 1")
			return err
		}
	} else if txt.Body != "2" && mem.SetupStage == 1 {
		// stage 2 name request, real name
		mem.SetupStage = 2
		mem.Name = txt.Body
		if err := mem.put(clnt); err != nil {
			slog.Error("put Member failed during sign up stage 2, real name")
			return err
		}
		if err := mem.sendMessage(sndr, memberTypeRequest); err != nil {
			slog.Error("message send failed during sign up stage 2, real name")
			return err
		}
	} else if txt.Body == "2" && mem.SetupStage == 1 {
		// stage 2 name request, anonymous
		mem.SetupStage = 2
		mem.Name = "Anonymous"
		if err := mem.put(clnt); err != nil {
			slog.Error("put Member failed during sign up stage 2, anonymous")
			return err
		}
		if err := mem.sendMessage(sndr, memberTypeRequest); err != nil {
			slog.Error("message send failed during sign up stage 2, anonymous")
			return err
		}
	} else if txt.Body == "1" && mem.SetupStage == 2 {
		// final message for member sign up
		mem.SetupStatus = "completed"
		mem.SetupStage = 99
		mem.Intercessor = false
		if err := mem.put(clnt); err != nil {
			slog.Error("put Member failed during sign up final member message")
			return err
		}
		if err := mem.sendMessage(sndr, prayerInstructions); err != nil {
			slog.Error("message send failed during sign up final member message")
			return err
		}
	} else if txt.Body == "2" && mem.SetupStage == 2 {
		// stage 3 intercessor sign up
		mem.SetupStage = 3
		mem.Intercessor = true
		if err := mem.put(clnt); err != nil {
			slog.Error("put Member failed during sign up stage 3")
			return err
		}
		if err := mem.sendMessage(sndr, prayerNumRequest); err != nil {
			slog.Error("message send failed during sign up stage 3")
			return err
		}
	} else if mem.SetupStage == 3 {
		// final message for intercessor sign up
		num, err := strconv.Atoi(txt.Body)
		if err != nil {
			if err := mem.sendMessage(sndr, wrongInput); err != nil {
				slog.Error("message send failed during sign up final intercessor message - wrong input")
				return err
			}
			slog.Warn("wrong input received during sign up final intercessor message",
				"member", mem.Phone)
			return nil
		}

		phones := IntercessorPhones{}
		if err := phones.get(clnt); err != nil {
			slog.Error("get IntercessorPhones failed during sign up final intercessor message")
			return err
		}
		phones.addPhone(mem.Phone)
		if err := phones.put(clnt); err != nil {
			slog.Error("put IntercessorPhones failed during sign up final intercessor message")
			return err
		}

		mem.SetupStatus = "completed"
		mem.SetupStage = 99
		mem.WeeklyPrayerLimit = num
		if err := mem.put(clnt); err != nil {
			slog.Error("put Member failed during sign up final intercessor message")
			return err
		}
		if err := mem.sendMessage(sndr, intercessorInstructions); err != nil {
			slog.Error("message send failed during sign up final intercessor message - instructions")
			return err
		}

	} else {
		// catch all response for incorrect input
		slog.Warn("wrong input received during sign up", "member", mem.Phone)
		if err := mem.sendMessage(sndr, wrongInput); err != nil {
			slog.Error("message send failed during sign up - wrong input")
			return err
		}
	}

	return nil
}

func findIntercessors(clnt DDBConnecter) ([]Member, error) {
	var intercessors []Member

	for len(intercessors) < numIntercessorsPerPrayer {
		allPhones := IntercessorPhones{}
		if err := allPhones.get(clnt); err != nil {
			slog.Error("get phone list failed during find intercessors")
			return nil, err
		}
		randPhones := allPhones.genRandPhones()

		for _, phn := range randPhones {
			intr := Member{Phone: phn}
			if err := intr.get(clnt); err != nil {
				slog.Error("get intercessor failed during find intercessors")
				return nil, err
			}

			if intr.PrayerCount < intr.WeeklyPrayerLimit {
				intercessors = append(intercessors, intr)
				intr.PrayerCount += 1
				allPhones.delPhone(intr.Phone)
				if err := intr.put(clnt); err != nil {
					slog.Error("put intercessor failed during find intercessors - +1 count")
					return nil, err
				}

				if intr.WeeklyPrayerDate == "" {
					intr.WeeklyPrayerDate = time.Now().Format(time.RFC3339)
					if err := intr.put(clnt); err != nil {
						slog.Error("put intercessor failed during find intercessors - set date")
						return nil, err
					}
				}
			} else if intr.PrayerCount >= intr.WeeklyPrayerLimit {
				currentTime := time.Now()
				previousTime, err := time.Parse(time.RFC3339, intr.WeeklyPrayerDate)
				if err != nil {
					slog.Error("date parse failed during find intercessors")
					return nil, err
				}

				diff := currentTime.Sub(previousTime).Hours()
				// reset prayer counter if time between now and weekly prayer date is greater than
				// 7 days and select intercessor
				if (diff / 24) > 7 {
					intercessors = append(intercessors, intr)
					intr.PrayerCount = 1
					allPhones.delPhone(intr.Phone)
					intr.WeeklyPrayerDate = time.Now().Format(time.RFC3339)
					if err := intr.put(clnt); err != nil {
						slog.Error("put intercessor failed during find intercessors - count reset")
						return nil, err
					}
				} else if (diff / 24) < 7 {
					allPhones.delPhone(intr.Phone)
				}
			}
		}
	}

	return intercessors, nil
}

func prayerRequest(txt TextMessage, mem Member, clnt DDBConnecter, sndr TextSender) error {
	const (
		prayerIntro        = "Hello! Please pray for this person:\n"
		prayerConfirmation = "Your prayer request has been sent out!"
	)

	intercessors, err := findIntercessors(clnt)
	if err != nil {
		slog.Error("failed to find intercessors during prayer request")
		return err
	}

	for _, intr := range intercessors {
		pryr := Prayer{
			Intercessor:      intr,
			IntercessorPhone: intr.Phone,
			Request:          txt.Body,
			Requestor:        mem,
		}
		if err := pryr.put(clnt); err != nil {
			slog.Error("failed to put prayer during prayer request")
			return err
		}
		intr.sendMessage(sndr, prayerIntro+pryr.Request)
	}

	if err := mem.sendMessage(sndr, prayerConfirmation); err != nil {
		slog.Error("message send failed during prayer request")
		return err
	}

	return nil
}

func MainFlow(txt TextMessage, clnt DDBConnecter, sndr TextSender) error {
	const (
		removeUser = "You have been removed from prayer texter. If you ever want to sign back up, text the word pray to this number."
	)

	mem := Member{Phone: txt.Phone}

	if strings.ToLower(txt.Body) == "pray" || mem.SetupStatus == "in-progress" {
		if err := signUp(txt, mem, clnt, sndr); err != nil {
			return err
		}
	} else if strings.ToLower(txt.Body) == "cancel" || strings.ToLower(txt.Body) == "stop" {
		if err := mem.delete(clnt); err != nil {
			slog.Error("failed to delete member during cancellation")
			return err
		}
		if mem.Intercessor {
			phones := IntercessorPhones{}
			if err := phones.get(clnt); err != nil {
				slog.Error("failed to get phone list during cancellation")
				return err
			}
			phones.delPhone(mem.Phone)
			if err := phones.put(clnt); err != nil {
				slog.Error("failed to put phone list during cancellation")
				return err
			}
		}
		if err := mem.sendMessage(sndr, removeUser); err != nil {
			slog.Error("message send failed during cancellation")
			return err
		}
	} else if mem.SetupStatus == "completed" {
		if err := prayerRequest(txt, mem, clnt, sndr); err != nil {
			return err
		}
	} else if mem.SetupStatus == "" {
		slog.Warn("non registered user, dropping message", "member", mem.Phone)
	}

	return nil
}