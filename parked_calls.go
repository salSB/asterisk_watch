package main

import "github.com/Simply-Bits/astmon/gami/event"

func AddParkedCall(m event.ParkedCall) {
	/* Event m looks like:
	   Event: 'ParkedCall',
	   Privilege: 'call,all',
	   Exten: '7801',
	   Channel: 'SIP/trunksip1-00108c0a',
	   Parkinglot: 'parkinglot_furrier',
	   From: 'SIP/7128furrier-00108c0d',
	   Timeout: 300,
	   CallerIDNum: '5203581545',
	   CallerIDName: 'TUCSON,AZ',
	   ConnectedLineNum: '5207481300',
	   ConnectedLineName: 'Jesse Clark',
	   Uniqueid: 'pbxA-1417817584.1132773'     ***  this only appears on an async event not when we enum with show parked calls
	   ]
	*/
	var (
		orgID string
		pOrg  *ORGINFO
	)
	if m.Parkinglot[:11] == "parkinglot_" {
		orgID = m.Parkinglot[11:]
		pOrg = GetOrgInfoPtr(orgID)
		defer pOrg.Unlock()
		pOrg.ParkingLots[m.Exten] = m
		/*
		   // Update the "connected to" in the Inbound channel data since they are parked now
		   if ( (dataID = channel_map.get(evt.channel)) ) {
		       dataID.update({connectedDisplayName: 'Parked in lot '+evt.exten});
		   }
		*/
	}
}

func RemoveParkedCall(m event.ParkedCall) {
	/* These events looks like:
	   { event: 'UnParkedCall',
	       privilege: 'call,all',
	       exten: '7801',
	       channel: 'SIP/trunksip1-00108bde',
	       parkinglot: 'parkinglot_furrier21',
	       from: 'SIP/7211furrier21-00108bfb',
	       calleridnum: '5202194388',
	       calleridname: 'Gerber Collisio',
	       connectedlinenum: '5207441308',
	       connectedlinename: '21 Front 1',
	       uniqueid: 'pbxA-1417817516.1132743' }
	   { event: 'ParkedCallGiveUp',
	       privilege: 'call,all',
	       exten: '7801',
	       channel: 'SIP/trunksip1-00108bbc',
	       parkinglot: 'parkinglot_furrier',
	       calleridnum: '5203581545',
	       calleridname: 'TUCSON,AZ',
	       custgroup: '',
	       connectedlinenum: '5207481700',
	       connectedlinename: 'Danny Pena',
	       uniqueid: 'pbxA-1417817478.1132717' }
	   { event: 'ParkedCallTimeOut',
	       privilege: 'call,all',
	       exten: '7801',
	       channel: 'SIP/trunksip1-00108bbc',
	       parkinglot: 'parkinglot_furrier',
	       calleridnum: '5203581545',
	       calleridname: 'TUCSON,AZ',
	       custgroup: '',
	       connectedlinenum: '5207481700',
	       connectedlinename: 'Danny Pena',
	       uniqueid: 'pbxA-1417817478.1132717' }
	*/
	var (
		orgID string
		pOrg  *ORGINFO
	)
	if m.Parkinglot[:11] == "parkinglot_" {
		orgID = m.Parkinglot[11:]
		if pOrg = FindOrgInfoPtr(orgID); pOrg != nil {
			delete(pOrg.ParkingLots, m.Exten)
			pOrg.Unlock()
		}
	}
	/*
	   if (evt.event === 'UnParkedCall') {
	       // Update the "connected to" in any Inbound channel data
	       if ( (dataID = channel_map.get(evt.channel)) ) {
	           var connectedChannel = evt.from.split('-', 1);
	           var connectedDisplayName;
	           if (connectedChannel) {
	               connectedChannel = connectedChannel[0];
	               if ( (connectedDisplayName = authID2Desciption_map.get(connectedChannel)) ) {
	                   dataID.update({connectedDisplayName: connectedDisplayName});
	               }
	           }
	       }
	   } else if (evt.event === 'ParkedCallTimeOut') {
	       // Update the "connected to" in any Inbound channel data
	       if ( (dataID = channel_map.get(evt.channel)) ) {
	           dataID.update({connectedDisplayName: null});
	       }
	   }
	*/
}
