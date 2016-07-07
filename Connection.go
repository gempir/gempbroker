package main

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"time"
)

// Connection stores messages sent in the last 30 seconds and the connection itself
type Connection struct {
	conn     net.Conn
	messages int32
	active   bool
	anon     bool
	joins    []string
	alive    bool
	conntype connType
}

// NewConnection initialize a Connection struct
func NewConnection(conn net.Conn) Connection {
	return Connection{
		conn:     conn,
		messages: 0,
		active:   false,
		anon:     true,
		joins:    make([]string, 0),
	}
}

func (connection *Connection) checkIfAlive() bool {
	connection.alive = false
	connection.Message("PING")
	time.Sleep(10 * time.Second)
	if connection.alive {
		return false
	}
	log.Debugf("connection died, reconnecting ... %p\n", connection)
	return true
}

func (connection *Connection) reduceConnectionMessages() {
	atomic.AddInt32(&connection.messages, -1)
}

// Message called everytime you send a message
func (connection *Connection) Message(message string) error {
	if connection.anon {
		return errors.New("connection is anonymous") // don't send message on an anonymous connection
	}
	if !connection.active {
		return errors.New("connection is inactive") // wait for connection to become active
	}
	log.Debug(connection.conn, message)
	fmt.Fprint(connection.conn, message+"\r\n")
	atomic.AddInt32(&connection.messages, 1)
	time.AfterFunc(30*time.Second, connection.reduceConnectionMessages)
	return nil
}
