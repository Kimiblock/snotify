package main

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

var (
	mu       sync.Mutex
	lastLine string
)

func monitorDbus() {
	cmd := exec.Command("dbus-monitor", "path='/org/freedesktop/Notifications',interface='org.freedesktop.Notifications',member='Notify'")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println("Error creating stdout pipe:", err)
		return
	}

	if err := cmd.Start(); err != nil {
		fmt.Println("Error starting dbus-monitor:", err)
		return
	}

	buf := make([]byte, 1024) // Limit the line length to 1024 bytes

	for {
		n, err := stdout.Read(buf)
		if err != nil {
			fmt.Println("Error reading from dbus-monitor:", err)
			break
		}

		line := string(buf[:n])
		if strings.Contains(line, "member=Notify") {
			// Lock the mutex to safely update lastLine
			mu.Lock()
			lastLine = line
			mu.Unlock()
		}
	}
}

func playSoundOnNewLine() {
	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		// Lock the mutex to safely read lastLine
		mu.Lock()
		currentLine := lastLine
		mu.Unlock()

		if currentLine != "" {
			fmt.Println("Received a Notify event. Playing sound...")
			playSound()

			// Clear lastLine to prevent repeated execution
			mu.Lock()
			lastLine = ""
			mu.Unlock()
		}
	}
}

func playSound() {
	soundCmd := exec.Command("/usr/bin/paplay", "/opt/snotify/message.ogg")
	if err := soundCmd.Start(); err != nil {
		fmt.Println("Error playing sound:", err)
		return
	}

	if err := soundCmd.Wait(); err != nil {
		fmt.Println("Error waiting for sound:", err)
	}
}

func main() {
	go monitorDbus()       // Start monitoring dbus in a goroutine
	go playSoundOnNewLine() // Start checking for new lines and playing sound in a goroutine

	// The program will run indefinitely without waiting for Enter key input
	select {}
}
