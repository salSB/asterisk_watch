package main

import (
	"database/sql"
	"github.com/Simply-Bits/astmon/gami"
	"github.com/Simply-Bits/astmon/gami/event"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	baseSIPAuthIDRegexp = regexp.MustCompile(`^SIP/(7[0-9]{3})(?:\.\d)?([a-zA-Z][a-zA-Z0-9]+)(\-[[:xdigit:]]+)?$`)
)

func authIDtoOrgID(authID string) (orgID string, exten string, valid bool) {
	/* for baseSIPAuthIDRegexp:
	   input="SIP/7102acme"
	       m[0]=SIP/7102acme
	       m[1]=7102
	       m[2]=acme
	   input="SIP/7102.2acme"
	       m[0]=SIP/7102.2acme
	       m[1]=7102
	       m[2]=acme
	   input="SIP/7102.2acme-00055678"
	       m[0]=SIP/7102.2acme-00055678
	       m[1]=7102
	       m[2]=acme
	       m[3]=-00055678
	*/

	//*******************************************************************************
	// WARNING!  We are assuming that an AuthID of "SIP/7###.#acme" or "SIP/7###acme"
	// maps to extension 7### via a hint.  For example, SIP/7110.2ls maps via a hint
	// to exten 7110 and SIP/7110ls also maps to exten 7110.
	// If this assumption is broken in Asterisk dialplans, then this code will not
	// work.  In such case we would need to store the hint "App" data and map back
	// from that list to an extension number.
	//*******************************************************************************

	var m []string
	logger.Debugf("Decoding AuthID %s\n", authID)
	if strings.HasPrefix(authID, "PJSIP/") {
		logger.Debugf("AuthID %s is PJSIP, not SIP, so cannot decode\n", authID)
		authID = strings.Replace(authID, "PJSIP/", "SIP/", 1)
		logger.Debugf("Changed AuthID to %s for decoding\n", authID)
	}
	m = baseSIPAuthIDRegexp.FindStringSubmatch(authID)
	if len(m) < 2 || len(m[1]) == 0 {
		//log.Printf("Unable to decode AuthID %s\n", authID)
		return
	}
	exten = m[1]
	orgID = m[2]
	valid = true
	return
}

/*
	Event looks like:

	type PeerStatus struct {
		Privilege   []string                   -> [system all]
		ChannelType string `AMI:"Channeltype"` -> SIP
		Peer        string `AMI:"Peer"`        -> SIP/7554.2ls
		PeerStatus  string `AMI:"Peerstatus"`  -> Registered
	}

Event: PeerStatus
Privilege: system,all
ChannelType: SIP
Peer: SIP/7554.2ls
PeerStatus: Registered
Address: 64.119.41.110:5063
*/
func handlePeerStatus(evt event.PeerStatus) {
	if evt.PeerStatus != "Registered" {
		return
	}
	// Get from authID to an OrgID and extension (see authIDtoOrgID for warnings)
	orgID, exten, ok := authIDtoOrgID(evt.Peer)
	if ok == false {
		return
	}
	DeviceDescMapMutex.RLock()
	dispName, found := DeviceDescMap[evt.Peer]
	DeviceDescMapMutex.RUnlock()
	if !found {
		if dispName, found = DBFindAuthID(orgID, exten); !found {
			log.Printf("Unable to find peer %s in the AstConfig database\n", evt.Peer)
			return
		}
	}
	pOrg := GetOrgInfoPtr(orgID)
	if _, found := pOrg.Extensions[exten]; !found {
		// No hint was found for this extension (7###) when the initial Asterisk Database dump was processed
		// so let's add a new entry dynamically.
		pExtInfo := new(EXTENSIONINFO)
		pExtInfo.Exten = exten
		pExtInfo.Status = AST_EXTENSION_NOT_INUSE
		pExtInfo.LastStatusChange = time.Now()
		pExtInfo.Type = EXTENSIONINFO_TYPE_ENDPOINT
		pExtInfo.Label = dispName
		pOrg.Extensions[exten] = pExtInfo

		// Now we need to get Asterisk to add the hint to its internal list of hints (in memory data structure)
		// so that it will start sending us ExtensionStatus events.  To get Asterisk to add it, we do an
		// ExtensionState AMI command.  Since we're going across the network to another process, we don't
		// want to keep the ORG locked so we unlock it first.
		pOrg.Unlock()
		resp, err := pAMI.Action("ExtensionState", gami.Params{"Context": orgID + "-intern", "Exten": exten})
		if err != nil {
			log.Printf("Error calling ExtensionState command\n")
			return
		}
		/* Resp looks like:

		&{
			ID: gamigeneral_727887
		   	Status: Success
		   	Params: map[
		   		Context:ls-intern
		   		Hint:SIP/7504ls&SIP/7504.2ls&Custom:DND7504ls
		   		Status:0
		   		Actionid:gamigeneral_727887
		   		Message:Extension Status
		   		Exten:7504
		   	]
		}
		*/
		pOrg.Lock()
		pExtInfo = pOrg.Extensions[exten]
		pExtInfo.Status, _ = strconv.Atoi(resp.Params["Status"])
		pExtInfo.LastStatusChange = time.Now()
		pExtInfo.App, _ = resp.Params["Hint"]
		pExtInfo.Type, pExtInfo.Label = GetHintType(pExtInfo.App)
	}
	pOrg.Unlock()
}

func DBFindAuthID(orgID string, exten string) (string, bool) {
	var (
		authID   string
		dispName sql.NullString
	)
	authID = exten + orgID
	err := DB.QueryRow(`SELECT fullname from sippeers WHERE name=? LIMIT 1`, authID).Scan(&dispName)
	/*
		err := DBAstConfig.QueryRow(`select AstConfig.phone_registrations.display_name
			from AstConfig.phone_registrations
			where AstConfig.phone_registrations.auth_userID=? limit 1`, authID).Scan(&dispName)
	*/
	if err != nil {
		return "", false
	}
	DeviceDescMapMutex.Lock()
	DeviceDescMap[authID] = dispName.String
	DeviceDescMapMutex.Unlock()
	return dispName.String, true
}
