package main

import (
	"github.com/Simply-Bits/astmon/gami/event"
	"log"
	"math"
	"regexp"
	"strconv"
	"time"
)

var (
	queuenameRegex1 = regexp.MustCompile(`^(?:queue-)([a-zA-z0-9]*)(-([a-zA-Z0-9-]*))?$`)
	queuenameRegex2 = regexp.MustCompile(`^([a-zA-z0-9]*)(-([a-zA-Z0-9-]*))?$`)
	dynAgeRegex     = regexp.MustCompile(`^([0-9]*):([0-9]{1,2})\sm:s$`)
)

func queueNameToOrgID(queuename string) (orgID string, orgqueuename string, valid bool) {
	/* for queuenameRegex1:
	    input="queue-acme-support"
	        m[0]=queue-acme-support
	        m[1]=acme
	        m[2]=-support
	        m[3]=support
	    input="queue-acme"
	        m[0]=queue-acme
	        m[1]=acme
	        m[2]=
	        m[3]=
	    input="queue-"  ** ERROR
	        m[0]=queue-
	        m[1]=
	        m[2]=
	        m[3]=
	   for queuenameRegex2:
	    input="acme-support"
	        m[0]=acme-support
	        m[1]=acme
	        m[2]=-support
	        m[3]=support
	    input="support"  ** ERROR
	        m[0]=support
	        m[1]=support
	        m[2]=
	        m[3]=
	*/
	var m []string
	logger.Debugf("Decoding queuename: %s\n", queuename)
	if len(queuename) > 6 && queuename[0:6] == "queue-" {
		m = queuenameRegex1.FindStringSubmatch(queuename)
		if len(m) < 2 || len(m[1]) == 0 {
			logger.Warnf("Unable to decode queue name %s\n", queuename)
			return
		}
	} else {
		m = queuenameRegex2.FindStringSubmatch(queuename)
		if len(m) < 4 || len(m[3]) == 0 {
			logger.Warnf("Unable to decode queue name %s\n", queuename)
			return
		}
	}
	orgID = m[1]
	if len(m) > 3 && len(m[3]) > 0 {
		orgqueuename = m[3]
	} else {
		orgqueuename = "undefined"
	}
	valid = true
	return
}

func handleQueueParams(evt event.QueueParams) {
	/* This event is returned after a QueueStatus manager command and looks like:
	   Event: QueueParams
	   Queue: queue-furrier20
	   Max: 5
	   Strategy: ringall
	   Calls: 0
	   HoldTime: 9
	   TalkTime: 113
	   Completed: 1968
	   Abandoned: 118
	   ServiceLevel: 60
	   ServicelevelPerf: 96.8
	   Weight: 0
	   WhenStatsCleared: 1969-12-31 05:00:00 PM

	   Max = Maximum calls queue will hold
	   Holdtime = Current avg holdtime, based on an exponential average
	   TalkTime = Current avg talktime, based on the same exponential average
	   Completed = Number of queue calls completed
	   Abandoned = Number of queue calls abandoned
	   ServiceLevel =seconds setting for servicelevel

	   If we get this event, then we might or might not get subsequent QueueMember events (depending
	   on the Member filter supplied to the QueueStatus command).  We will get a complete list of
	   queue entries via QueueEntry events so we always create a new list for the entries.
	*/

	// log.Println("QP=", evt)
	printQueueParamsEvent(evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		logger.Debugf("[handleQueueParams] Unable to decode queue name %s\n", evt.Queue)
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		logger.Debugf("[handleQueueParams] Unable to FindOrg InfoPtr for %s\n", orgID)
		return
	} else {
		logger.Debugf("[handleQueueParams] Handling Queues for %s\n", orgID)
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == false {
			pQI = new(QUEUEINFO)
			pOrg.Queues[queuename] = pQI
			pQI.Members = make(map[string]*QUEUEMEMBER, 10)
		}
		pQI.Entries = make(map[string]*QUEUEENTRY, 10)
		pQI.Max = evt.Max
		pQI.Strategy = evt.Strategy
		pQI.Calls = evt.Calls
		pQI.HoldTime = evt.HoldTime
		pQI.TalkTime = evt.TalkTime
		pQI.Completed = evt.Completed
		pQI.Abandoned = evt.Abandoned
		pQI.ServiceLevel = evt.ServiceLevel
		pQI.ServicelevelPerf = evt.ServicelevelPerf
		pQI.Weight = evt.Weight
		// evt.WhenStatsCleared looks like '2016-07-11 04:13:57 PM'
		loc, _ := time.LoadLocation("America/Phoenix")
		pQI.WhenStatsCleared, _ = time.ParseInLocation("2006-01-02 03:04:05 PM", evt.WhenStatsCleared, loc)
		//pQI.WhenStatsCleared = evt.WhenStatsCleared
		pQI.CallsCompletedInSL = int(math.Floor(float64(pQI.Completed) / 100.0 * pQI.ServicelevelPerf))
	}
}

