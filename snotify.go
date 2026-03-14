package main

import (
	"errors"
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
	ID		string
}

func getConn() (conn *dbus.Conn) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Fatalln("Could not connect to session bus:", err)
	}
	return conn
}

type NotificationSound struct {
	FileDescriptor dbus.UnixFD `db:"file-descriptor,omitempty"`
}

type PortalNotification struct {
	Title			string
	Body			string
	Sound			bool
	ID			string
}

func legacyNotifWatcher() () {
	conn := getConn()
	monitorObj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	ruleSlice := []string{
		"type='method_call',interface='org.freedesktop.Notifications',member='Notify',path='/org/freedesktop/Notifications',destination='org.freedesktop.Notifications'",
		//"type='method_call',interface='org.gtk.Notifications',member='AddNotification',path='/org/gtk/Notifications',destination='org.gtk.Notifications'", // GTK's notif API, do we really need those?
		"type='method_call',interface='org.freedesktop.portal.Notification',member='AddNotification',path='/org/freedesktop/portal/desktop',destination='org.freedesktop.portal.Desktop'",
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
	log.Println("Initialized D-Bus connection")
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

		if err != nil {
			notif, err := decodePortalNotif(sig)
			if err != nil {
				log.Println("Could not decode Portal notification:", err)
				continue
			}
			con.Type = "Portal"
			con.ID = notif.ID
			if notif.Sound {
				log.Println("Portal notification has sound, suppressing ours")
			}
		} else {
			con.Type = "legacy"
			con.ID = body.App
			if lastMsg.Body == body.Body && lastMsg.App == body.App && lastMsg.Summary == body.Summary {
				log.Println("Skipping duplicate notification")
				continue
			}
			lastMsg = body
		}
		log.Println(con.ID, "sent", con.Type,"notification:", body)
		busSigChan <- con
	}
}

func decodePortalNotif(con *dbus.Message) (PortalNotification, error) {
	var m = make(map[string]dbus.Variant)
	var notif PortalNotification
	err := dbus.Store(con.Body, &notif.ID, &m)
	if err != nil {
		return notif, errors.New("Could not store message: " + err.Error())
	}
	val, ok := m["priority"]
	if ok {
		log.Println("Portal notification priority:", val)
	}
	_, ok = m["sound"]
	if ok {
		notif.Sound = true
	}

	return notif, nil
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
	playback, err := client.NewPlayback(
		reader,
		pulse.PlaybackSampleRate(readerFile.SampleRate()),
		//pulse.PlaybackStereo,
		pulse.PlaybackLatency(0.5),
	)
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