package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Simply-Bits/go.uuid"
	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 4096
)

type WSClient struct {
	sync.Mutex
	ws           *websocket.Conn
	send         chan []byte // Channel storing outcoming messages
	RemoteAddr   string
	OrgIDWatched map[string]bool
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  maxMessageSize,
	WriteBufferSize: maxMessageSize,
}

type hub struct {
	clients    map[*WSClient]bool // Registered clients
	broadcast  chan string        // Messages to be broadcast to all clients
	orgChanged chan []byte        // Change notifications for an OrgID
	register   chan *WSClient     // Register requests
	unregister chan *WSClient     // Unregister requests
}

type ReloadNotify1 struct {
	Event string `json:"event"`
}

type HintChangeNotify1 struct {
	Event            string `json:"event"`
	OrgID            string `json:"orgid"`
	Exten            string `json:"exten"`
	Status           int    `json:"status"`
	StatusStr        string `json:"statusstr"`
	LastStatusChange int64  `json:"laststatuschange"`
	CallerIDNum      string `json:"calleridnum"`
	CallerIDName     string `json:"calleridname"`
}

type QCNCallerEntry struct {
	Uniqueid     string    `json:"uniqueid"`
	Position     int       `json:"position"`
	CallerIDNum  string    `json:"calleridnum"`
	CallerIDName string    `json:"calleridname"`
	WhenEntered  time.Time `json:"whenentered"`
}

type QueueChangeNotify1 struct {
	// Notification for queue stats change
	Event            string           `json:"event"`
	OrgID            string           `json:"orgid"`
	QueueName        string           `json:"queuename"`
	Calls            int              `json:"calls"`
	HoldTime         int              `json:"holdtime"` // Current exponential average hold time
	TalkTime         int              `json:"talktime"` // Current exponential average talktime
	Completed        int              `json:"completed"`
	Abandoned        int              `json:"abandoned"`
	ServicelevelPerf float64          `json:"servicelevelperf"`
	WhenStatsCleared time.Time        `json:"whenstatscleared"`
	Entries          []QCNCallerEntry `json:"callers"`
}

type QueueChangeNotify2 struct {
	// Notification for queue member/agent change or add
	Event      string    `json:"event"`
	OrgID      string    `json:"orgid"`
	QueueName  string    `json:"queuename"`
	Name       string    `json:"name"`
	Location   string    `json:"location"`
	Penalty    int       `json:"penalty"`
	CallsTaken int       `json:"callstaken"`
	DynamicAge int       `json:"dynamicage"`
	LastCall   time.Time `json:"lastcall"`
	WhenAdded  time.Time `json:"whenadded"`
	Status     int       `json:"status"`
	Paused     int       `json:"paused"`
}

type QueueChangeNotify3 struct {
	// Notification for queue member/agent remove
	Event     string `json:"event"`
	OrgID     string `json:"orgid"`
	QueueName string `json:"queuename"`
	Name      string `json:"name"`
	Location  string `json:"location"`
}

var asyncNotifyHub = hub{
	broadcast:  make(chan string, 16),
	orgChanged: make(chan []byte, 16),
	register:   make(chan *WSClient),
	unregister: make(chan *WSClient),
	clients:    make(map[*WSClient]bool),
}

/****************************************************************************************/
func sendEventHintChange(orgID string, pExt *EXTENSIONINFO) {
	var hcn HintChangeNotify1
	hcn.Event = "HINTCHANGE"
	hcn.OrgID = orgID
	hcn.Exten = pExt.Exten
	hcn.Status = pExt.Status
	hcn.StatusStr = extensionStatus2Str(pExt.Status)
	hcn.LastStatusChange = pExt.LastStatusChange.Unix() * 1000 // convert to milliseconds
	hcn.CallerIDNum = pExt.ConnectedLineNum
	hcn.CallerIDName = pExt.ConnectedLineName
	jsondata, err := json.Marshal(hcn)
	if err != nil {
		logger.Errorf("sendEventHintChange Marshal error: %s", err)
		return
	}
	asyncNotifyHub.orgChanged <- jsondata
}

func sendReload() {
	asyncNotifyHub.broadcast <- `{"event":"RELOAD"}`
}

func sendLayoutChange(panelID uuid.UUID) {
	asyncNotifyHub.broadcast <- fmt.Sprintf(`{"event":"LAYOUTCHANGE", "panelid":"%s"}`, panelID.String())
}