func printQueueParamsEvent(evt event.QueueParams) {
	logger.Debugf("------- QueueParams Event -------\n")
	logger.Debugf("Queue: %s\n", evt.Queue)
	logger.Debugf("Max: %d\n", evt.Max)
	logger.Debugf("Strategy: %s\n", evt.Strategy)
	logger.Debugf("CALLS: %d\n", evt.Calls)
	logger.Debugf("HoldTime: %d\n", evt.HoldTime)
	logger.Debugf("TalkTime: %d\n", evt.TalkTime)
	logger.Debugf("Completed: %d\n", evt.Completed)
	logger.Debugf("Abandoned: %d\n", evt.Abandoned)
	logger.Debugf("ServiceLevel: %d\n", evt.ServiceLevel)
	logger.Debugf("ServicelevelPerf: %f\n", evt.ServicelevelPerf)
	logger.Debugf("Weight: %d\n", evt.Weight)
	logger.Debugf("WhenStatsCleared: %s\n", evt.WhenStatsCleared)
	logger.Debugf("------- End QueueParams Event -------\n")
}

// Search for OrgInfo in our Org map, returning nil if none found.
// Upon return of non-nil value, the found ORGINFO is locked
// and the caller must call pOrg.Unlock()
func FindOrgInfoPtr(orgID string) *ORGINFO {
	var (
		pOrg *ORGINFO
		ok   bool
	)
	orgMapRWMutex.RLock()
	defer orgMapRWMutex.RUnlock()
	if pOrg, ok = orgMap[orgID]; !ok {
		logger.Warnf("[FindOrgInfoPtr] OrgID (%s) not found\n", orgID)
		return nil
	}
	//logger.Debugf("[FindOrgInfoPtr] Found existing OrgInfo for OrgID (%s) -> [%#v]\n", orgID, pOrg)
	//printOrgInfo(pOrg)
	pOrg.Lock()
	return pOrg
}

func handleQueueMember(evt event.QueueMember) {
	/* This event is returned after a QueueStatus manager command and looks like:
	   Queue: queue-furrier20
	   Name: SIP/7201furrier20
	   Location: SIP/7201furrier20
	   Membership: static
	   DynamicAge:
	   Penalty: 0
	   CallsTaken: 0
	   LastCall: 0
	   WhenAdded: 0
	   Status: 1
	   Paused: 0
	*/
	printQueueMemberEvent(evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	logger.Debugf("\t Handling Queue Member for OrgID (%s), Queue (%s)", orgID, queuename)
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		logger.Debugf("\t Unable to FindOrg InfoPtr for OrgID (%s)", orgID)
		return
	} else {
		logger.Debugf("\t Found OrgInfo for OrgID (%s), processing member", orgID)
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			logger.Debugf("\t Found QueueInfo for Queue (%s), processing member", queuename)
			pQM, found := pQI.Members[evt.Location]
			if !found {
				pQM = new(QUEUEMEMBER)
				pQI.Members[evt.Location] = pQM
				pQM.Location = evt.Location
			}
			pQM.Name = evt.Name
			pQM.Membership = evt.Membership

			// evt.DynamicAge looks like: "%ld:%2.2ld m:s", (long) (now - mem->whenadded) / 60, (long) (now - mem->whenadded) % 60);
			if len(evt.DynamicAge) > 0 {
				var min, sec int
				var err error
				m := dynAgeRegex.FindStringSubmatch(evt.DynamicAge)
				if len(m) == 3 {
					if min, err = strconv.Atoi(m[1]); err != nil {
						min = 0
					}
					if sec, err = strconv.Atoi(m[2]); err != nil {
						sec = 0
					}
					pQM.DynamicAge = 60*min + sec
				}
			}

			pQM.Penalty = evt.Penalty
			pQM.CallsTaken = evt.CallsTaken
			pQM.LastCall = time.Unix(int64(evt.LastCall), 0)
			pQM.WhenAdded = time.Unix(int64(evt.WhenAdded), 0)
			if evt.Status == 254 {
				// If the agent is within their wrapup time, asterisk returns this 254 value
				// when enumerating queues.  The wrapup completion never sends an event
				// so we really have to ignore it here else it will forever be 254 from now on
				evt.Status = 1
			}
			pQM.Status = evt.Status
			pQM.Paused = evt.Paused
		} else {
			logger.Debugf("\t Unable to find QueueInfo for Queue (%s)", queuename)
		}
	}
}

