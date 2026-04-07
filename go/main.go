package main

import (
	"fmt"
	"math/rand"
	"time"
)

const (
	Customers    = 20
	Capacity     = 5
	ArrivalMin   = 500
	ArrivalMax   = 2000
	DurationMin  = 1000
	DurationMax  = 4000
	Satisfaction = 3.0
	GracePeriod  = 10000
)

type MsgKind int

const (
	MsgArrive MsgKind = iota
	MsgAdmitted
	MsgTurnedAway
	MsgNextCustomer
	MsgCustomerReady
	MsgNoneWaiting
	MsgWakeUp
	MsgRateRequest
	MsgRating
	MsgGetStats
	MsgStatsReply
	MsgShutdown
)

type Message struct {
	Kind       MsgKind
	From       chan Message // reply-to channel
	CustomerID int
	Value      int   // rating, or other integer payload
	ArrivalMs  int64 // arrival timestamp
}

func Send(to chan Message, msg Message) {
	to <- msg
}

var logStartTime = time.Now()

func logMsg(entity string, msg string) {
	elapsed := time.Since(logStartTime).Milliseconds()
	fmt.Printf("(%dms)(%s) %s\n", elapsed, entity, msg)
}

func customer(id int, waitingRoom chan Message) {
	customerChan := make(chan Message, 5)
	logMsg(fmt.Sprintf("Customer %d", id), "arrived")
	arrivalTime := time.Now()
	Send(waitingRoom, Message{Kind: MsgArrive, CustomerID: id, From: customerChan})
	msg := <-customerChan
	switch msg.Kind {
	case MsgAdmitted:
		logMsg(fmt.Sprintf("Customer %d", id), "admitted to waiting room")
	case MsgTurnedAway:
		logMsg(fmt.Sprintf("Customer %d", id), "turned away")
		return
	}
	called := <-customerChan
	if called.Kind != MsgNextCustomer {
		return
	}
	waitTime := time.Since(arrivalTime).Milliseconds()
	rateReq := <-customerChan
	if rateReq.Kind != MsgRateRequest {
		return
	}
	score := max(1, min(5, 5-int((float64(waitTime)/1000.0)/Satisfaction)+(rand.Intn(3)-1)))
	logMsg(fmt.Sprintf("Customer %d", id), fmt.Sprintf("giving rating %d (waited %fs)", score, float64(waitTime)/1000.0))
	Send(rateReq.From, Message{Kind: MsgRating, CustomerID: id, Value: score, From: customerChan})
}

func waitingRoom(mailbox chan Message) {
	q := make([]chan Message, 0)
	turnedAway := 0
	barberSleeping := false
	var barberChan chan Message
	for {
		msg := <-mailbox
		switch msg.Kind {
		case MsgArrive:
			if len(q) > Capacity-1 {
				msg.From <- Message{Kind: MsgTurnedAway}
				turnedAway++
				logMsg("WaitingRoom", fmt.Sprintf("turned away Customer %d", msg.CustomerID))
			} else {
				Send(msg.From, Message{Kind: MsgAdmitted})
				q = append(q, msg.From)
				logMsg("WaitingRoom", fmt.Sprintf("admitted Customer %d", msg.CustomerID))
				if barberSleeping {
					barberChan <- Message{Kind: MsgWakeUp}
					barberSleeping = false
				}
			}
		case MsgNextCustomer:
			barberChan = msg.From
			if len(q) > 0 {
				next := q[0]
				q = q[1:]
				msg.From <- Message{Kind: MsgCustomerReady, From: next}
				barberSleeping = false
			} else {
				msg.From <- Message{Kind: MsgNoneWaiting}
				barberSleeping = true
			}
		case MsgGetStats:
			Send(msg.From, Message{Kind: MsgStatsReply, Value: turnedAway})
		case MsgShutdown:
			logMsg("WaitingRoom", "shutting down")
			return
		}
	}
}

