package main

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
)

func monitorGNOMEDND() {
	cmdline := []string{"monitor", "org.gnome.desktop.notifications", "show-banners"}
	cmd := exec.Command("gsettings", cmdline...)
	attrs := syscall.SysProcAttr{
		Pdeathsig:	syscall.SIGTERM,
	}
	cmd.SysProcAttr = &attrs
	cmd.Start()
	go updateDnd()
	type sigChange struct {
		prefix	string
		changes	[]string
		tag	string
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	obj := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	call := obj.Call(
		"org.freedesktop.DBus.AddMatch",
		0,
		"type='signal',path='/ca/desrt/dconf/Writer/user',interface='ca.desrt.dconf.Writer',member='Notify'",
	)
	log.Println("Monitoring DnD")
	if call.Err != nil {
		log.Fatalln("Could not monitor DND status:", call.Err)
	}
	sigChan := make(chan *dbus.Signal, 10)
	conn.Signal(sigChan)

	for sig := range sigChan {
		var chg sigChange
		if sig.Name == "ca.desrt.dconf.Writer.Notify" &&
		sig.Path == "/ca/desrt/dconf/Writer/user" {
			err := dbus.Store(sig.Body, &chg.prefix, &chg.changes, &chg.tag)
			if err != nil {
				log.Fatalln("Could not store change:", err)
			}
			if chg.prefix == "/org/gnome/desktop/notifications/show-banners" {
				go updateDnd()
			}
		}
	}
}

func updateDnd() {
	cmdline := []string{"get", "org.gnome.desktop.notifications", "show-banners"}
	cmd := exec.Command("gsettings", cmdline...)
	attr := &syscall.SysProcAttr{
		Pdeathsig:	syscall.SIGKILL,
	}
	cmd.SysProcAttr = attr
	out, err := cmd.Output()
	if err != nil {
		log.Fatalln("Could not get banner state:", err)
	}
	output := strings.TrimSuffix(string(out), "\n")
	boolVal, err := strconv.ParseBool(output)
	if err != nil {
		log.Fatalln("Could not get banner state:", err)
	}
	dndLock.Lock()
	soundAllowed = boolVal
	dndLock.Unlock()
	log.Println("Updated show-banner status:", boolVal)
}