func printQueueMemberEvent(evt event.QueueMember) {
	logger.Debugf("------- QueueMember Event -------\n")
	logger.Debugf("Queue: %s\n", evt.Queue)
	logger.Debugf("Name: %s\n", evt.Name)
	logger.Debugf("Location: %s\n", evt.Location)
	logger.Debugf("Membership: %s\n", evt.Membership)
	logger.Debugf("DynamicAge: %s\n", evt.DynamicAge)
	logger.Debugf("Penalty: %d\n", evt.Penalty)
	logger.Debugf("CallsTaken: %d\n", evt.CallsTaken)
	logger.Debugf("LastCall: %d\n", evt.LastCall)
	logger.Debugf("WhenAdded: %d\n", evt.WhenAdded)
	logger.Debugf("Status: %d\n", evt.Status)
	logger.Debugf("Paused: %d\n", evt.Paused)
	logger.Debugf("------- End QueueMember Event -------\n")
}

func handleQueueEntry(evt event.QueueEntry) {
	/* This event is returned after a QueueStatus manager command and looks like:
	   Queue: %s
	   Channel: %s
	   Uniqueid: %s
	   CallerIDNum: %s
	   CallerIDName: %s
	   ConnectedLineNum: %s
	   ConnectedLineName: %s
	   Position: %d
	   Wait: %ld
	*/
	now := time.Now().Unix()
	log.Println("QE=", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			pQE := new(QUEUEENTRY)
			pQI.Entries[evt.Uniqueid] = pQE
			pQE.Position = evt.Position
			pQE.Channel = evt.Channel
			pQE.Uniqueid = evt.Uniqueid
			pQE.CallerIDNum = evt.CallerIDNum
			pQE.CallerIDName = evt.CallerIDName
			pQE.ConnectedLineNum = evt.ConnectedLineNum
			pQE.ConnectedLineName = evt.ConnectedLineName
			pQE.WhenEntered = time.Unix(now-int64(evt.Wait), 0)
		}
	}
}

func handleQueueJoin(evt event.QueueJoin) {
	/* This is an async event when a call goes into a queue and looks like:
	   Event: Join
	   Queue: sb-sales
	   Privilege: call,all
	   Channel: SIP/trunksip1-00129d73
	   Uniqueid: pbxA-1418427006.1273457
	   CallerIDNum: 3072626144
	   CallerIDName: WEBSITE-CASPER,WY
	   ConnectedLineNum: unknown
	   ConnectedLineName: unknown
	   Position: 1
	   Count: 1
	   Custgroup: ls
	*/
	if processQueueEntries.Load().(bool) == false {
		logger.Debugf("[handleQueueJoin] Skipping processing of QueueJoin event as processing is disabled\n")
		return
	}
	now := time.Now()
	logger.Debugf("QJ=%#v", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			// Adjust all the positions of entries with a position the same or higher than the one we just removed
			for _, v := range pQI.Entries {
				if v.Position >= evt.Position {
					v.Position = v.Position + 1
				}
			}
			pQE := new(QUEUEENTRY)
			pQI.Entries[evt.Uniqueid] = pQE
			pQE.Position = evt.Position
			pQE.Channel = evt.Channel
			pQE.Uniqueid = evt.Uniqueid
			pQE.CallerIDNum = evt.CallerIDNum
			pQE.CallerIDName = evt.CallerIDName
			pQE.ConnectedLineNum = evt.ConnectedLineNum
			pQE.ConnectedLineName = evt.ConnectedLineName
			pQE.CountWhenEntered = evt.Count
			pQE.WhenEntered = now
			pQI.Calls = pQI.Calls + 1
			sendQueueCallersChange(orgID, queuename, pQI)
			logger.Debugf("After Join, Queue Entries: %s\n", queuename)
			for _, v := range pQI.Entries {
				logger.Debugf("\t%d\t%s\t%s\n", v.Position, v.CallerIDName, v.WhenEntered)
			}
		}
	}
}

