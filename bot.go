package main

import (
	"strings"
	"sync"
	"time"
)

type bot struct {
	sync.Mutex
	ID              string
	pass            string
	nick            string
	read            chan string
	toClient        chan string
	join            chan string
	channels        map[string][]*connection
	readconns       []*connection
	sendconns       []*connection
	whisperconn     *connection
	ticker          *time.Ticker
	clientConnected bool
	client          *Client
}

func newBot(client *Client) *bot {
	return &bot{
		read:      make(chan string, 10),
		join:      make(chan string, 50000),
		channels:  make(map[string][]*connection),
		readconns: make([]*connection, 0),
		sendconns: make([]*connection, 0),
		ticker:    time.NewTicker(1 * time.Minute),
		client:    client,
	}
}

func (bot *bot) Init() {
	go bot.joinChannels()
	go bot.checkConnections()
	bot.newConn(connReadConn)
	// twitch changed something about whispers or there is some black magic going on,
	// but its only reading whispers once even with more connections
	bot.newConn(connWhisperConn)
}

// close all connections and delete bot
func (bot *bot) close() {
	bot.Lock()
	bot.ticker.Stop()
	close(bot.read)
	close(bot.join)
	for _, conn := range bot.readconns {
		conn.conntype = connDelete
		conn.close()
	}
	for _, conn := range bot.sendconns {
		conn.conntype = connDelete
		conn.close()
	}
	bot.whisperconn.conntype = connDelete
	bot.whisperconn.close()
	for k := range bot.channels {
		delete(bot.channels, k)
	}
	Log.Info("CLOSED BOT", bot.nick)
	bot.Unlock()
}

func (bot *bot) checkConnections() {
	for _ = range bot.ticker.C {
		for _, co := range bot.readconns {
			conn := co
			conn.active = false
			err := conn.send("PING")
			if err != nil {
				Log.Error(err.Error())
				conn.restore()
				conn.close()
			}
			go func() {
				time.Sleep(10 * time.Second)
				if !conn.active {
					Log.Info("read connection died, reconnecting...")
					conn.restore()
					conn.close()
				}
			}()
		}
		for _, co := range bot.sendconns {
			conn := co
			if time.Since(conn.lastUse) < time.Minute*10 {
				// close unused connections
				conn.active = false
				err := conn.send("PING")
				if err != nil {
					Log.Error(err.Error())
					conn.restore()
					conn.close()
				} else {
					go func() {
						time.Sleep(10 * time.Second)
						if !conn.active {
							Log.Info("send connection died, closing...")
							conn.restore()
							conn.close()
						}
					}()
				}
			} else {
				if len(bot.sendconns) > 2 {
					Log.Info("closing unused connection")
					conn.restore()
					conn.close()
				} else {
					conn.lastUse = time.Now()
				}
			}
		}
		go func() {
			err := bot.whisperconn.send("PING")
			if err != nil {
				Log.Error(err.Error())
				bot.whisperconn.restore()
			}
			time.Sleep(10 * time.Second)
			if !bot.whisperconn.active {
				bot.newConn(connWhisperConn)
			}
		}()
		for channel := range bot.channels {
			if conns, ok := bot.channels[channel]; ok && len(conns) < 1 {
				bot.join <- channel
			}
		}
	}
}

func (bot *bot) partChannel(channel string) {
	channel = strings.ToLower(channel)
	if conns, ok := bot.channels[channel]; ok {
		for _, conn := range conns {
			err := conn.send("PART " + channel)
			if err != nil {
				Log.Error(err.Error())
				conn.restore()
				conn.close()
				bot.partChannel(channel)
				return
			}
			conn.part(channel)
		}
		Log.Infof("left channel on %d connections\n", len(conns))
		delete(bot.channels, channel)
		return
	}
	Log.Error("never joined ", channel)
}

func (bot *bot) joinChannels() {
	for channel := range bot.join {
		Log.Debug(channel)
		bot.joinChannel(channel)
		<-joinTicker.C
	}
}

func (bot *bot) joinChannel(channel string) {
	channel = strings.ToLower(channel)
	if conns, ok := bot.channels[channel]; ok && len(conns) > 0 {
		// TODO: check msg ids and join channels more than one time
		Log.Info("already joined channel", channel)
		return
	}
	var conn *connection
	for _, c := range bot.readconns {
		if len(c.joins) < 50 {
			conn = c
			break
		}
	}
	if conn == nil {
		bot.newConn(connReadConn)
		bot.joinChannel(channel)
		return
	}
	for !conn.active {
		time.Sleep(100 * time.Millisecond)
	}
	err := conn.send("JOIN " + channel)
	if err != nil {
		Log.Error(err.Error())
		conn.restore()
		conn.close()
		bot.join <- channel
		return
	}
	if _, ok := bot.channels[channel]; !ok {
		bot.channels[channel] = make([]*connection, 0)
	}
	conn.joins = append(conn.joins, channel)
	bot.channels[channel] = append(bot.channels[channel], conn)
	Log.Info("joined channel", channel)
}

func (bot *bot) newConn(t connType) {
	whisperConns := 0
	var conn *connection
	switch t {
	case connReadConn:
		conn = newConnection(t)
		go conn.connect(bot.client, bot.pass, bot.nick)
		bot.readconns = append(bot.readconns, conn)
	case connSendConn:
		conn = newConnection(t)
		go conn.connect(bot.client, bot.pass, bot.nick)
		bot.sendconns = append(bot.sendconns, conn)
	case connWhisperConn:
		if bot.whisperconn != nil {
			bot.whisperconn.close()
		}
		conn = newConnection(t)
		go conn.connect(bot.client, bot.pass, bot.nick)
		bot.whisperconn = conn
	}
	if bot.whisperconn != nil {
		whisperConns = 1
	}
	Log.Infof("NEW %s, TOTAL: %d", conn.name, len(bot.readconns)+len(bot.sendconns)+whisperConns)
}

func (bot *bot) readChat() {
	for msg := range bot.toClient {
		bot.read <- msg
	}
}

func (bot *bot) say(msg string) {
	var conn *connection
	var min = 15
	// find connection with the least sent messages
	for _, c := range bot.sendconns {
		if c.msgCount < min {
			conn = c
			min = conn.msgCount
		}
	}
	if conn == nil || min > 10 {
		bot.newConn(connSendConn)
		Log.Infof("created new conn, total: %d\n", len(bot.sendconns))
		bot.say(msg)
		return
	}
	// add to msg counter before waiting to stop other go routines from sending on this connection
	conn.countMsg()
	for !conn.active {
		time.Sleep(100 * time.Millisecond)
	}
	conn.lastUse = time.Now()
	err := conn.send("PRIVMSG " + msg)
	if err != nil {
		Log.Error(err.Error())
		conn.restore()
		conn.close()
		bot.say(msg)
		return
	}
	Log.Debugf("%p   %d\n", conn, conn.msgCount)
	Log.Info("sent:", msg)
}

func (bot *bot) handleMessage(spl []string) {
	msg := spl[1]
	switch spl[0] {
	case "JOIN":
		bot.join <- strings.ToLower(msg)
	case "PART":
		bot.partChannel(strings.ToLower(msg))
	case "PRIVMSG":
		bot.say(msg)
	default:
		Log.Warning("unhandled message", spl[0], msg)
	}
}