// When queue stats have changed
func sendQueueChange(orgID string, queuename string, pQI *QUEUEINFO) {
	var qcn QueueChangeNotify1
	qcn.Event = "QUEUECHANGE"
	qcn.OrgID = orgID
	qcn.QueueName = queuename
	qcn.Calls = pQI.Calls
	qcn.HoldTime = pQI.HoldTime
	qcn.TalkTime = pQI.TalkTime
	qcn.Completed = pQI.Completed
	qcn.Abandoned = pQI.Abandoned
	qcn.ServicelevelPerf = pQI.ServicelevelPerf
	qcn.WhenStatsCleared = pQI.WhenStatsCleared
	jsondata, err := json.Marshal(qcn)
	if err != nil {
		logger.Errorf("sendQueueChange Marshal error: %s", err)
		return
	}
	asyncNotifyHub.orgChanged <- jsondata
}

func sendQueueCallersChange(orgID string, queuename string, pQI *QUEUEINFO) {
	var qcn QueueChangeNotify1
	qcn.Event = "QUEUECALLERSCHANGE"
	qcn.OrgID = orgID
	qcn.QueueName = queuename
	qcn.Calls = pQI.Calls
	qcn.HoldTime = pQI.HoldTime
	qcn.TalkTime = pQI.TalkTime
	qcn.Completed = pQI.Completed
	qcn.Abandoned = pQI.Abandoned
	qcn.ServicelevelPerf = pQI.ServicelevelPerf
	qcn.WhenStatsCleared = pQI.WhenStatsCleared
	qcn.Entries = make([]QCNCallerEntry, len(pQI.Entries))
	for _, pQE := range pQI.Entries {
		qcn.Entries[pQE.Position-1].Uniqueid = pQE.Uniqueid
		qcn.Entries[pQE.Position-1].Position = pQE.Position
		qcn.Entries[pQE.Position-1].CallerIDNum = pQE.CallerIDNum
		qcn.Entries[pQE.Position-1].CallerIDName = pQE.CallerIDName
		qcn.Entries[pQE.Position-1].WhenEntered = pQE.WhenEntered
	}
	jsondata, err := json.Marshal(qcn)
	if err != nil {
		logger.Errorf("sendQueueCallersChange Marshal error: %s", err)
		return
	}
	asyncNotifyHub.orgChanged <- jsondata
}

func sendQMemberChange(orgID string, queuename string, event string, pQM *QUEUEMEMBER) {
	var qcn QueueChangeNotify2
	qcn.Event = event
	qcn.OrgID = orgID
	qcn.QueueName = queuename
	qcn.Name = pQM.Name
	qcn.Location = pQM.Location
	qcn.Penalty = pQM.Penalty
	qcn.CallsTaken = pQM.CallsTaken
	qcn.LastCall = pQM.LastCall
	qcn.WhenAdded = pQM.WhenAdded
	qcn.DynamicAge = pQM.DynamicAge
	qcn.Status = pQM.Status
	qcn.Paused = pQM.Paused
	jsondata, err := json.Marshal(qcn)
	if err != nil {
		logger.Errorf("sendQMemberChange Marshal error: %s", err)
		return
	}
	asyncNotifyHub.orgChanged <- jsondata
}

func sendQMemberRemove(orgID string, queuename string, location string, memberName string) {
	var qcn QueueChangeNotify3
	qcn.Event = "AGENTREMOVE"
	qcn.OrgID = orgID
	qcn.QueueName = queuename
	qcn.Name = memberName
	qcn.Location = location
	jsondata, err := json.Marshal(qcn)
	if err != nil {
		logger.Errorf("sendQMemberRemove Marshal error: %s", err)
		return
	}
	asyncNotifyHub.orgChanged <- jsondata
}

/* INTERNALS ***************************************************************************************/
func (h *hub) run() {
	for {
		select {
		case c := <-h.register: // New client has connected to websocket
			h.clients[c] = true
			break

		case c := <-h.unregister: // Client has disconnected or gone away
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			break

		case jsondata := <-h.orgChanged:
			h.broadcastOrgChange(jsondata)
			break

		case msg := <-h.broadcast:
			h.broadcastMessage(msg)
			break
		}
	}
}

