package main

import (
	"github.com/Simply-Bits/astmon/gami/event"
	"strings"
	"time"
)

func ExtensionStatus(m event.ExtensionStatus) {
	/* Event looks like:
	   type ExtensionStatus struct {
	       Privilege []string
	       Exten     string `AMI:"Exten"`
	       Context   string `AMI:"Context"`
	       Hint      string `AMI:"Hint"`
	       Status    int    `AMI:"Status"`
	   }
	   Event: ExtensionStatus
	   Privilege: call,all
	   Exten: 7110
	   Context: ls-intern
	   Hint: SIP/7110.4ls&SIP/7110ls
	   Status: 2
	*/
	var (
		orgID string
		pOrg  *ORGINFO
		tNow  time.Time
	)
	if processExtensionStatus.Load().(bool) == false {
		logger.Warnf("ExtensionStatus event received but processing is disabled\n")
		return
	}
	tNow = time.Now()
	parts := strings.SplitN(m.Context, "-", 2)
	if len(parts) == 2 {
		orgID = parts[0]
	}
	pOrg = GetOrgInfoPtr(orgID)
	defer pOrg.Unlock()
	pExtInfo, ok := pOrg.Extensions[m.Exten]
	if ok {
		/*
			AST_EXTENSION_REMOVED:                       "Removed",       // -2
			AST_EXTENSION_DEACTIVATED:                   "Deactivated",   // -1
			AST_EXTENSION_NOT_INUSE:                     "Idle",          // 0
			AST_EXTENSION_INUSE:                         "In-Use",        // 1
			AST_EXTENSION_BUSY:                          "In-Use",        // 2 * Changed this to InUse since so we don't show two different states to users
			AST_EXTENSION_UNAVAILABLE:                   "Unavailable",   // 4
			AST_EXTENSION_RINGING:                       "Ringing",       // 8
			AST_EXTENSION_ONHOLD:                        "Hold",          // 16
			AST_EXTENSION_INUSE | AST_EXTENSION_RINGING: "In-Use (Ring)", // 9
			AST_EXTENSION_INUSE | AST_EXTENSION_ONHOLD:  "In Use (Hold)", // 17
		*/
		if pExtInfo.Status == AST_EXTENSION_NOT_INUSE || m.Status == AST_EXTENSION_NOT_INUSE {
			// Old status was IDLE OR new status is going to IDLE, so record the time stamp
			pExtInfo.LastStatusChange = tNow
		}
		if m.Status == AST_EXTENSION_NOT_INUSE {
			// Going to IDLE, so clear these
			pExtInfo.ConnectedLineNum = ""
			pExtInfo.ConnectedLineName = ""
		}
		pExtInfo.Status = m.Status
	} else {
		// this extension must not have existed during the initial load, so add it
		pExtInfo = new(EXTENSIONINFO)
		pExtInfo.Exten = m.Exten
		pExtInfo.Status = m.Status
		pExtInfo.LastStatusChange = tNow
		pExtInfo.App = m.Hint
		if len(pExtInfo.App) == 0 {
			pExtInfo.Type = EXTENSIONINFO_TYPE_ENDPOINT
			pExtInfo.Label = m.Exten
		} else {
			pExtInfo.Type, pExtInfo.Label = GetHintType(pExtInfo.App)
		}
		pOrg.Extensions[m.Exten] = pExtInfo
	}
	sendEventHintChange(orgID, pExtInfo)

}

func GetHintType(application string) (t int, label string) {
	parts := strings.SplitN(application, "&", 2)
	if len(parts) == 0 {
		t = EXTENSIONINFO_TYPE_UNKNOWN
		label = ""
		return
	}
	t = EXTENSIONINFO_TYPE_CUSTOM
	label = "Custom"
	application = strings.ToUpper(parts[0])
	if len(application) > 4 {
		if application[0:4] == "SIP/" {
			t = EXTENSIONINFO_TYPE_ENDPOINT
			DeviceDescMapMutex.RLock()
			label, _ = DeviceDescMap[parts[0]]
			DeviceDescMapMutex.RUnlock()
		} else if application[0:10] == "CUSTOM:DND" {
			t = EXTENSIONINFO_TYPE_ENDPOINT
			label = "DND"
		} else if application[0:5] == "PARK:" {
			t = EXTENSIONINFO_TYPE_PARK
			label = "Lot " + application[5:9]
		}
	}
	return
}

func NewState(m event.Newstate) {
	/* Event looks like:
	Event: Newstate
	Privilege: call,all
	Channel: SIP/7110ls-00059dd3
	ChannelState: 6
	ChannelStateDesc: Up
	CallerIDNum: 5205450410
	CallerIDName: Joe
	ConnectedLineNum: 5202450700
	ConnectedLineName: CRACCHIOLO JOE
	Uniqueid: pbxA-1489102900.389669
	*/
	var (
		orgID string
		exten string
		valid bool
		found bool
		pOrg  *ORGINFO
		pExt  *EXTENSIONINFO
	)
	if processExtensionStatus.Load().(bool) == false {
		return
	}
	if m.ChannelState != 6 { // 6 = Up
		return
	}
	// Get from authID to an OrgID and extension (see authIDtoOrgID for warnings)
	orgID, exten, valid = authIDtoOrgID(m.Channel)
	if valid == false {
		logger.Warnf("NewState: Unable to get OrgID from %s", m.Channel)
		return
	}
	if pOrg = FindOrgInfoPtr(orgID); pOrg != nil {
		pExt, found = pOrg.Extensions[exten]
		if found {
			pExt.ConnectedLineNum = m.ConnectedLineNum
			pExt.ConnectedLineName = m.ConnectedLineName
			sendEventHintChange(orgID, pExt)
		} else {
			logger.Warnf("NewState: Unable to find extension for %s", exten)
		}
		pOrg.Unlock()
	} else {
		logger.Warnf("NewState: Cannot find OrgID %s", orgID)
	}
}