func handleQueueCallerJoin(evt event.QueueCallerJoin) {
	/* This is an async event when a call goes into a queue in asterisk 22	*/
	if processQueueEntries.Load().(bool) == false {
		logger.Debugf("[handleQueueCallerJoin] Skipping processing of QueueJoin event as processing is disabled\n")
		return
	}
	now := time.Now()
	//logger.Debugf("QJ=%#v", evt)
	printQueueCallerJoinEvent(evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			// Adjust all the positions of entries with a position the same or higher than the one we just removed
			for _, v := range pQI.Entries {
				if v.Position >= evt.Position {
					v.Position = v.Position + 1
				}
			}
			pQE := new(QUEUEENTRY)
			pQI.Entries[evt.Uniqueid] = pQE
			pQE.Position = evt.Position
			pQE.Channel = evt.Channel
			pQE.Uniqueid = evt.Uniqueid
			pQE.CallerIDNum = evt.CallerIDNum
			pQE.CallerIDName = evt.CallerIDName
			pQE.ConnectedLineNum = evt.ConnectedLineNum
			pQE.ConnectedLineName = evt.ConnectedLineName
			pQE.CountWhenEntered = evt.Count
			pQE.WhenEntered = now
			pQI.Calls = pQI.Calls + 1
			sendQueueCallersChange(orgID, queuename, pQI)
			logger.Debugf("After Join, Queue Entries: %s\n", queuename)
			for _, v := range pQI.Entries {
				logger.Debugf("\t%d\t%s\t%s\n", v.Position, v.CallerIDName, v.WhenEntered)
			}
		}
	}
}

func printQueueCallerJoinEvent(evt event.QueueCallerJoin) {
	logger.Debugf("------- QueueCallerJoin Event -------\n")
	logger.Debugf("Queue: %s, Channel: %s\n", evt.Queue, evt.Channel)
	logger.Debugf("CallerIDNum: %s, CallerIDName: %s\n", evt.CallerIDNum, evt.CallerIDName)
	logger.Debugf("ConnectedLineNum: %s, ConnectedLineName: %s\n", evt.ConnectedLineNum, evt.ConnectedLineName)
	logger.Debugf("Position: %d, Count: %d, Uniqueid: %s\n", evt.Position, evt.Count, evt.Uniqueid)
	logger.Debugf("------- End QueueCallerJoin Event -------\n")
}

func handleQueueCallerLeave(evt event.QueueCallerLeave) {
	/* This is an async event when a call leaves a queue in asterisk 22	*/
	if processQueueEntries.Load().(bool) == false {
		logger.Debugf("[handleQueueCallerLeave] Skipping processing of QueueCallerLeave event as processing is disabled\n")
		return
	}
	//logger.Debugf("QL= %#v", evt)
	printQueueCallerLeaveEvent(evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			pQE, found2 := pQI.Entries[evt.Uniqueid]
			if found2 == true {
				delete(pQI.Entries, evt.Uniqueid)
				pQI.Calls = pQI.Calls - 1
				// Adjust all the positions of entries with a position higher than the one we just removed
				for _, v := range pQI.Entries {
					if v.Position > pQE.Position {
						v.Position = v.Position - 1
					}
				}
				logger.Debugf("After Leave, Queue Entries: %s\n", queuename)
				for _, v := range pQI.Entries {
					logger.Debugf("\t%d\t%s\t%s\n", v.Position, v.CallerIDName, v.WhenEntered)
				}
			}
			sendQueueCallersChange(orgID, queuename, pQI)
		}
	}
}

func printQueueCallerLeaveEvent(evt event.QueueCallerLeave) {
	logger.Debugf("------- QueueCallerLeave Event -------\n")
	logger.Debugf("Queue: %s, Channel: %s\n", evt.Queue, evt.Channel)
	logger.Debugf("CallerIDNum: %s, CallerIDName: %s\n", evt.CallerIDNum, evt.CallerIDName)
	logger.Debugf("ConnectedLineNum: %s, ConnectedLineName: %s\n", evt.ConnectedLineNum, evt.ConnectedLineName)
	logger.Debugf("Position: %d, Count: %d, Uniqueid: %s\n", evt.Position, evt.Count, evt.Uniqueid)
	logger.Debugf("------- End QueueCallerLeave Event -------\n")
}

