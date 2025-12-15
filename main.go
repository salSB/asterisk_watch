package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"error
	"flag"
	"fmt"
	"github.com/Simply-Bits/astmon/gami"
	"github.com/Simply-Bits/astmon/gami/event"
	"github.com/Simply-Bits/go.uuid"
"
	"github.com/boj/redistore"
	_ "g
	_ "github.com/go-sql-driver/mysql"
	"github.com/gorilla/mux"
	"github.com/justinas/alice"
	"github.com/sirupsen/logrus"
	logrus_syslog "github.com/sirupsen/logrus/hooks/syslog"
	"html/template"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"github.com/boj/redistore"
)

// Session constants
const (
	//                  12345678901234567890123456789012
	SESSION_ENC_KEY  = "kdjfsdhf89shdkfhsd0ghihiosdhfg9h"
	SESSION_AUTH_KEY = "sdfuhr98sdyfnzdzvou;lw3nrfz9d8gv"
	SESSION_NAME     = "ftgsess"
)

const (
	AST_EXTENSION_REMOVED     = -2     // Extension removed
	AST_EXTENSION_DEACTIVATED = -1     // Extension hint removed
	AST_EXTENSION_NOT_INUSE   = 0      // No device INUSE or BUSY
	AST_EXTENSION_INUSE       = 1 << 0 // One or more devices INUSE
	AST_EXTENSION_BUSY        = 1 << 1 // All devices BUSY
	AST_EXTENSION_UNAVAILABLE = 1 << 2 // All devices UNAVAILABLE/UNREGISTERED
	AST_EXTENSION_RINGING     = 1 << 3 // All devices RINGING
	AST_EXTENSION_ONHOLD      = 1 << 4 // All devices ONHOLD
)

const (
	USERFLAG_SUPERUSER  = 0x00000001
	USERFLAG_DISABLED   = 0x00000002
	USERFLAG_ADMIN      = 0x00000004
	USERFLAG_BILLING    = 0x00000008
	USERFLAG_STAFFADMIN = 0x00000010
	USERFLAG_RESERVED   = 0x00000020
	USERFLAG_STAFFUSER  = 0x00000040

	MGMTGROUPS_ALL = 0x7FFFFFFFFFFFFFFF
)

type UserEntry struct {
	ID              uint64 `json:"id"`
	LoginID         string `json:"loginid"`
	Password        string // only filled in when saving a UserEntry -- it's a write-only field
	Flags           uint32 `json:"flags"`
	OrgID           uint64 `json:"orgid"`
	MgmtGroups      uint64
	Referer         string
	AvailAccounts   []AvailAccountEntry
	AvailAcctCounts [6]int   // indexed by ACCTTYPE_???
	AvailTN         []string // array of TN's to which the user has R/O or R/W permissions
}

type AvailAccountEntry struct {
	Desc string
	ID   uint64
}

type EXTENSIONINFO struct {
	Exten             string
	Status            int
	LastStatusChange  time.Time
	App               string
	Type              int
	Label             string
	ConnectedLineNum  string
	ConnectedLineName string
}

type QUEUEMEMBER struct {
	Name       string
	Location   string
	Membership string
	DynamicAge int // how long they have been a member in seconds
	Penalty    int
	CallsTaken int
	LastCall   time.Time
	WhenAdded  time.Time
	Status     int
	Paused     int
}

type QUEUEENTRY struct {
	Position          int
	CountWhenEntered  int // number of calls in queue when this call entered
	Channel           string
	Uniqueid          string
	CallerIDNum       string
	CallerIDName      string
	ConnectedLineNum  string
	ConnectedLineName string
	WhenEntered       time.Time
	SecsInQueue       int
}

type QUEUEINFO struct {
	Max                int // Maxmimum number of calls allowed
	Strategy           string
	Calls              int // Current number of calls in the queue
	HoldTime           int // Current exponential average hold time
	TalkTime           int // Current exponential average talktime
	Completed          int // Number of completed calls for the queue
	Abandoned          int
	ServiceLevel       int
	ServicelevelPerf   float64
	Weight             int
	WhenStatsCleared   time.Time
	CallsCompletedInSL int
	Entries            map[string]*QUEUEENTRY  // Uniqueid->Entry
	Members            map[string]*QUEUEMEMBER // Location->Member
}

type QUEUEPAGEINFO struct {
	Name              string
	ShowAgents        int
	ShowCalls         int
	QI                QUEUEINFO
	CallerPositionMap []*QUEUEENTRY // sorted list of pointers to callers
}

type EXTPAGEINFO struct {
	OrgID            string
	Exten            string
	ShowCID          int
	Type             int
	Label            string
	Status           int
	StatusStr        string
	SecsInStatus     int   // calculated seconds in this status
	LastStatusChange int64 // when this status was entered (formatted as milliseconds elapsed since 1 January 1970 00:00:00 UTC)
}

type VARPAGEINFO struct {
	Name    string
	Type    string
	Label   string
	Options []string
	Values  []string
}

type ORGINFO struct {
	OrgID string
	sync.Mutex
	Extensions  map[string]*EXTENSIONINFO // [key: SIP/7xxx{orgID}, value: Display name]
	ParkingLots map[string]event.ParkedCall
	Queues      map[string]*QUEUEINFO // Queuename index
}

type PanelLayout struct {
	LayoutType int                `json:"layout"`
	ExtensOld  []string           `json:"extens"` // ordered list of extensions
	Extens     []PanelLayoutExten `json:"extens2"`
	Queues     []PanelLayoutQueue `json:"queues"`
	Variables  []string           `json:"vars"`
}

type PanelLayoutExten struct {
	Exten   string `json:"exten"`
	ShowCID int    `json:"showcid"`
}

type PanelLayoutQueue struct {
	QueueName  string `json:"qn"`
	ShowAgents int    `json:"showagents"`
	ShowCalls  int    `json:"showcalls"`
}

const (
	EXTENSIONINFO_TYPE_UNKNOWN  = 0
	EXTENSIONINFO_TYPE_ENDPOINT = 1
	EXTENSIONINFO_TYPE_PARK     = 2
	EXTENSIONINFO_TYPE_CUSTOM   = 3
)

var (
	Config                 Configuration
	logger                 *logrus.Logger
	hook                   *logrus_syslog.SyslogHook
	DB                     *sql.DB
	DBAstConfig            *sql.DB
	ServerInstance         uuid.UUID // Redis session server instance ID
	SessionStore           *redistore.RediStore
	queuesReloaded         chan int
	mapInprocessChannels   map[string]bool
	chanHangups            chan string
	chanNewChannels        chan string
	processExtensionStatus atomic.Value
	processQueueEntries    atomic.Value
	pAMI                   *gami.AMIClient
	AMIEvents              map[string]chan *gami.AMIEvent
	AMIEventsMutex         sync.RWMutex
	DeviceDescMap          map[string]string // [key: SIP/7xxx{orgID}, value: Display name]
	DeviceDescMapMutex     sync.RWMutex
	ctxExiting             context.Context
	orgMapRWMutex          sync.RWMutex
	orgMap                 = map[string]*ORGINFO{}
)

