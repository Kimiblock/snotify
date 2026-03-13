package main

import (
	"log"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/jfreymuth/oggvorbis"
	"github.com/jfreymuth/pulse"
)

const (
	version		float64		= 1.0
	oggFile		string		= "message.ogg"
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
		if lastMsg.Body == body.Body && lastMsg.App == body.App && lastMsg.Summary == body.Summary {
			log.Println("Skipping duplicate notification")
			continue
		}
		lastMsg = body
		if err != nil {
			log.Println("Could not decode legacy notification:", err)
		}
		log.Println(body.App, "sent legacy notification:", body)
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

func audioController() {
	client, err := pulse.NewClient(
		pulse.ClientApplicationName("Snotify Notification Sounds"),
		pulse.ClientApplicationIconName("notifications-new-symbolic"),
	)
	if err != nil {
		log.Fatalln("Could not connect to PulseAudio:", err)
	}
	file, err := os.Open(oggFile)
	if err != nil {
		log.Fatalln("Could not open audio message file:", err)
	}
	defer file.Close()
	readerFile, err := oggvorbis.NewReader(file)
	if err != nil {
		log.Fatalln("Could not read audio message file:", err)
	}

	reader := pulse.Float32Reader(func(f []float32) (int, error) {
		return readerFile.Read(f)
	})
	playback, err := client.NewPlayback(reader)
	if err != nil {
		log.Fatalln("Could not request PulseAudio playback:", err)
	}
	defer playback.Close()
	for sig := range busSigChan {
		playback.Stop()
		readerFile.SetPosition(0)
		log.Println("Playing sound for:", sig)
		go playback.Start()
	}
}

func main() {
	log.Println("Starting snotify, version", version)
	go audioController()
	var wg sync.WaitGroup
	wg.Go(func() {
		legacyNotifWatcher()
	})

	wg.Wait()
}