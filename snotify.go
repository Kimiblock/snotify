package main

import (
	"bufio"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/godbus/dbus/v5"
	"github.com/jfreymuth/oggvorbis"
	"github.com/jfreymuth/pulse"
)

const (
	version		float64		= 1.0
)

var (
	busSigChan			= make(chan notif, 16)
	soundAllowed			bool
	dndLock				sync.RWMutex
	oggFile				string
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

func dndWatcher() () {
	//conn := getConn()
	env := os.Getenv("XDG_CURRENT_DESKTOP")
	switch env {
		case "GNOME":
			valCmdSlice := []string{
				"stdbuf",
				"-oL",
				"gsettings",
				"monitor",
				"org.gnome.desktop.notifications",
				"show-banners",
			}
			getCmdSlice := []string{
				"gsettings",
				"get",
				"org.gnome.desktop.notifications",
				"show-banners",
			}
			cmd := exec.Command(valCmdSlice[0], valCmdSlice[1:]...)
			attr := syscall.SysProcAttr{
				Pdeathsig:	syscall.SIGTERM,
			}
			cmd.SysProcAttr = &attr
			var wg sync.WaitGroup
			wg.Add(1)
			go func () {
				pipe, err := cmd.StdoutPipe()
				wg.Done()
				if err != nil {
					log.Println("GSettings failed:", err)
					return
				}
				scanner := bufio.NewScanner(pipe)
				cmdGet, err := exec.Command(getCmdSlice[0], getCmdSlice[1:]...).Output()
				if err != nil {
					log.Println("GSettings failed:", err)
					return
				}
				line := string(cmdGet)
				rawVal := strings.TrimSpace(line)
				log.Println("Allow sound:", rawVal)
				val, err := strconv.ParseBool(rawVal)
				if err != nil {
					log.Println("Could not parse result:", err)

				} else {
					dndLock.Lock()
					soundAllowed = val
					dndLock.Unlock()
				}
				for scanner.Scan() {
					line := scanner.Text()
					log.Println("Allow status changed:", line)
					rawVal := strings.TrimPrefix(line, "show-banners:")
					rawVal = strings.TrimSpace(rawVal)
					val, err := strconv.ParseBool(rawVal)
					if err != nil {
						log.Println("Could not parse result:", err)
						continue
					}
					dndLock.Lock()
					soundAllowed = val
					dndLock.Unlock()
				}
			} ()
			wg.Wait()
			err := cmd.Start()
			if err != nil {
				fmt.Println("Could not start DnD monitor:", err)
				return
			}
			log.Println("Started DnD watcher")
			err = cmd.Wait()
			if err != nil {
				fmt.Println("GSettings monitor returned error:", err)
				return
			}
		default:
			log.Println("Do not disturb unsupported:", env)
	}
}

func legacyNotifWatcher() () {
	conn := getConn()
	monitorObj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	ruleSlice := []string{
		"type='method_call',interface='org.freedesktop.Notifications',member='Notify',path='/org/freedesktop/Notifications',destination='org.freedesktop.Notifications'",
		//"type='method_call',interface='org.gtk.Notifications',member='AddNotification',path='/org/gtk/Notifications',destination='org.gtk.Notifications'", // GTK's notif API, do we really need those?
		"type='method_call',interface='org.freedesktop.portal.Notification',member='AddNotification',path='/org/freedesktop/portal/desktop',destination='org.freedesktop.portal.Desktop'",
		"type='method_call',interface='org.gtk.Notifications',member='AddNotification',path='/org/gtk/Notifications',destination='org.gtk.Notifications'",
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
				notif, err := decodeGTKNotif(sig)
				if err != nil {
					log.Println("Could not decode notification: tried Legacy, Portal and GTK:", err)
					continue
				}
				con.ID = notif.ID
				con.Type = "GTK"
			} else {
				con.Type = "Portal"
				con.ID = notif.ID
				if notif.Sound {
					log.Println("Portal notification has sound, suppressing ours")
					continue
				}
			}

		} else {
			con.Type = "legacy"
			con.ID = body.App
			if lastMsg.Body == body.Body && lastMsg.App == body.App && lastMsg.Summary == body.Summary {
				log.Println("Skipping duplicate notification")
				lastMsg = classicNotifBody{}
				continue
			}
			lastMsg = body
		}
		log.Println(con.ID, "sent", con.Type,"notification:", body)
		busSigChan <- con
	}
}

func decodeGTKNotif(msg *dbus.Message) (PortalNotification, error) {
	var m = make(map[string]dbus.Variant)
	var notif PortalNotification
	var title string
	err := dbus.Store(msg.Body, &notif.ID, &title, &m)
	if err != nil {
		return notif, errors.New("Could not store message: " + err.Error())
	}
	notif.Title = title
	return notif, nil
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
		dndLock.RLock()
		if soundAllowed == false {
			dndLock.RUnlock()
			log.Println("Not playing sound with DnD")
			continue
		}
		dndLock.RUnlock()
		playback.Stop()
		readerFile.SetPosition(0)
		log.Println("Playing sound for:", sig)
		go playback.Start()
	}
}

func main() {
	log.Println("Starting snotify, version", version)
	envFile := os.Getenv("SNOTIFY_OGG_FILE")
	if len(envFile) == 0 {
		oggFile = "/opt/snotify/message.ogg"
	} else {
		oggFile = envFile
	}
	go audioController()
	go dndWatcher()
	var wg sync.WaitGroup
	wg.Go(func() {
		legacyNotifWatcher()
	})

	wg.Wait()
}