func main() {
	var (
		err          error
		configPath   string
		stdOut       bool
		h            *http.Server
		pgmTerminate context.CancelFunc
		resp         *gami.AMIResponse
	)
	flag.BoolVar(&Config.Debug, "debug", false, "Log more verbose debug information")
	flag.BoolVar(&stdOut, "stdout", false, "Log to stdout instead of syslog")
	flag.StringVar(&configPath, "config", "/etc/asterisk_watch.yml", "Configuration file to use")
	flag.Parse()
	// Logger Setup
	logger, err = setupLogger(Config.Debug, stdOut)
	if err != nil {
		logger.Fatalf("Error setting up logger: %v", err)
	}
	// Configuration Load
	err = Config.Load(configPath)
	if err != nil {
		logger.Fatalf("Error loading configuration: %v", err)
	}
	if Config.Debug {
		logger.Debugf("Configuration loaded from %s", configPath)
		// Print Configuration
		Config.Print()
	}
	// Influx Setup
	if Config.HasInfluxConfig() {
		if err = initInflux(); err != nil {
			logger.Fatalf("Error initializing InfluxDB: %s", err)
		}
		defer closeInflux()
	}
	// Redis Setup
	ServerInstance, _ = uuid.NewV4()
	SessionStore, err = redistore.NewRediStoreWithDB(10, "tcp", Config.Redis.Host, "", "", "1", []byte(SESSION_AUTH_KEY), []byte(SESSION_ENC_KEY))
	if err != nil {
		logger.Fatalf("Unable to connect to Redis at %s, error %s\n", Config.Redis.Host, err)
	}
	// Some startup log messages
	logger.Infof("asterisk_watch (%s) starting at %s, binding to %s \n", os.Args[0], time.Now(), Config.Web.SSLBindAddr)

	// Channels and Maps Initialization
	queuesReloaded = make(chan int)     // channel to signal queue reload events
	processExtensionStatus.Store(false) // sets atomic flag indicating extension status processing is inactive
	processQueueEntries.Store(false)    // sets atomic flag indicating queue entry processing is inactive

	// Call Tracking
	mapInprocessChannels = make(map[string]bool, 2048) // Allocates map to track active call channels
	chanNewChannels = make(chan string, 2048)          // creates buffered channel for new call notifications
	chanHangups = make(chan string, 2048)              // creates buffered channel for call hangup notifications

	// AMI Connection
	pAMI, err = gami.Dial(Config.AMI.ConnectString)
	if err != nil {
		logger.Fatalf("Error connecting to AMI at %s: %v", Config.AMI.ConnectString, err)
	}
	AMIEvents = make(map[string]chan *gami.AMIEvent)
	pAMI.Run()
	go handleAMI(pAMI)

	// FTG DB
	DB, err = sql.Open("mysql", Config.Database.ConnectString)
	if err != nil {
		logger.Fatalf("Error connecting to FTG database: %v", err)
	}
	logger.Debugf("Connected to FTG database at %s\n", Config.Database.Host)
	defer func() { _ = DB.Close() }()

	// ASTCONFIG DB
	DBAstConfig, err = sql.Open("mysql", Config.Database.AstConnectString)
	if err != nil {
		logger.Fatalf("Error connecting to AstConfig database: %v", err)
	}
	logger.Debugf("Connected to AstConfig database at %s\n", Config.Database.Host)
	defer func() { _ = DBAstConfig.Close() }()

	logger.Debugf("Building Device Description Map from SIP Peers\n")
	// Device Description Map from SIP Peers view
	DeviceDescMap = make(map[string]string, 1024)
	BuildSipDescriptionMap()
	if Config.Debug {
		PrintDeviceDescMap()
	}
	logger.Debugf("Finished Building SIP Description Map\n")

	// Legacy AMI support has been REMOVED - see older versions of astmon for reference
	// Login to AMI with Events
	if err = pAMI.Login(Config.AMI.User, Config.AMI.Pass); err != nil {
		logger.Fatalf("Error logging into AMI: %v", err)
	}
	logger.Infof("Connected and logged into AMI at %s\n", Config.AMI.ConnectString)

	// Events On
	_, err = pAMI.Action("Events", gami.Params{"EventMask": "on"})
	if err != nil {
		logger.Fatalf("Error enabling AMI events: %s", err)
	}
	logger.Debugf("AMI EventMask Enabled")

	// Get initial Extension Status
	// After ExtensionStateList action
	if resp, err = pAMI.Action("ExtensionStateList", nil); err != nil {
		logger.Errorf("Error getting ExtensionStateList from AMI: %s", err)
	} else {
		processExtensionStatus.Store(true)
		logger.Debugf("ExtensionStateList Response=%+v\n", resp)
	}

	// Detect Asterisk Version
	var DetectedVersion string
	if resp, err = pAMI.Action("CoreSettings", nil); err != nil {
		logger.Fatalf("Error getting Asterisk version from AMI: %s", err)
	} else {
		if v, found := resp.Params["Asteriskversion"]; found {
			DetectedVersion = v
		}
		logger.Infof("Asterisk version %s detected [System: %s] [AMI Version: %s]\n", DetectedVersion, resp.Params["Systemname"], resp.Params["Amiversion"])
	}

	// Initial Queue Status to populate existing queue entries
	logger.Debugf("Using QueueStatus Action\n")
	if resp, err = pAMI.Action("QueueStatus", nil); err != nil {
		logger.Fatalf("Error getting initial QueueStatus from AMI: %s", err)
	} else if resp.Status == "Success" {
		processQueueEntries.Store(true)
		logger.Debugf("QueueStatus Action Started\n")
	}

	// go asyncNotifyHub
	go asyncNotifyHub.run()
	initError := InitInuseChannelMap()
	if initError != nil {
		logger.Errorf("failed initializing in-use channel map: %s\n", initError)
	}

	// Populate in-use channel map
	go processInUseChannels()
	periodicQueueRefresh()

	// Graceful Shutdown Context
	ctxExiting, pgmTerminate = context.WithCancel(context.Background())
	signalChannel := make(chan os.Signal, 2)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGINT)
	go func() {
		sig := <-signalChannel
		switch sig {
		case os.Interrupt, syscall.SIGTERM:
			pgmTerminate()
		}
	}()

	// HTTPS handling
	commonHandlers := alice.New(loggingHandler, authHandler)

	// The order of the Handle() calls is important here, be careful rearranging these
	r := mux.NewRouter()
	// SSL Cert Renewal Handler
	rAcme := mux.NewRouter()
	rAcme.PathPrefix("/.well-known/acme-challenge/").Handler(http.StripPrefix("/.well-known/acme-challenge/",
		http.FileServer(http.Dir(filepath.Join(Config.Web.TemplatePath, "/.well-known/acme-challenge/")))))
	log.Println("Starting ACME HTTP server")
	go func() {
		logger.Fatal(http.ListenAndServe("0.0.0.0:99", rAcme))
	}()
	// Favicon.ico Handler
	r.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Join(Config.Web.TemplatePath, "/static/favicon.ico")
		var file *os.File
		if file, err = os.Open(name); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			http.Error(w, "", 401)
			return
		} else {
			defer func() { _ = file.Close() }()
			http.ServeContent(w, r, name, time.Now(), file)
		}
	})
	// Static files Handler
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/",
		http.FileServer(http.Dir(filepath.Join(Config.Web.TemplatePath, "/static/")))))
	// Websocket Handler
	r.HandleFunc("/ws", serveWS)

	r.Handle("/edit/{panelID}", commonHandlers.ThenFunc(editLayout)).Methods("GET")
	r.Handle("/layout/1/{panelID}", commonHandlers.ThenFunc(saveLayout)).Methods("PUT", "POST")
	r.Handle("/vars/get/{panelID}", commonHandlers.ThenFunc(VarGet))
	r.Handle("/vars/set/{panelID}", commonHandlers.ThenFunc(VarSet)).Methods("POST")
	r.Handle("/{panelID}", commonHandlers.ThenFunc(panelGet)).Methods("GET")
	r.HandleFunc("/", homeGet).Methods("GET")

	idleConnsClosed := make(chan struct{})
	logger.Infof("Starting HTTPS server")
	h = &http.Server{Addr: Config.Web.SSLBindAddr, Handler: r}
	go func() {
		if err = h.ListenAndServeTLS(Config.Web.SSLCertFile, Config.Web.SSLPrivateKeyFile); err != nil {
			logger.Fatal(err)
		}
	}()

	<-ctxExiting.Done()
	log.Println("asterisk_watch Shutting down")
	pAMI.Close()
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	func() { _ = h.Shutdown(ctx) }()
	close(idleConnsClosed)
	<-idleConnsClosed
}

