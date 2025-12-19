package main

import (
	"github.com/Simply-Bits/astmon/gami/event"
	"time"
)

func AddParkedCall(m event.ParkedCall) {
	var (
		pOrg *ORGINFO
	)
	printParkedCall(m)
	pOrg = GetOrgInfoPtr(m.ParkingLot) // in ast 22+ the parking lot name is now just the orgID
	defer pOrg.Unlock()
	m.ParkedAt = time.Now() // It really doesn't matter when it was parked, combining the timeout(at current time) with the current time is sufficient to track timeouts
	pOrg.ParkingLots[m.ParkeeChannel] = m
	sendReload()
}

func RemoveParkedCall(removeOrgID string, channel string) {
	var (
		pOrg *ORGINFO
	)
	logger.Debugf("Removing parked call from orgID %s, channel %s", removeOrgID, channel)
	// in ast 22+ the parking lot name is now just the orgID
	if pOrg = FindOrgInfoPtr(removeOrgID); pOrg != nil {
		delete(pOrg.ParkingLots, channel)
		pOrg.Unlock()
	}
	sendReload()
}

func printParkedCall(m event.ParkedCall) {
	logger.Debugf(" ------ Parked Call ------ ")
	logger.Debugf(" ParkeeExten:             %s", m.ParkeeExten)
	logger.Debugf(" ParkeeChannel:          %s", m.ParkeeChannel)
	logger.Debugf(" ParkeeChannelState:     %s", m.ParkeeChannelState)
	logger.Debugf(" ParkeeChannelStateDesc: %s", m.ParkeeChannelStateDesc)
	logger.Debugf(" ParkeeContext:          %s", m.ParkeeContext)
	logger.Debugf(" ParkeeLanguage:         %s", m.ParkeeLanguage)
	logger.Debugf(" ParkeeUniqueID:         %s", m.ParkeeUniqueID)
	logger.Debugf(" ParkeeLinkedID:         %s", m.ParkeeLinkedID)
	logger.Debugf(" ParkeePriority:         %s", m.ParkeePriority)
	logger.Debugf(" ParkeeAccountCode:      %s", m.ParkeeAccountCode)
	logger.Debugf(" ParkerDialString:       %s", m.ParkerDialString)
	logger.Debugf(" ParkingDuration:        %s", m.ParkingDuration)
	logger.Debugf(" ParkeeCallerIDNum:      %s", m.ParkeeCallerIDNum)
	logger.Debugf(" ParkeeCallerIDName:     %s", m.ParkeeCallerIDName)
	logger.Debugf(" ParkeeConnectedLineNum: %s", m.ParkeeConnectedLineNum)
	logger.Debugf(" ParkeeConnectedLineName:%s", m.ParkeeConnectedLineName)
	logger.Debugf(" ParkingLot:             %s", m.ParkingLot)
	logger.Debugf(" ParkingSpace:           %s", m.ParkingSpace)
	logger.Debugf(" ParkingTimeout:         %s", m.ParkingTimeout)
	logger.Debugf(" ------------------------ ")
}