/*
	func (h *hub) broadcastHintChange(hcn HintChangeNotify1) {
		jsondata, err := json.Marshal(hcn)
		if err != nil {
			logger.Errorf( "broadcastHintChange Marshal error: %s", err)
			return
		}
		for c := range h.clients {
			c.Lock()
			if _, ok := c.OrgIDWatched[hcn.OrgID]; ok {
				logger.Debugf("Sending HINTCHANGE to client\n")
				c.Unlock()
				// This OrgID is being watched by this client
				select {
				case c.send <- jsondata:
					break

				// We can't reach the client
				default:
					close(c.send)
					delete(h.clients, c)
				}
			} else {
				c.Unlock()
			}
		}
	}
*/
func (h *hub) broadcastOrgChange(jsondata []byte) {
	var (
		notifyHdr struct { // we just need to unmarshal the first two fields
			Event string
			OrgID string
		}
	)

	err := json.Unmarshal(jsondata, &notifyHdr)
	if err != nil {
		logger.Errorf("broadcastOrgChange Unmarshal error: %s", err)
		return
	}
	logger.Debugf("ORGCHANGE: %+v", notifyHdr)
	for c := range h.clients {
		c.Lock()
		if _, ok := c.OrgIDWatched[notifyHdr.OrgID]; ok {
			// log.Printf("Sending CN (%s) to client\n", notifyHdr.Event)
			c.Unlock()
			// This OrgID is being watched by this client
			select {
			case c.send <- jsondata:
				break

			// We can't reach the client
			default:
				close(c.send)
				delete(h.clients, c)
			}
		} else {
			c.Unlock()
		}
	}
}

func (h *hub) broadcastMessage(msg string) {
	for c := range h.clients {
		select {
		case c.send <- []byte(msg):
			break

		// We can't reach the client
		default:
			close(c.send)
			delete(h.clients, c)
		}
	}
}

func serveWS(w http.ResponseWriter, r *http.Request) {
	// wss://<domain>/ws
	var (
		err error
		ws  *websocket.Conn
	)
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	ws, err = upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Errorf("Websocket upgrade error for client at %s:%s\n", r.RemoteAddr, err)
		return
	}
	c := &WSClient{
		RemoteAddr: r.RemoteAddr,
		send:       make(chan []byte, maxMessageSize),
		ws:         ws,
	}
	c.OrgIDWatched = make(map[string]bool)
	asyncNotifyHub.register <- c
	go c.writePump()
	c.readPump()
}

func CheckAccessToOrgID(OrgID string) bool {
	return true
}

func (c *WSClient) readPump() {
	var (
		msg struct {
			Cmd   string `json:"cmd"`
			OrgID string `json:"orgid"`
		}
	)
	defer func() {
		asyncNotifyHub.unregister <- c
		c.ws.Close()
	}()
	c.ws.SetReadLimit(maxMessageSize)
	err := c.ws.SetReadDeadline(time.Now().Add(pongWait))
	if err != nil {
		logger.Warnf("Websocket SetReadDeadline error for client at %s:%s\n", c.RemoteAddr, err)
	}
	c.ws.SetPongHandler(func(string) error {
		err = c.ws.SetReadDeadline(time.Now().Add(pongWait))
		if err != nil {
			logger.Warnf("Websocket SetReadDeadline error for client at %s:%s\n", c.RemoteAddr, err)
		}
		return nil
	})
	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {

			logger.Errorf("Websocket read error for client at %s:%s\n", c.RemoteAddr, err)

			// this stops the readPump loop for this WS client
			// the client should reconnect according to the WebSockets spec
			break
		}

		logger.Debugf("WS MSG:%s\n", message)
		if err = json.Unmarshal([]byte(message), &msg); err == nil {
			// We should be connected by a TCP/TLS session, so it should be fairly safe to assume
			// this is the user we previously authenticated.
			switch msg.Cmd {
			case "WATCHORG":
				if CheckAccessToOrgID(msg.OrgID) {
					c.Lock()
					c.OrgIDWatched[msg.OrgID] = true
					c.Unlock()
					c.send <- []byte(fmt.Sprintf(`{"event":"WATCHORG", "result":"OK", "serverinstance":"%s"}`, ServerInstance.String()))
					// c.send <- []byte(`{"Event":"WATCHORG", "Result":"OK"}`)
				} else {
					c.send <- []byte(`{"event":"WATCHORG", "result":"ACCESS DENIED"}`)
				}
			case "UNWATCHORG":
				c.Lock()
				delete(c.OrgIDWatched, msg.OrgID)
				c.Unlock()
				c.send <- []byte(`{"event":"UNWATCHORG", "result":"OK"}`)
			}
		}
		msg.Cmd = ""
		// if we wanted to broadcast to others: asyncNotifyHub.broadcast <- string(message)
	}
}

func (c *WSClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.ws.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			if !ok {
				_ = c.write(websocket.CloseMessage, []byte{})
				return
			}
			// Note that we're always using text messages as browser support for binary messages is lacking
			if err := c.write(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

func (c *WSClient) write(mt int, message []byte) error {
	err := c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	if err != nil {
		logger.Warnf("Websocket SetWriteDeadline error for client at %s:%s\n", c.RemoteAddr, err)
	}
	return c.ws.WriteMessage(mt, message)
}