func handleQueueLeave(evt event.QueueLeave) {
	/*
	   Event: Leave
	   Privilege: call,all
	   Channel: SIP/trunksip1-00129d59
	   Queue: queue-furrier51
	   Count: 0
	   Position: 1
	   Uniqueid: pbxA-1418426971.1273431
	   Custgroup: furrier51
	*/
	if processQueueEntries.Load().(bool) == false {
		logger.Debugf("[handleQueueJoin] Skipping processing of QueueJoin event as processing is disabled\n")
		return
	}
	logger.Debugf("QL= %#v", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			pQE, found := pQI.Entries[evt.Uniqueid]
			if found == true {
				delete(pQI.Entries, evt.Uniqueid)
				pQI.Calls = pQI.Calls - 1
				// Adjust all the positions of entries with a position higher than the one we just removed
				for _, v := range pQI.Entries {
					if v.Position > pQE.Position {
						v.Position = v.Position - 1
					}
				}
				logger.Debugf("After Leave, Queue Entries: %s\n", queuename)
				for _, v := range pQI.Entries {
					logger.Debugf("\t%d\t%s\t%s\n", v.Position, v.CallerIDName, v.WhenEntered)
				}
			}
			sendQueueCallersChange(orgID, queuename, pQI)
		}
	}
}

func handleQueueMemberStatus(evt event.QueueMemberStatus) {
	if processQueueEntries.Load().(bool) == false {
		return
	}
	log.Println("QMS=", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	logger.Debugf("Handling Queue Member Status for OrgID (%s), Queue (%s), event(%#v)", orgID, queuename, evt)
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			if pQM, found := pQI.Members[evt.Location]; found {
				pQM.Penalty = evt.Penalty
				pQM.CallsTaken = evt.CallsTaken
				pQM.LastCall = time.Unix(int64(evt.LastCall), 0)
				pQM.Status = evt.Status
				pQM.Paused = evt.Paused
				sendQMemberChange(orgID, queuename, "AGENTCHANGE", pQM)
			}
		}
	}
}

func handleQueueMemberAdded(evt event.QueueMemberAdded) {
	if processQueueEntries.Load().(bool) == false {
		return
	}
	log.Println("QMA=", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			pQM, found := pQI.Members[evt.Location]
			if !found {
				pQM = new(QUEUEMEMBER)
				pQI.Members[evt.Location] = pQM
				pQM.Location = evt.Location
			}
			pQM.Name = evt.MemberName
			pQM.Membership = evt.Membership
			pQM.DynamicAge = 0
			pQM.Penalty = evt.Penalty
			pQM.CallsTaken = evt.CallsTaken
			pQM.LastCall = time.Unix(int64(evt.LastCall), 0)
			pQM.WhenAdded = time.Unix(int64(evt.WhenAdded), 0)
			pQM.Status = evt.Status
			pQM.Paused = evt.Paused
			if !found {
				sendQMemberChange(orgID, queuename, "AGENTADD", pQM)
			} else {
				sendQMemberChange(orgID, queuename, "AGENTCHANGE", pQM)
			}
		}
	}
}

func handleQueueMemberRemoved(evt event.QueueMemberRemoved) {
	if processQueueEntries.Load().(bool) == false {
		return
	}
	log.Println("QMR=", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			_, found := pQI.Members[evt.Location]
			if found {
				sendQMemberRemove(orgID, queuename, evt.Location, evt.MemberName)
				delete(pQI.Members, evt.Location)
			}
		}
	}
}