type EXTPAGEINFOByExten []EXTPAGEINFO

func (a EXTPAGEINFOByExten) Len() int           { return len(a) }
func (a EXTPAGEINFOByExten) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a EXTPAGEINFOByExten) Less(i, j int) bool { return a[i].Exten < a[j].Exten }

func homeGet(w http.ResponseWriter, r *http.Request) {
	if templatePtr, err := getTemplate("home.tmpl"); err != nil {
		logger.Error(err)
		http.Error(w, "Template retrieval", 404)
		return
	} else {
		if err = templatePtr.Execute(w, nil); err != nil {
			logger.Error(err)
			http.Error(w, "Template execution error", 404)
			return
		}
	}
}

func loggingHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		t1 := time.Now()
		next.ServeHTTP(w, r)
		t2 := time.Now()
		logger.Infof("[%s] %s %q %v\n", r.RemoteAddr, r.Method, r.URL.String(), t2.Sub(t1))
	}
	return http.HandlerFunc(fn)
}

func authHandler(next http.Handler) http.Handler {
	fn := func(w http.ResponseWriter, r *http.Request) {
		var (
			err error
		)
		session, err := SessionStore.Get(r, SESSION_NAME)
		if err != nil {
			// Session likely deleted, just force reauth
			log.Printf("Session Error: %s\n", err)
			noCacheHeaders(w)
			http.Redirect(w, r, "https://uc.simplybits.net/login", http.StatusFound)
			return
		}
		if session.IsNew {
			// Check user & password
			log.Println("New session, needing login")
			http.Redirect(w, r, "https://uc.simplybits.net/login", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	}

	return http.HandlerFunc(fn)
}

func noCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache, private, max-age=0")
	w.Header().Set("Expires", time.Unix(0, 0).Format(http.TimeFormat))
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Accel-Expires", "0")
}

func processInUseChannels() {
	var (
		Channel string
	)
	for {
		select {
		case <-ctxExiting.Done():
			return
		case Channel = <-chanNewChannels:
			if len(Channel) > 14 && Channel[0:14] == "SIP/trunksip1-" {
				mapInprocessChannels[Channel] = true
				saveInfluxNumTrunkChannels(len(mapInprocessChannels), "PSTN")
			}
		case Channel = <-chanHangups:
			if len(Channel) > 14 && Channel[0:14] == "SIP/trunksip1-" {
				delete(mapInprocessChannels, Channel)
				saveInfluxNumTrunkChannels(len(mapInprocessChannels), "PSTN")
			}
		}
	}
}

func periodicQueueRefresh() {
	ticker := time.NewTicker(60 * time.Minute)
	go func() {
		for {
			select {
			case <-queuesReloaded:
				doQueueRefresh()
			case <-ticker.C:
				doQueueRefresh()
			}
		}
	}()
}

func doQueueRefresh() {
	processQueueEntries.Store(false)
	if resp, err := pAMI.Action("QueueStatus", nil); err != nil {
		logger.Fatalf("Error getting QueueStatus from AMI: %s", err)
	} else {
		processQueueEntries.Store(true)
		logger.Debugf("QueueStatus Response=%+v\n", resp)
		sendReload()
	}
}

func handleAMI(pAMI *gami.AMIClient) {
	for {
		select {
		case err := <-pAMI.NetError: // handle network errors
			logger.Errorf("AMI Network Error: %s", err)
			<-time.After(time.Second)
			if err2 := pAMI.Reconnect(); err2 == nil {
				if _, err3 := pAMI.Action("Events", gami.Params{"EventMask": "on"}); err3 != nil {
					logger.Errorf("Error re-enabling AMI events after reconnect: %s", err3)
				}
			} else {
				logger.Errorf("Error reconnecting to AMI: %s", err2)
			}

		case err := <-pAMI.Error:
			logger.Errorf("AMI Error: %s", err)

		case pEV := <-pAMI.Events:
			logger.Debugf("-- [Inbound AMI Event] --")
			//printEv(pEV)
			//if GC.LegacyAMI {
			//	/* *pEV (AMIEvent) looks like {
			//	       ID: "Newstate"
			//	       Privilege: [call all]
			//	       Params: map[Channel:SIP/7126furrier-0015e729 Channelstate:6 Channelstatedesc:Up Connectedlinename:ROYAL AUTO Uniqueid:pbxA-1470679849.1518133 Calleridnum:5207481300 Calleridname:Fred Duarte Connectedlinenum:5207904132]
			//	   }
			//	   event.New(ev) turns AMIEvent into an interface{
			//	       ID:QueueMemberStatus
			//	       Privilege:[agent all]
			//	       Params:map[
			//	           Custgroup:ls
			//	           Membership:dynamic
			//	           Whenadded:1463418121
			//	           Status:1
			//	           Callstaken:0
			//	           Paused:0 Penalty:0 Membername:Lance Jones Lastcall:0 Queue:ls-sbsales-commercial Location:SIP/7148.2ls
			//	       ]
			//	   }
			//	*/
			//	if i := event.New(pEV); i == nil || reflect.TypeOf(i) == nil {
			//		mutexMyAMIEvents.RLock()
			//		c, exists := myAMIEvents[pEV.Params["Actionid"]]
			//		mutexMyAMIEvents.RUnlock()
			//		if exists {
			//			c <- pEV
			//		} else {
			//			log.Printf("Event ActionID [%s] is NOT in myAMIEvents\n", pEV.Params["Actionid"])
			//
			//			switch pEV.ID {
			//			case "DataGet Tree":
			//				DataGet(pEV)
			//			}
			//		}
			//	} else {
			//		handleEvIface(i)
			//	}
			//} else {
			//////////////////////////////////////////////////////////////////
			// Non-legacy AMI processing - check for ActionID events FIRST
			AMIEventsMutex.RLock()
			if actionID, hasActionID := pEV.Params["Actionid"]; hasActionID {
				if c, exists := AMIEvents[actionID]; exists {
					AMIEventsMutex.RUnlock()
					c <- pEV
					continue // Skip normal event processing for ActionID events
				}
			}
			AMIEventsMutex.RUnlock()

			// Then do normal event processing for non-ActionID events
			if i := event.New(pEV); i != nil && reflect.TypeOf(i) != nil {
				handleEvIface(i)
			}
			//////////////////////////////////////////////////////////////////
			//}
		}
	}
}

func handleEvIface(i interface{}) {
	logger.Debugf("[%s]\n", reflect.TypeOf(i).String())
	switch reflect.TypeOf(i).String() {
	// Handled in parkedcalls.go
	case "event.ParkedCall":
		AddParkedCall(i.(event.ParkedCall))
	case "event.UnParkedCall", "event.ParkedCallGiveUp", "event.ParkedCallTimeOut":
		RemoveParkedCall(i.(event.ParkedCall))
	// Handled in hints.go
	case "event.ExtensionStatus":
		ExtensionStatus(i.(event.ExtensionStatus))
	// Handled in queues.go
	case "event.QueueParams":
		handleQueueParams(i.(event.QueueParams))
	case "event.QueueMember":
		handleQueueMember(i.(event.QueueMember))
	case "event.QueueEntry":
		handleQueueEntry(i.(event.QueueEntry))
	case "event.QueueJoin":
		handleQueueJoin(i.(event.QueueJoin))
	case "event.QueueLeave":
		handleQueueLeave(i.(event.QueueLeave))
	case "event.QueueMemberStatus":
		handleQueueMemberStatus(i.(event.QueueMemberStatus))
	case "event.QueueMemberAdded":
		handleQueueMemberAdded(i.(event.QueueMemberAdded))
	case "event.QueueMemberRemoved":
		handleQueueMemberRemoved(i.(event.QueueMemberRemoved))
	case "event.QueueAgentComplete":
		handleQueueAgentComplete(i.(event.QueueAgentComplete))
	case "event.QueueCallerAbandon":
		handleQueueCallerAbandon(i.(event.QueueCallerAbandon))
	case "event.QueueMemberPaused":
		handleQueueMemberPaused(i.(event.QueueMemberPaused))
	case "event.QueueMemberPenalty":
		handleQueueMemberPenalty(i.(event.QueueMemberPenalty))
	case "event.PeerStatus":
		handlePeerStatus(i.(event.PeerStatus))
	case "event.Newstate":
		NewState(i.(event.Newstate))
	case "event.Reload":
		reloadEvent := i.(event.SystemReload)
		logger.Infof("Got Reload event, Module: %s, Message: %s", reloadEvent.Module, reloadEvent.Message)
		if reloadEvent.Module == "Queues" {
			queuesReloaded <- 1
		}
	case "event.Newchannel":
		newChannelEvent := i.(event.Newchannel)
		chanNewChannels <- newChannelEvent.Channel
	case "event.Hangup":
		hangupEvent := i.(event.Hangup)
		chanHangups <- hangupEvent.Channel
	case "event.Rename":
		// Channel: %s\r\nNewname: %s\r\nUniqueid: %s
	case "event.Masquerade":
		//"Clone: %s\r\n"
		//"CloneState: %s\r\n"
		//"Original: %s\r\n"
		//"OriginalState: %s\r\n",
	case "event.FullyBooted":
		// Handle FullyBooted event
		logger.Debugf("[FullyBooted]\t [Status: %s]\t [Uptime: %s]\t [LastReload: %s]\n", i.(event.FullyBooted).Status, i.(event.FullyBooted).Uptime, i.(event.FullyBooted).Lastreload)
	default:
		logger.Warnf("UNHANDLED EventType:%s\n", reflect.TypeOf(i))
	}
}

func printEv(ev *gami.AMIEvent) {
	var b []byte
	var err error
	b, err = json.MarshalIndent(ev, "|", "  ")
	if err == nil {
		logger.Debugf(string(b))
	} else {
		logger.Errorf("Error printing event: %s\n", err)
	}
	return
}

func BuildSipDescriptionMap() {

	rows, err := DB.Query(`
		SELECT concat('SIP/', sippeers.name) as 'auth_userID', sippeers.fullname as 'display_name'
		FROM sippeers
		WHERE sippeers.fullname IS NOT NULL`)
	if err != nil {
		logger.Errorf("Error building SIP Description Map: %s", err)
		return
	}
	defer func() { _ = rows.Close() }()
	DeviceDescMapMutex.Lock()
	defer DeviceDescMapMutex.Unlock()
	for rows.Next() {
		var authUserID, dispName string
		if err = rows.Scan(&authUserID, &dispName); err != nil {
			logger.Errorf("Error scanning SIP Description Map row: %s", err)
			break
		}
		DeviceDescMap[authUserID] = dispName
		logger.Debugf("Added to DeviceDescMap: authUserID:%s dispName:%s\n", authUserID, dispName)
	}
}

func PrintDeviceDescMap() {
	DeviceDescMapMutex.RLock()
	defer DeviceDescMapMutex.RUnlock()
	for k, v := range DeviceDescMap {
		if len(k) < 15 {
			k = k + "\t"
		}
		logger.Debugf("\tauthUserID:\t %s \t\t dispName:\t %s\n", k, v)
	}
}

func extensionStatus2Str(status int) string {
	var m = map[int]string{
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
	}
	if s, ok := m[status]; ok {
		return s
	}
	return "Unknown"
}

func InitInuseChannelMap() error {
	ActionID := "Status_" + fmt.Sprintf("%d", rand.Intn(1000000))
	logger.Debugf("Initializing in-use channel map with ActionID %s\n", ActionID)
	AMIEventsMutex.Lock()
	AMIEvents[ActionID] = make(chan *gami.AMIEvent)
	AMIEventsMutex.Unlock()
	defer func() {
		AMIEventsMutex.Lock()
		close(AMIEvents[ActionID])
		delete(AMIEvents, ActionID)
		AMIEventsMutex.Unlock()
	}()
	if resp, err := pAMI.Action("Status", gami.Params{"ActionID": ActionID}); err != nil {
		return err
	} else if resp.Status == "Success" {
		logger.Debugf("Status Action response [%s] - [%s]\n", resp.ID, resp.Status)
		for {
			select {
			case pEV := <-AMIEvents[ActionID]:
				logger.Debugf("Got InitInuseChannelMap Event")
				if pEV.ID == "Status" {
					if channel, found := pEV.Params["Channel"]; found {
						if len(channel) > 14 && channel[0:14] == "SIP/trunksip1-" {
							mapInprocessChannels[channel] = true
						}
					}
				} else if pEV.ID == "StatusComplete" {
					logger.Debugf("[StatusComplete] Completed initializing in-use channel map, found %d channels\n", len(mapInprocessChannels))
					saveInfluxNumTrunkChannels(len(mapInprocessChannels), "PSTN")
					return nil
				}
			case <-time.After(15 * time.Second):
				return errors.New("timeout")
			}
		}
	} else {
		if m, exists := resp.Params["Message"]; exists {
			return errors.New(m)
		} else {
			return errors.New(resp.Status)
		}
	}
}

func panelGet(w http.ResponseWriter, r *http.Request) {
	// /{panelID}
	var (
		reqVars map[string]string
		CurUser UserEntry
		data    struct {
			PanelID     uuid.UUID
			PanelAccess int
			Layout      PanelLayout
			PBXID       string
			AstOrgID    string
			Exts        []EXTPAGEINFO
			Queues      []QUEUEPAGEINFO
			Vars        []VARPAGEINFO
		}
		ID          string
		err         error
		pOrg        *ORGINFO
		e           EXTPAGEINFO
		q           QUEUEPAGEINFO
		tNow        time.Time
		templatePtr *template.Template
	)
	session, _ := SessionStore.Get(r, SESSION_NAME)
	if err = json.Unmarshal(session.Values["CurUser"].([]byte), &CurUser); err != nil {
		logger.Errorf("Error retrieving session user data: %s\n", err)
		http.Error(w, "Invalid session", 403)
		return
	}
	reqVars = mux.Vars(r)
	ID = reqVars["panelID"]
	if data.PanelID, err = uuid.FromString(ID); err != nil {
		log.Printf("Error INVALID ID '%s', error:%s\n", ID, err)
		http.Error(w, "Invalid ID", 400)
		return
	}
	if data.PanelAccess = CheckUserAccessToPanelID(&CurUser, data.PanelID); data.PanelAccess == 0 {
		log.Printf("Error ACCESS DENIED for Panel '%s', error:%s\n", ID, err)
		http.Error(w, "Access denied", 401)
		return
	}
	err = LoadPanel(data.PanelID, &data.Layout, &data.PBXID, &data.AstOrgID)
	if err != nil {
		log.Printf("Error reading dashboard '%s', error:%s\n", ID, err)
		http.Error(w, "I/O Error", 404)
		return
	}

	logger.Debugf("--------------------------------\n")
	logger.Debugf("PanelID %s, PBXID %s, AstOrgID %s\n", data.PanelID.String(), data.PBXID, data.AstOrgID)
	logger.Debugf("Layout: %+v\n", data.Layout)
	logger.Debugf("User: %+v\n", CurUser)
	logger.Debugf("Panel Loaded, Access Checked, Loading Template\n")
	logger.Debugf("--------------------------------\n")

	if data.Layout.LayoutType < 0 || data.Layout.LayoutType > 5 {
		data.Layout.LayoutType = 0
	}

	if templatePtr, err = getTemplate(fmt.Sprintf("panel%d.tmpl", data.Layout.LayoutType), "panelgeneral.js"); err != nil {
		log.Printf("Error Template retrieval error:%s\n", err)
		http.Error(w, "Template retrieval", 404)
		return
	} else {
		data.Vars = make([]VARPAGEINFO, 0, 4)
		for _, v := range getOrgVars(data.AstOrgID) {
			for _, value := range data.Layout.Variables {
				if strings.EqualFold(value, v.Name) {
					data.Vars = append(data.Vars, v)
					break
				}
			}
		}

		data.Exts = make([]EXTPAGEINFO, 0, 32)
		data.Queues = make([]QUEUEPAGEINFO, 0, 4)
		if pOrg = GetOrgInfoPtr(data.AstOrgID); pOrg == nil {
			http.Error(w, "Bad Asterisk Org ID", 404)
			return
		}
		tNow = time.Now()
		for _, value := range data.Layout.Extens {
			for ext, extinfo := range pOrg.Extensions {
				if strings.EqualFold(value.Exten, ext) {
					e.Exten = ext
					e.ShowCID = value.ShowCID
					e.Status = extinfo.Status
					e.StatusStr = extensionStatus2Str(extinfo.Status)
					e.Label = extinfo.Label
					e.Type = extinfo.Type
					e.SecsInStatus = int(tNow.Sub(extinfo.LastStatusChange) / time.Second)
					e.LastStatusChange = extinfo.LastStatusChange.Unix() * 1000 // convert to milliseconds
					data.Exts = append(data.Exts, e)
					break
				}
			}
		}
		for _, value := range data.Layout.Queues {
			for queuename, pQI := range pOrg.Queues {
				if strings.EqualFold(value.QueueName, queuename) {
					q.Name = queuename
					q.QI = *pQI
					q.ShowAgents = value.ShowAgents
					q.ShowCalls = value.ShowCalls
					q.CallerPositionMap = make([]*QUEUEENTRY, len(q.QI.Entries))
					for _, pQE := range q.QI.Entries {
						pQE.SecsInQueue = int(tNow.Sub(pQE.WhenEntered) / time.Second)
						q.CallerPositionMap[pQE.Position-1] = pQE
					}
					data.Queues = append(data.Queues, q)
					break
				}
			}
		}
		pOrg.Unlock()
		// log.Println(data)
		noCacheHeaders(w)
		if err = templatePtr.Execute(w, data); err != nil {
			log.Println(err)
			http.Error(w, "Template execution error", 404)
			return
		}
	}
}

// Returns: 0 = no access, 1 = read-only, 2 = read/write
func CheckUserAccessToPanelID(pCurUser *UserEntry, PanelID uuid.UUID) int {
	var (
		err       error
		x         uint64
		WriteFlag int
		OrgID     uint64
	)
	if (pCurUser.Flags & USERFLAG_SUPERUSER) > 0 {
		return 2
	}
	if (pCurUser.Flags & USERFLAG_STAFFADMIN) > 0 {
		err = DB.QueryRow(`SELECT Account.OrgID FROM Account
           INNER JOIN Org ON Account.OrgID=Org.ID
           WHERE Account.DashboardID=? AND (Org.MgmtGroups & ?) > 0`,
			PanelID.Bytes(), pCurUser.MgmtGroups).Scan(&OrgID)
		if err == nil {
			return 2
		}
	}
	err = DB.QueryRow(`SELECT count(*),WriteFlag FROM Account
               INNER JOIN UserPerm ON UserPerm.AccountID=Account.ID
               WHERE Account.DashboardID=? AND UserPerm.UserID=?`,
		PanelID.Bytes(), pCurUser.ID).Scan(&x, &WriteFlag)
	// logger.Printf("CheckAccess for UserID %d, returned x=%d, wf=%d\n", pCurUser.ID, x, WriteFlag)
	if err == nil && x > 0 {
		if WriteFlag == 1 {
			return 2
		} else {
			return 1
		}
	} else {
		// User doesn't have direct access to Dash through UserPerm, so check other methods
		// Is user an Admin for the Org to which the Dashboard belongs?
		err = DB.QueryRow(`SELECT Account.OrgID FROM Account WHERE Account.DashboardID=?`, PanelID.Bytes()).Scan(&OrgID)
		if err == nil {
			if OrgID == pCurUser.OrgID && ((pCurUser.Flags & USERFLAG_ADMIN) > 0) {
				return 2
			}
		}
		if (pCurUser.Flags & USERFLAG_STAFFUSER) > 0 {
			err = DB.QueryRow(`SELECT Account.OrgID FROM Account
	           INNER JOIN Org ON Account.OrgID=Org.ID
	           WHERE Account.DashboardID=? AND (Org.MgmtGroups & ?) > 0`,
				PanelID.Bytes(), pCurUser.MgmtGroups).Scan(&OrgID)
			if err == nil {
				return 1
			}
		}
	}
	return 0
}

func LoadPanel(PanelID uuid.UUID, pLayout *PanelLayout, pPBXID *string, pAstOrgID *string) error {
	var (
		err      error
		jsondata string
	)
	err = DB.QueryRow(`
		SELECT Account.PBXID,Org.AstOrgID,ifnull(JSONData,'') AS JSONData
		FROM Dashboard
		INNER JOIN Account ON Account.DashboardID=Dashboard.ID
		INNER JOIN Org ON Account.OrgID=Org.ID
		WHERE Dashboard.ID=? AND (Account.Flags & 0x00000020)=0`,
		PanelID.Bytes()).Scan(pPBXID, pAstOrgID, &jsondata)
	if err == nil {
		if err = json.Unmarshal([]byte(jsondata), pLayout); err != nil {
			return err
		}
		if len(pLayout.ExtensOld) > 0 {
			// Convert old format to new format
			pLayout.Extens = make([]PanelLayoutExten, len(pLayout.ExtensOld))
			for i := range pLayout.ExtensOld {
				pLayout.Extens[i].Exten = pLayout.ExtensOld[i]
			}
			pLayout.ExtensOld = make([]string, 0)
			if json, err := json.Marshal(*pLayout); err == nil {
				SavePanel(json, PanelID)
				log.Printf("Updated panel ID %s to new format", PanelID.String())
			}
		}
	} else {
		return err
	}
	return nil
}

func SavePanel(json []byte, PanelID uuid.UUID) error {
	_, err := DB.Exec(`update Dashboard set JSONData=? where ID=?`, json, PanelID.Bytes())
	if err != nil {
		log.Printf("Error updating Dashboard record %s, error:%s\n", PanelID.String(), err)
		return err
	}
	return nil
}

func getOrgVars(AstOrgID string) []VARPAGEINFO {
	var ()
	a := make([]VARPAGEINFO, 0, 4)
	rows, err := DBAstConfig.Query(`SELECT vartype,varname,label,varoptions,varvalues FROM AstConfig.Variables
		WHERE AstConfig.Variables.orgID=? ORDER BY label`, AstOrgID)
	if err != nil {
		log.Println(err)
		return a
	}
	defer rows.Close()
	for rows.Next() {
		var (
			nsVarOptions sql.NullString
			nsVarValues  sql.NullString
			v            VARPAGEINFO
		)
		if err := rows.Scan(&v.Type, &v.Name, &v.Label, &nsVarOptions, &nsVarValues); err != nil {
			log.Println(err)
			break
		}
		if nsVarValues.Valid {
			v.Values = strings.Split(nsVarValues.String, ";")
		}
		if nsVarOptions.Valid {
			v.Options = strings.Split(nsVarOptions.String, ";")
		} else {
			v.Options = v.Values
		}
		a = append(a, v)
	}
	return a
}

// Search for OrgInfo in our Org map, creating one if not found
// Upon return, the newly created or found ORGINFO is locked
// and the caller must call pOrg.Unlock()
func GetOrgInfoPtr(orgID string) *ORGINFO {
	var (
		pOrg *ORGINFO
		ok   bool
	)
	orgMapRWMutex.RLock()
	if pOrg, ok = orgMap[orgID]; !ok {
		pOrg = new(ORGINFO)
		pOrg.OrgID = orgID
		pOrg.Extensions = make(map[string]*EXTENSIONINFO)
		pOrg.ParkingLots = make(map[string]event.ParkedCall)
		pOrg.Queues = make(map[string]*QUEUEINFO)
		orgMapRWMutex.RUnlock()
		orgMapRWMutex.Lock()
		orgMap[orgID] = pOrg
		logger.Debugf("Created new ORGINFO for OrgID %s\n", orgID)
		printOrgInfo(pOrg)
		orgMapRWMutex.Unlock()
	} else {
		logger.Debugf("Found existing ORGINFO for OrgID %s\n", orgID)
		printOrgInfo(pOrg)
		orgMapRWMutex.RUnlock()
	}
	pOrg.Lock()
	return pOrg
}

func printOrgInfo(org *ORGINFO) {
	logger.Debugf("----- ORGINFO for OrgID %s -----\n", org.OrgID)
	logger.Debugf("Extensions:\n")
	for ext, extinfo := range org.Extensions {
		logger.Debugf("\tExten: %s, Label: %s, Type: %d, Status: %d, LastStatusChange: %s\n",
			ext, extinfo.Label, extinfo.Type, extinfo.Status, extinfo.LastStatusChange.String())
	}
	logger.Debugf("ParkingLots:\n")
	for lotname, pcall := range org.ParkingLots {
		logger.Debugf("\tLotName: %s, Channel: %s, CallerIDNum: %s, CallerIDName: %s, From: %s\n",
			lotname, pcall.Channel, pcall.CallerIDNum, pcall.CallerIDName, pcall.From)
	}
	logger.Debugf("Queues:\n")
	for qname, pQI := range org.Queues {
		logger.Debugf("\tQueueName: %s, NumMembers: %d, NumEntries: %d\n", qname, len(pQI.Members), len(pQI.Entries))
	}
	logger.Debugf("----- END ORGINFO for OrgID %s -----\n", org.OrgID)
}

// LAYOUTS

func VarGet(w http.ResponseWriter, r *http.Request) {
	// GET to /vars/get/{panelID}
	type varResult struct {
		Name  string `json:"name"`
		Value string `json:"val"`
	}
	var (
		err         error
		CurUser     UserEntry
		reqVars     map[string]string
		PanelID     uuid.UUID
		PanelAccess int
		Layout      PanelLayout
		PBXID       string
		AstOrgID    string
		results     []varResult
	)
	session, _ := SessionStore.Get(r, SESSION_NAME)
	if err = json.Unmarshal(session.Values["CurUser"].([]byte), &CurUser); err != nil {
		http.Error(w, "Invalid session", 403)
		return
	}
	reqVars = mux.Vars(r)
	if PanelID, err = uuid.FromString(reqVars["panelID"]); err != nil {
		http.Error(w, "Invalid ID", 400)
		return
	}
	if PanelAccess = CheckUserAccessToPanelID(&CurUser, PanelID); PanelAccess < 1 {
		http.Error(w, "Access denied", 401)
		return
	}
	err = LoadPanel(PanelID, &Layout, &PBXID, &AstOrgID)
	if err != nil {
		log.Printf("Error reading dashboard '%s', error:%s\n", reqVars["panelID"], err)
		http.Error(w, "I/O Error", 404)
		return
	}
	logger.Debugf("VarGet for PanelID %s, PBXID %s, AstOrgID %s, Layout %#v\n", PanelID.String(), PBXID, AstOrgID, Layout)
	results = make([]varResult, 0, len(Layout.Variables))
	for _, v := range Layout.Variables {
		if val, err := AMIDBGet(AstOrgID, v); err != nil {
			log.Println(err)
		} else {
			results = append(results, varResult{Name: v, Value: val})
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(results); err != nil {
		log.Println(err)
	}
}

func VarSet(w http.ResponseWriter, r *http.Request) {
	// POST to /vars/set/{panelID}
	// data is json: {name: <name>, val: <value>}
	type varData struct {
		Name  string `json:"name"`
		Value string `json:"val"`
	}
	var (
		err         error
		CurUser     UserEntry
		reqVars     map[string]string
		PanelID     uuid.UUID
		PanelAccess int
		Layout      PanelLayout
		PBXID       string
		AstOrgID    string
		data        varData
	)
	session, _ := SessionStore.Get(r, SESSION_NAME)
	if err = json.Unmarshal(session.Values["CurUser"].([]byte), &CurUser); err != nil {
		http.Error(w, "Invalid session", 403)
		return
	}
	reqVars = mux.Vars(r)
	if PanelID, err = uuid.FromString(reqVars["panelID"]); err != nil {
		http.Error(w, "Invalid ID", 400)
		return
	}
	if PanelAccess = CheckUserAccessToPanelID(&CurUser, PanelID); PanelAccess != 2 {
		http.Error(w, "Access denied", 401)
		return
	}
	err = LoadPanel(PanelID, &Layout, &PBXID, &AstOrgID)
	if err != nil {
		log.Printf("Error reading dashboard '%s', error:%s\n", reqVars["panelID"], err)
		http.Error(w, "I/O Error", 404)
		return
	}
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Missing JSON data", 422)
		return
	}
	err = json.Unmarshal(body, &data)
	if err != nil {
		http.Error(w, "Invalid JSON data", 422)
		return
	}
	for _, name := range Layout.Variables {
		if name == data.Name {
			// Name supplied matches one in the panel config so the user must be allowed to change it
			AMIDBPut(AstOrgID, name, data.Value)
			http.Error(w, "{}", 200)
			return
		}
	}
	http.Error(w, "Invalid variable", 422)
}

func AMIDBGet(family string, key string) (string, error) {
	var value string
	ActionID := "DBGet_" + fmt.Sprintf("%d", rand.Intn(1000000))

	AMIEventsMutex.Lock()
	AMIEvents[ActionID] = make(chan *gami.AMIEvent)
	AMIEventsMutex.Unlock()
	defer func() {
		AMIEventsMutex.Lock()
		close(AMIEvents[ActionID])
		delete(AMIEvents, ActionID)
		AMIEventsMutex.Unlock()
	}()
	if resp, err := pAMI.Action("DBGet", gami.Params{"ActionID": ActionID, "Family": family, "Key": key}); err != nil {
		return value, err
	} else if resp.Status == "Success" {
		for {
			select {
			case pEV := <-AMIEvents[ActionID]:
				logger.Debugf("[Processing myAMIEvent] %#v\n", pEV)
				if pEV.ID == "DBGetResponse" {
					logger.Debugf("[Processing DBGetResponse]\n")
					value = pEV.Params["Val"]
				} else if pEV.ID == "DBGetComplete" {
					logger.Debugf("[Processing DBGetComplete]\n")
					return value, nil
				}
			case <-time.After(5 * time.Second):
				return value, errors.New("timeout")
			}
		}
	} else {
		if m, exists := resp.Params["Message"]; exists {
			return value, errors.New(m)
		} else {
			return value, errors.New(resp.Status)
		}
	}
}

func AMIDBPut(family string, key string, val string) error {
	if resp, err := pAMI.Action("DBPut", gami.Params{"Family": family, "Key": key, "Val": val}); err != nil {
		log.Printf("Error executing DBPut(%s,%s,%s):%s", family, key, val, err)
		return err
	} else if resp.Status != "Success" {
		if m, exists := resp.Params["Message"]; exists {
			return errors.New(m)
		} else {
			return errors.New(resp.Status)
		}
	}
	return nil
}

func saveLayout(w http.ResponseWriter, r *http.Request) {
	// PUT or POST to /layout/{panelID}
	// body should be PanalLayout in JSON format
	var (
		reqVars     map[string]string
		ID          string
		PanelID     uuid.UUID
		err         error
		Layout      PanelLayout
		PanelAccess int
		CurUser     UserEntry
	)
	session, _ := SessionStore.Get(r, SESSION_NAME)
	if err := json.Unmarshal(session.Values["CurUser"].([]byte), &CurUser); err != nil {
		http.Error(w, "Invalid session", 403)
		return
	}
	reqVars = mux.Vars(r)
	ID = reqVars["panelID"]
	if PanelID, err = uuid.FromString(ID); err != nil {
		http.Error(w, "Invalid ID", 400)
		return
	}
	if PanelAccess = CheckUserAccessToPanelID(&CurUser, PanelID); PanelAccess != 2 {
		http.Error(w, "Access denied", 401)
		return
	}
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Missing JSON data", 422)
		return
	}
	// We just unmarshal it here in order to validate it
	err = json.Unmarshal(body, &Layout)
	if err != nil {
		http.Error(w, "Invalid JSON data", 422)
		return
	}
	if err = SavePanel(body, PanelID); err != nil {
		http.Error(w, "I/O Error", 404)
		return
	}
	http.Error(w, "Saved", 200)
	sendLayoutChange(PanelID)
}

func editLayout(w http.ResponseWriter, r *http.Request) {
	// /edit/{panelID}
	var (
		CurUser UserEntry
		reqVars map[string]string
		data    struct {
			PanelID     uuid.UUID
			PanelAccess int
			Layout      PanelLayout
			PBXID       string
			AstOrgID    string
			AvailExts   []EXTPAGEINFO
			UsedExts    []EXTPAGEINFO
			AvailQueues []QUEUEPAGEINFO
			UsedQueues  []QUEUEPAGEINFO
			AvailVars   []VARPAGEINFO
			UsedVars    []VARPAGEINFO
		}
		ID   string
		err  error
		pOrg *ORGINFO
		e    EXTPAGEINFO
		q    QUEUEPAGEINFO
	)
	session, _ := SessionStore.Get(r, SESSION_NAME)
	if err := json.Unmarshal(session.Values["CurUser"].([]byte), &CurUser); err != nil {
		http.Error(w, "Invalid session", 403)
		return
	}
	reqVars = mux.Vars(r)
	ID = reqVars["panelID"]
	if data.PanelID, err = uuid.FromString(ID); err != nil {
		http.Error(w, "Invalid ID", 400)
		return
	}
	if data.PanelAccess = CheckUserAccessToPanelID(&CurUser, data.PanelID); data.PanelAccess != 2 {
		http.Error(w, "Access denied", 401)
		return
	}
	err = LoadPanel(data.PanelID, &data.Layout, &data.PBXID, &data.AstOrgID)
	if err != nil {
		log.Printf("Error reading dashboard '%s', error:%s\n", ID, err)
		http.Error(w, "I/O Error", 404)
		return
	}
	if templatePtr, err := getTemplate("editLayout.tmpl"); err != nil {
		log.Println(err)
		http.Error(w, "Template retrieval", 404)
		return
	} else {
		allvars := getOrgVars(data.AstOrgID) // This does a DB operation, so do it before we lock the Org in memory
		if pOrg = GetOrgInfoPtr(data.AstOrgID); pOrg == nil {
			http.Error(w, "Bad Asterisk Org ID", 404)
			return
		}
		data.AvailExts = make([]EXTPAGEINFO, 0, 64)
		data.UsedExts = make([]EXTPAGEINFO, 0, 64)
		data.AvailQueues = make([]QUEUEPAGEINFO, 0, 8)
		data.UsedQueues = make([]QUEUEPAGEINFO, 0, 8)
		data.AvailVars = make([]VARPAGEINFO, 0, len(allvars))
		data.UsedVars = make([]VARPAGEINFO, 0, len(allvars))
		for _, v := range allvars {
			used := false
			for _, value := range data.Layout.Variables {
				if strings.EqualFold(value, v.Name) {
					data.UsedVars = append(data.UsedVars, v)
					used = true
					break
				}
			}
			if !used {
				data.AvailVars = append(data.AvailVars, v)
			}
		}
		for _, value := range data.Layout.Extens {
			for ext, extinfo := range pOrg.Extensions {
				if strings.EqualFold(value.Exten, ext) {
					e.Exten = ext
					e.ShowCID = value.ShowCID
					e.Label = extinfo.Label
					e.Type = extinfo.Type
					data.UsedExts = append(data.UsedExts, e)
					break
				}
			}
		}
		for ext, extinfo := range pOrg.Extensions {
			if len(extinfo.Label) > 0 && !data.Layout.extensionUsed(ext) {
				e.Exten = ext
				e.ShowCID = 0
				e.Label = extinfo.Label
				e.Type = extinfo.Type
				data.AvailExts = append(data.AvailExts, e)
			}
		}
		for _, value := range data.Layout.Queues {
			for queuename, pQI := range pOrg.Queues {
				if strings.EqualFold(value.QueueName, queuename) {
					q.Name = queuename
					q.QI = *pQI
					q.ShowAgents = value.ShowAgents
					q.ShowCalls = value.ShowCalls
					data.UsedQueues = append(data.UsedQueues, q)
					break
				}
			}
		}
		for queuename, pQI := range pOrg.Queues {
			if !data.Layout.queueUsed(queuename) {
				q.Name = queuename
				q.QI = *pQI
				data.AvailQueues = append(data.AvailQueues, q)
			}
		}
		pOrg.Unlock()
		sort.Sort(EXTPAGEINFOByExten(data.AvailExts))
		noCacheHeaders(w)
		if err := templatePtr.Execute(w, data); err != nil {
			log.Println(err)
			http.Error(w, "Template execution error", 404)
			return
		}
	}
}

func (pLayout *PanelLayout) extensionUsed(ext string) bool {
	for _, value := range pLayout.Extens {
		if strings.EqualFold(value.Exten, ext) {
			return true
		}
	}
	return false
}

func (pLayout *PanelLayout) queueUsed(queuename string) bool {
	for _, value := range pLayout.Queues {
		if strings.EqualFold(value.QueueName, queuename) {
			return true
		}
	}
	return false
}
