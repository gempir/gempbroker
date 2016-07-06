package main

import (
	"bufio"
	"fmt"
	"net"
	"net/textproto"
	"strings"
	"sync"
	"time"
)

type connType uint32

const (
	connWhisperConn = iota
	connReadConn
	connSendConn
)

type connection struct {
	sync.Mutex
	conn     net.Conn
	active   bool
	anon     bool
	joins    []string
	msgCount int
	alive    bool
	conntype connType
	client   *Client
}

func newConnection(t connType) *connection {
	c := &connection{
		joins:    make([]string, 0),
		conntype: t,
	}

	return c
}

func (conn *connection) login(pass string, nick string) {
	conn.anon = pass == ""
	if !conn.anon {
		conn.send("PASS " + pass)
		conn.send("NICK " + nick)
		return
	}
	conn.send("NICK justinfan123")
}

func (conn *connection) close() {
	conn.conn.Close()
	conn.alive = false
}

func (conn *connection) restore() {
	if conn.conntype == connReadConn {
		var i int
		var channels []string
		for index, co := range conn.client.bot.sendconns {
			if conn == co {
				i = index
				channels = co.joins
				break
			}
		}
		conn.client.bot.readconns = append(conn.client.bot.readconns[:i], conn.client.bot.readconns[i+1:]...)
		for _, ch := range channels {
			conn.client.bot.join <- ch
		}
	} else {
		var i int
		for index, co := range conn.client.bot.sendconns {
			if conn == co {
				i = index
				break
			}
		}
		conn.client.bot.sendconns = append(conn.client.bot.sendconns[:i], conn.client.bot.sendconns[i+1:]...)
	}
}

func (conn *connection) connect(client *Client, pass string, nick string) {
	c, err := net.Dial("tcp", *addr)
	if err != nil {
		Log.Error("unable to connect to irc server", err)
		conn.restore()
	}

	conn.conn = c
	conn.client = client

	conn.login(pass, nick)
	conn.send("CAP REQ :twitch.tv/tags")
	conn.send("CAP REQ :twitch.tv/commands")

	defer conn.close()
	reader := bufio.NewReader(conn.conn)
	tp := textproto.NewReader(reader)
	for {
		line, err := tp.ReadLine()
		if err != nil {
			Log.Debug("read:", err)
			conn.restore()
			return
		}

		if strings.HasPrefix(line, "PING") {
			conn.send(strings.Replace(line, "PING", "PONG", 1))
		} else if strings.HasPrefix(line, "PONG") {
			Log.Debug("PONG")
		} else {
			client.toClient <- line
		}
		conn.active = true
	}
}

func (conn *connection) send(msg string) {
	conn.Lock()
	fmt.Fprint(conn.conn, msg+"\r\n")
	conn.Unlock()
}

func (conn *connection) reduceMsgCount() {
	conn.Lock()
	conn.msgCount--
	conn.Unlock()
}

func (conn *connection) countMsg() {
	conn.Lock()
	conn.msgCount++
	conn.Unlock()
	time.AfterFunc(30*time.Second, conn.reduceMsgCount)
}