func handleQueueAgentComplete(evt event.QueueAgentComplete) {
	/* This is an async event and looks like:
	   Event: AgentComplete
			"Queue: %s\r\n"
			"Uniqueid: %s\r\n"
			"Channel: %s\r\n"
			"Member: %s\r\n"
			"MemberName: %s\r\n"
			"HoldTime: %ld\r\n"
			"TalkTime: %ld\r\n"
			"Reason: %s\r\n"
	*/
	if processQueueEntries.Load().(bool) == false {
		return
	}
	log.Println("AC=", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			pQI.Completed = pQI.Completed + 1
			if evt.HoldTime < pQI.ServiceLevel {
				pQI.CallsCompletedInSL = pQI.CallsCompletedInSL + 1
			}
			pQI.ServicelevelPerf = math.Floor(1000.0*(float64(pQI.CallsCompletedInSL)/float64(pQI.Completed))) / 10.0
			// log.Printf("Calculated new SLP, %d / %d = %f\n", pQI.CallsCompletedInSL, pQI.Completed, pQI.ServicelevelPerf)
			calcAverageHoldTime(pQI, evt.HoldTime)
			calcAverageTalkTime(pQI, evt.TalkTime)
			sendQueueChange(orgID, queuename, pQI)
			if pQM, found := pQI.Members[evt.Member]; found { // note that we use evt.Member here, but it's called Location elsewhere (just inconsistent naming in Asterisk)
				pQM.CallsTaken = pQM.CallsTaken + 1
				pQM.LastCall = time.Now() // this might be slightly different (+- a few seconds) from that stored in the Asterisk
				sendQMemberChange(orgID, queuename, "AGENTCHANGE", pQM)
			}
		}
	}
}

func calcAverageHoldTime(pQI *QUEUEINFO, newHoldTime int) {
	oldvalue := pQI.HoldTime
	pQI.HoldTime = (((oldvalue << 2) - oldvalue) + newHoldTime) >> 2
}

func calcAverageTalkTime(pQI *QUEUEINFO, newTalkTime int) {
	// Calculate talktime using the exponential average
	oldtalktime := pQI.TalkTime
	pQI.TalkTime = (((oldtalktime << 2) - oldtalktime) + newTalkTime) >> 2
}

func handleQueueCallerAbandon(evt event.QueueCallerAbandon) {
	/* This is an async event and looks like:
	   Event: QueueCallerAbandon
	       "Queue: %s\r\n"
	       "Uniqueid: %s\r\n"
	       "Position: %d\r\n"
	       "OriginalPosition: %d\r\n"
	       "HoldTime: %d\r\n"
	       "Custgroup: %s\r\n",
	*/
	if processQueueEntries.Load().(bool) == false {
		return
	}
	log.Println("QCA=", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			pQI.Abandoned = pQI.Abandoned + 1
			sendQueueChange(orgID, queuename, pQI)
		}
	}
}

// Pause (instead of Paused) For asterisk 22+
func handleQueueMemberPause(evt event.QueueMemberPause) {
	if processQueueEntries.Load().(bool) == false {
		return
	}
	printQueueMemberPauseEvent(evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			if pQM, found := pQI.Members[evt.Interface]; found {
				pQM.Paused = evt.Paused
				sendQMemberChange(orgID, queuename, "AGENTCHANGE", pQM)
			}
		}
	}
}

func printQueueMemberPauseEvent(evt event.QueueMemberPause) {
	logger.Debugf("------- QueueMemberPause Event -------\n")
	logger.Debugf("Queue: %s\n", evt.Queue)
	logger.Debugf("Interface: %s\n", evt.Interface)
	logger.Debugf("Paused: %d\n", evt.Paused)
	logger.Debugf("MemberName: %s\n", evt.MemberName)
	logger.Debugf("Reason: %s\n", evt.Reason)
	logger.Debugf("------- End QueueMemberPause Event -------\n")
}

func handleQueueMemberPaused(evt event.QueueMemberPaused) {
	if processQueueEntries.Load().(bool) == false {
		return
	}
	log.Println("QMP=", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			if pQM, found := pQI.Members[evt.Location]; found {
				pQM.Paused = evt.Paused
				sendQMemberChange(orgID, queuename, "AGENTCHANGE", pQM)
			}
		}
	}
}

func handleQueueMemberPenalty(evt event.QueueMemberPenalty) {
	if processQueueEntries.Load().(bool) == false {
		return
	}
	log.Println("QMP=", evt)
	orgID, queuename, ok := queueNameToOrgID(evt.Queue)
	if ok == false {
		return
	}
	if pOrg := FindOrgInfoPtr(orgID); pOrg == nil {
		return
	} else {
		defer pOrg.Unlock()
		pQI, found := pOrg.Queues[queuename]
		if found == true {
			if pQM, found := pQI.Members[evt.Location]; found {
				pQM.Penalty = evt.Penalty
				sendQMemberChange(orgID, queuename, "AGENTCHANGE", pQM)
			}
		}
	}
}