func barber(mailbox chan Message, waitingRoomChan chan Message) {
	cuts := 0
	avgDuration := 0.0
	avgRating := 0.0
	customerNumber := 1
	waitingRoomChan <- Message{Kind: MsgNextCustomer, From: mailbox}
	for {
		msg := <-mailbox
		switch msg.Kind {
		case MsgNoneWaiting:
			logMsg("Barber", "no customers going to sleep")
			sleeping := true
			for sleeping {
				sleepMsg := <-mailbox
				switch sleepMsg.Kind {
				case MsgWakeUp:
					logMsg("Barber", "woke up")
					Send(waitingRoomChan, Message{Kind: MsgNextCustomer, From: mailbox})
					sleeping = false
				case MsgGetStats:
					Send(sleepMsg.From, Message{Kind: MsgStatsReply, CustomerID: 1, Value: int(avgDuration * 1000)})
					Send(sleepMsg.From, Message{Kind: MsgStatsReply, CustomerID: 2, Value: int(avgRating * 1000)})
					Send(sleepMsg.From, Message{Kind: MsgStatsReply, CustomerID: 3, Value: cuts})
				case MsgShutdown:
					logMsg("Barber", "shutting down")
					return
				}
			}
		case MsgCustomerReady:
			customerChan := msg.From
			duration := DurationMin + rand.Intn(DurationMax-DurationMin+1)
			logMsg("Barber", fmt.Sprintf("cutting Customer %d", customerNumber))
			customerChan <- Message{Kind: MsgNextCustomer}
			time.Sleep(time.Duration(duration) * time.Millisecond)
			logMsg("Barber", fmt.Sprintf("finished cutting Customer %d", customerNumber))
			customerChan <- Message{Kind: MsgRateRequest, From: mailbox}
			var ratingMsg Message
			gotRating := false
			for !gotRating {
				ratingMsg = <-mailbox
				switch ratingMsg.Kind {
				case MsgRating:
					gotRating = true
				case MsgGetStats:
					Send(ratingMsg.From, Message{Kind: MsgStatsReply, CustomerID: 1, Value: int(avgDuration * 1000)})
					Send(ratingMsg.From, Message{Kind: MsgStatsReply, CustomerID: 2, Value: int(avgRating * 1000)})
					Send(ratingMsg.From, Message{Kind: MsgStatsReply, CustomerID: 3, Value: cuts})
				case MsgShutdown:
					continue
				}
			}
			cuts++
			customerNumber++
			avgDuration = avgDuration + (float64(duration)-avgDuration)/float64(cuts)
			avgRating = avgRating + (float64(ratingMsg.Value)-avgRating)/float64(cuts)
			logMsg("Barber", fmt.Sprintf("received rating %d from Customer %d avg duration: %.0fms, avg rating: %.2f", ratingMsg.Value, ratingMsg.CustomerID, avgDuration, avgRating))
			waitingRoomChan <- Message{Kind: MsgNextCustomer, From: mailbox}
		case MsgGetStats:
			Send(msg.From, Message{Kind: MsgStatsReply, CustomerID: 1, Value: int(avgDuration * 1000)})
			Send(msg.From, Message{Kind: MsgStatsReply, CustomerID: 2, Value: int(avgRating * 1000)})
			Send(msg.From, Message{Kind: MsgStatsReply, CustomerID: 3, Value: cuts})
		case MsgShutdown:
			logMsg("Barber", "shutting down")
			return
		}
	}
}

func main() {
	waitingRoomChan := make(chan Message, 10)
	barberChan := make(chan Message, 10)
	go waitingRoom(waitingRoomChan)
	go barber(barberChan, waitingRoomChan)
	logMsg("ShopOwner", "barbershop open")
	time.Sleep(2 * time.Second)
	for i := 0; i <= Customers-1; i++ {
		logMsg("ShopOwner", fmt.Sprintf("spawning Customer %d", i))
		go customer(i, waitingRoomChan)
		time.Sleep(time.Duration(rand.Intn(ArrivalMax-ArrivalMin+1)+ArrivalMin) * time.Millisecond)
	}
	logMsg("ShopOwner", fmt.Sprintf("waiting %dms", GracePeriod))
	time.Sleep(time.Duration(GracePeriod) * time.Millisecond)
	barberStatsChan := make(chan Message, 3)
	wrStatsChan := make(chan Message, 1)
	Send(barberChan, Message{Kind: MsgGetStats, From: barberStatsChan})
	Send(waitingRoomChan, Message{Kind: MsgGetStats, From: wrStatsChan})
	wrStats := <-wrStatsChan
	avgDuration := 0.0
	avgRating := 0.0
	cuts := 0
	received := 0
	for received < 3 {
		msg := <-barberStatsChan
		switch msg.CustomerID {
		case 1:
			avgDuration = float64(msg.Value) / 1000.0
		case 2:
			avgRating = float64(msg.Value) / 1000.0
		case 3:
			cuts = msg.Value
		}
		received++
	}
	Send(barberChan, Message{Kind: MsgShutdown})
	Send(waitingRoomChan, Message{Kind: MsgShutdown})
	fmt.Println()
	fmt.Println("=== Barbershop Closing Report ===")
	fmt.Printf("Total customers arrived:    %d\n", Customers)
	fmt.Printf("Customers served:           %d\n", cuts)
	fmt.Printf("Customers turned away:      %d\n", wrStats.Value)
	fmt.Printf("Average haircut duration:   %.2fs\n", avgDuration/1000.0)
	fmt.Printf("Average satisfaction:        %.1f / 5.0\n", avgRating)
	fmt.Println("=================================")
}
