package main

import (
	"log"
	"sync"

	"github.com/godbus/dbus/v5"
)

const (
	version		float64		= 1.0
)

var (
	busSigChan			= make(chan notif, 16)
)

type notif struct {
	Type		string
}

func getConn() (conn *dbus.Conn) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Fatalln("Could not connect to session bus:", err)
	}
	return conn
}

func legacyNotifWatcher() () {
	conn := getConn()
	monitorObj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	ruleSlice := []string{
		"type='method_call',interface='org.freedesktop.Notifications',member='Notify',path='/org/freedesktop/Notifications',destination='org.freedesktop.Notifications'",
		//"type='method_call',member='Notify',path='/org/freedesktop/Notifications',interface='org.freedesktop.Notifications'",
	}
	arg2 := uint(0)
	call := monitorObj.Call("org.freedesktop.DBus.Monitoring.BecomeMonitor", 0, ruleSlice, arg2)
	if call.Err != nil {
		log.Fatalln("Could not become bus monitor:", call.Err)
	} else {
		log.Println("Bus replied:", call.Body)
	}
	var sigChan = make(chan *dbus.Message, 16)
	conn.Eavesdrop(sigChan)
	var lastMsg classicNotifBody
	for sig := range sigChan {
		var con notif
		var body classicNotifBody
		err := dbus.Store(sig.Body,
			&body.App,
			&body.ReplaceID,
			&body.Icon,
			&body.Summary,
			&body.Body,
			&body.Actions,
			&body.Hints,
			&body.Expire,
		)
		if lastMsg.Body == body.Body && lastMsg.App == body.App {
			log.Println("Skipping duplicate notification")
			continue
		}
		lastMsg = body
		if err != nil {
			log.Println("Could not decode legacy notification:", err)
		}
		log.Println(body.App, "sent legacy notification")
		busSigChan <- con
	}
}

type classicNotifBody struct {
	App		string
	ReplaceID	uint32
	Icon		string
	Summary		string
	Body		string
	Actions		[]string
	Hints		map[string]dbus.Variant
	Expire		int32
}

func main() {
	log.Println("Starting snotify, version", version)
	var wg sync.WaitGroup
	wg.Go(func() {
		legacyNotifWatcher()
	})

	wg.Wait()
}